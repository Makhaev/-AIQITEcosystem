package learning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// AttemptEligibility is the single deadline/format/state policy for staff and pupils.
func AttemptEligibility(parent Record, answer string, fileCount int) error {
	if err := AttemptAvailability(parent); err != nil {
		return err
	}
	if !validateAnswer(parent.Content.Format, answer, fileCount) {
		return httpx.Invalid("answer", "Ответ должен соответствовать формату назначения: текст, HTTP(S) ссылка или приватный файл.")
	}
	return nil
}

func AttemptAvailability(parent Record) error {
	if parent.Kind != "assignments" || !contains([]string{"assigned", "revision", "submitted"}, parent.State) {
		return httpx.Invalid("parent_id", "Назначение закрыто или имеет другой тип.")
	}
	if parent.Content.DueOn < time.Now().In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02") && (parent.Content.AllowLate == nil || !*parent.Content.AllowLate) {
		return httpx.E(409, "late_submission", "Срок истёк; поздняя сдача запрещена.")
	}
	return nil
}

type StudentAttempt struct {
	Revision int          `json:"revision"`
	Answer   string       `json:"answer"`
	File     *StudentFile `json:"file,omitempty"`
}
type StudentFile struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}

// SubmitStudent creates an ordinary learning attempt. No client-supplied identity,
// reviewer, file ID, scope or state is trusted. File and attempt commit atomically.
func (repo *Repository) SubmitStudent(ctx context.Context, assignment int64, in StudentAttempt) (int64, error) {
	tx, a, err := access.Begin(ctx, repo.Pool, access.Family, access.Read, true)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if !has(a, "student") {
		return 0, httpx.Forbidden()
	}
	var p Record
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT r.id,r.student_id,r.kind,r.state,r.current_revision,r.scope,r.lesson_id,r.reviewer_id,v.content
 FROM learning_records r JOIN learning_revisions v ON v.record_id=r.id AND v.revision=r.current_revision
 JOIN students s ON s.id=r.student_id JOIN student_accounts sa ON sa.student_id=s.id
 WHERE r.id=$1 AND r.kind='assignments' AND s.status='active' AND sa.user_id=$2 FOR UPDATE OF r`, assignment, a.ID).Scan(&p.ID, &p.StudentID, &p.Kind, &p.State, &p.Revision, &p.Scope, &p.LessonID, &p.ReviewerID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, missing()
	}
	if err != nil {
		return 0, err
	}
	if err = json.Unmarshal(raw, &p.Content); err != nil {
		return 0, err
	}
	if in.Revision != p.Revision {
		return 0, httpx.E(409, "revision_conflict", "Задание изменилось. Обновите его перед отправкой.")
	}
	count := 0
	if in.File != nil {
		count = 1
	}
	if err = AttemptEligibility(p, in.Answer, count); err != nil {
		return 0, err
	}
	if (p.Content.Format != "file" && in.File != nil) || (p.Content.Format == "file" && in.Answer != "") {
		return 0, httpx.Invalid("answer", "Отправьте ответ только в назначенном формате.")
	}
	c := p.Content
	c = Content{Title: c.Title, Answer: in.Answer, Format: c.Format, DueOn: c.DueOn, AllowLate: c.AllowLate, ProgramVersionID: c.ProgramVersionID, TemplateKey: c.TemplateKey, Channel: "student_portal", Sender: fmt.Sprintf("user:%d", a.ID)}
	input := Input{Kind: "attempts", ParentID: &p.ID, Content: c}
	if err = validate(&input); err != nil {
		return 0, err
	}
	var reviewer httpx.Actor
	var blocked bool
	if p.ReviewerID == nil {
		return 0, httpx.E(409, "reviewer_unavailable", "Проверяющий недоступен. Обратитесь к куратору.")
	}
	reviewer.ID = *p.ReviewerID
	err = tx.QueryRow(ctx, `SELECT blocked,ARRAY(SELECT role FROM user_roles WHERE user_id=u.id) FROM users u WHERE id=$1`, reviewer.ID).Scan(&blocked, &reviewer.Roles)
	if err != nil {
		return 0, err
	}
	if blocked || student(ctx, tx, reviewer, p.StudentID) != nil {
		return 0, httpx.E(409, "reviewer_unavailable", "Проверяющий недоступен. Обратитесь к куратору.")
	}
	if _, err = load(ctx, tx, reviewer, p.ID); err != nil {
		return 0, httpx.E(409, "reviewer_unavailable", "Проверяющий недоступен. Обратитесь к куратору.")
	}
	var fid int64
	if in.File != nil {
		f := files.Input{StudentID: p.StudentID, Name: in.File.Name, Data: in.File.Data}
		kind, hash, e := files.Validate(&f)
		if e != nil {
			return 0, e
		}
		err = tx.QueryRow(ctx, `INSERT INTO private_files(student_id,name,media_type,data,sha256,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, p.StudentID, f.Name, kind, f.Data, hash, a.ID).Scan(&fid)
		if err != nil {
			return 0, err
		}
		// The assigned reviewer manages the submitted attachment; pupil downloads still
		// require an active own binding and a visible parent attempt on every request.
		if _, err = tx.Exec(ctx, `INSERT INTO private_file_grants(file_id,user_id,can_manage) VALUES($1,$2,true)`, fid, reviewer.ID); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO family_file_access(file_id,students,parents,changed_by) VALUES($1,true,false,$2)`, fid, a.ID); err != nil {
			return 0, err
		}
		if err = database.AuditObject(ctx, tx, &a.ID, "file.created", "private_file", fid, "content"); err != nil {
			return 0, err
		}
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO learning_records(student_id,kind,scope,lesson_id,parent_id,parent_revision,reviewer_id,created_by,state) VALUES($1,'attempts',$2,$3,$4,$5,$6,$7,'submitted') RETURNING id`, p.StudentID, p.Scope, p.LessonID, p.ID, p.Revision, p.ReviewerID, a.ID).Scan(&id)
	if err != nil {
		return 0, err
	}
	raw, err = json.Marshal(c)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO learning_revisions(record_id,revision,content,author_id) VALUES($1,1,$2,$3)`, id, raw, a.ID); err != nil {
		return 0, err
	}
	if fid != 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO learning_files VALUES($1,1,$2)`, id, fid); err != nil {
			return 0, err
		}
	}
	if err = event(ctx, tx, a, id, 1, "created", ""); err != nil {
		return 0, err
	}
	if err = event(ctx, tx, a, p.ID, p.Revision, "attempt_received", ""); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}
