package learning

import (
	"aiq.local/core/internal/modules/curriculum"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func editor(ctx context.Context, tx pgx.Tx, a httpx.Actor, r Record) (bool, error) {
	current, err := curator(ctx, tx, a, r.StudentID)
	if err != nil {
		return false, err
	}
	if r.Kind == "goals" {
		return current, nil
	}
	if supervisor(a) || r.Scope == "curator" && current {
		return true, nil
	}
	if r.Scope == "lesson" && r.LessonID != nil && has(a, "teacher") {
		var own bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id WHERE l.id=$1 AND t.user_id=$2 AND l.status<>'cancelled')`, *r.LessonID, a.ID).Scan(&own)
		return own, err
	}
	return false, nil
}
func actions(ctx context.Context, tx pgx.Tx, a httpx.Actor, r Record) ([]string, error) {
	out := []string{}
	edit, err := editor(ctx, tx, a, r)
	if err != nil {
		return out, err
	}
	review := supervisor(a) || r.ReviewerID != nil && *r.ReviewerID == a.ID
	if r.State == "withdrawn" || r.State == "cancelled" || r.State == "revised" {
		return out, nil
	}
	if r.Kind == "goals" {
		if edit {
			switch r.State {
			case "planned":
				out = append(out, "activate", "correct", "cancel")
			case "active":
				out = append(out, "achieve", "cancel")
			case "achieved":
				out = append(out, "cancel")
			}
		}
		return out, nil
	}
	if edit && r.Kind == "observations" {
		out = append(out, "correct")
		if r.State == "draft" {
			out = append(out, "submit")
		}
	}
	if r.Kind == "projects" && edit && r.State == "active" {
		out = append(out, "submit")
	}
	if review && r.State == "submitted" && (r.Kind != "observations" || r.AuthorID != a.ID) {
		out = append(out, "approve", "return")
	}
	if r.Kind == "observations" && r.State == "reviewed" && review && r.AuthorID != a.ID {
		if r.Published {
			out = append(out, "unpublish")
		} else {
			out = append(out, "publish")
		}
	}
	if review || edit && r.State == "draft" {
		out = append(out, "withdraw")
	}
	return out, nil
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func initial(kind string) string {
	switch kind {
	case "goals":
		return "planned"
	case "observations":
		return "draft"
	case "assignments":
		return "assigned"
	case "projects":
		return "active"
	}
	return "submitted"
}
func (repo *Repository) Create(ctx context.Context, sid int64, in Input) (Card, error) {
	tx, a, err := access.Begin(ctx, repo.Pool, access.Students, access.Read, true)
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback(ctx)
	if err = student(ctx, tx, a, sid); err != nil {
		return Card{}, err
	}
	assigned, err := curator(ctx, tx, a, sid)
	if err != nil {
		return Card{}, err
	}
	scope := "explicit"
	if assigned {
		scope = "curator"
	} else if !supervisor(a) {
		scope = "lesson"
	}
	if in.Kind == "goals" && !assigned {
		return Card{}, httpx.Forbidden()
	}
	if in.Kind == "goals" {
		scope = "curator"
		in.ReviewerID = nil
	}
	if in.Kind == "attempts" || in.Kind == "goals" && in.ParentID != nil {
		if in.ParentID == nil {
			return Card{}, httpx.Invalid("parent_id", "Укажите назначение.")
		}
		parent, e := load(ctx, tx, a, *in.ParentID)
		if e != nil {
			return Card{}, e
		}
		if parent.StudentID != sid {
			return Card{}, httpx.Invalid("parent_id", "Другой ученик.")
		}
		if in.Kind == "attempts" {
			if parent.Kind != "assignments" || parent.State != "assigned" && parent.State != "revision" && parent.State != "submitted" {
				return Card{}, httpx.Invalid("parent_id", "Назначение закрыто или имеет другой тип.")
			}
			if !validateAnswer(parent.Content.Format, in.Content.Answer, len(in.FileIDs)) {
				return Card{}, httpx.Invalid("answer", "Ответ должен соответствовать формату назначения: текст, HTTP(S) ссылка или приватный файл.")
			}
			today := time.Now().In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02")
			if parent.Content.DueOn < today && (parent.Content.AllowLate == nil || !*parent.Content.AllowLate) {
				return Card{}, httpx.E(409, "late_submission", "Срок истёк; поздняя сдача запрещена.")
			}
			in.Content.Format = parent.Content.Format
			in.Content.DueOn = parent.Content.DueOn
			in.Content.AllowLate = parent.Content.AllowLate
			in.Content.ProgramVersionID = parent.Content.ProgramVersionID
			in.Content.TemplateKey = parent.Content.TemplateKey
		} else {
			if in.Revision != parent.Revision {
				return Card{}, httpx.E(409, "revision_conflict", "Цель изменилась. Обновите карточку.")
			}
			if parent.Kind != "goals" || !contains([]string{"planned", "active", "achieved"}, parent.State) || strings.TrimSpace(in.Reason) == "" {
				return Card{}, httpx.Invalid("parent_id", "Пересмотр требует действующей цели и причины.")
			}
			if _, err = tx.Exec(ctx, `UPDATE learning_records SET state='revised',closed_at=COALESCE(closed_at,clock_timestamp()) WHERE id=$1`, parent.ID); err != nil {
				return Card{}, err
			}
			if err = event(ctx, tx, a, parent.ID, parent.Revision, "revised", in.Reason); err != nil {
				return Card{}, err
			}
		}
		scope = parent.Scope
		in.LessonID = parent.LessonID
		in.ReviewerID = parent.ReviewerID
	} else if in.ParentID != nil {
		return Card{}, httpx.Invalid("parent_id", "Родитель допустим для попытки или пересмотра цели.")
	}
	if in.LessonID != nil {
		allowed, e := access.ObjectReadable(ctx, tx, a, "lesson", *in.LessonID)
		if e != nil {
			return Card{}, e
		}
		if !allowed {
			return Card{}, missing()
		}
		var member, own bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lessons l WHERE l.id=$1 AND ((l.roster_initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM lesson_participants p WHERE p.lesson_id=l.id AND p.student_id=$2 AND p.included)) OR (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e WHERE e.group_id=l.group_id AND e.student_id=$2 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date))))), EXISTS(SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id WHERE l.id=$1 AND t.user_id=$3)`, *in.LessonID, sid, a.ID).Scan(&member, &own)
		if err != nil {
			return Card{}, err
		}
		if !member || scope == "lesson" && !own && !(in.Kind == "attempts" && in.ReviewerID != nil && *in.ReviewerID == a.ID) {
			return Card{}, httpx.Forbidden()
		}
	} else if scope == "lesson" {
		return Card{}, httpx.Invalid("lesson_id", "Для педагогической записи укажите своё занятие с этим учеником.")
	}
	if in.Kind != "goals" && in.Kind != "attempts" && in.ReviewerID == nil {
		return Card{}, httpx.Invalid("reviewer_id", "Назначьте проверяющего.")
	}
	if in.ReviewerID != nil {
		reviewer := httpx.Actor{ID: *in.ReviewerID}
		var blocked bool
		err = tx.QueryRow(ctx, `SELECT blocked,ARRAY(SELECT role FROM user_roles WHERE user_id=u.id) FROM users u WHERE id=$1`, *in.ReviewerID).Scan(&blocked, &reviewer.Roles)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && blocked {
			return Card{}, httpx.Invalid("reviewer_id", "Проверяющий недоступен.")
		}
		if err != nil {
			return Card{}, err
		}
		if err = student(ctx, tx, reviewer, sid); err != nil {
			return Card{}, httpx.Invalid("reviewer_id", "У проверяющего нет доступа к ученику.")
		}
	}
	if in.Kind == "assignments" {
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT v.document FROM program_versions v WHERE v.id=$1 AND v.status='published' AND EXISTS(SELECT 1 FROM group_enrollments e JOIN groups g ON g.id=e.group_id WHERE e.student_id=$2 AND g.program_version_id=v.id AND e.starts_on<=(clock_timestamp() AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(clock_timestamp() AT TIME ZONE 'Europe/Moscow')::date))`, *in.Content.ProgramVersionID, sid).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return Card{}, httpx.Invalid("program_version_id", "Нужна опубликованная версия программы текущего зачисления ученика.")
		}
		if err != nil {
			return Card{}, err
		}
		var doc curriculum.Document
		if err = json.Unmarshal(raw, &doc); err != nil {
			return Card{}, err
		}
		found := false
		for _, section := range doc.Sections {
			for _, module := range section.Modules {
				for _, template := range module.Lessons {
					if template.Key == in.Content.TemplateKey {
						found = true
						in.Content.Context = template.Goals
					}
				}
			}
		}
		if !found {
			return Card{}, httpx.Invalid("template_key", "Шаблон отсутствует в выбранной версии.")
		}
	}
	if in.Kind == "observations" || in.Kind == "achievements" {
		if in.Content.OccurredOn > time.Now().In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02") {
			return Card{}, httpx.Invalid("occurred_on", "Дата факта не может быть в будущем.")
		}
	}
	if in.Kind == "achievements" {
		for _, ref := range in.Sources {
			base, e := load(ctx, tx, a, ref.ID)
			if e != nil {
				return Card{}, e
			}
			if base.Kind != "attempts" && base.Kind != "projects" {
				return Card{}, httpx.Invalid("sources", "Достижение требует принятой работы или проверенного вклада.")
			}
		}
	}
	var parentRevision *int
	if in.ParentID != nil {
		var n int
		if err = tx.QueryRow(ctx, `SELECT current_revision FROM learning_records WHERE id=$1`, *in.ParentID).Scan(&n); err != nil {
			return Card{}, err
		}
		parentRevision = &n
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO learning_records(student_id,kind,scope,lesson_id,parent_id,parent_revision,reviewer_id,created_by,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, sid, in.Kind, scope, in.LessonID, in.ParentID, parentRevision, in.ReviewerID, a.ID, initial(in.Kind)).Scan(&id)
	if err != nil {
		return Card{}, err
	}
	raw, err := json.Marshal(in.Content)
	if err != nil {
		return Card{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO learning_revisions VALUES($1,1,$2,$3,clock_timestamp())`, id, raw, a.ID); err != nil {
		return Card{}, err
	}
	if err = saveEvidence(ctx, tx, a, sid, id, 1, in.Sources, in.FileIDs); err != nil {
		return Card{}, err
	}
	if err = event(ctx, tx, a, id, 1, "created", in.Reason); err != nil {
		return Card{}, err
	}
	if in.Kind == "attempts" && in.ParentID != nil {
		if err = event(ctx, tx, a, *in.ParentID, *parentRevision, "attempt_received", ""); err != nil {
			return Card{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Card{}, err
	}
	return repo.Get(ctx, id, 0)
}
func (repo *Repository) Correct(ctx context.Context, id int64, in Input) (Card, error) {
	tx, a, err := access.Begin(ctx, repo.Pool, access.Students, access.Read, true)
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback(ctx)
	r, err := load(ctx, tx, a, id)
	if err != nil {
		return Card{}, err
	}
	allowed, err := actions(ctx, tx, a, r)
	if err != nil {
		return Card{}, err
	}
	if !contains(allowed, "correct") {
		return Card{}, httpx.Forbidden()
	}
	if r.Revision != in.Revision {
		return Card{}, httpx.E(409, "revision_conflict", "Запись изменилась. Обновите карточку.")
	}
	if in.Kind != r.Kind {
		return Card{}, httpx.Invalid("kind", "Нельзя изменить тип записи.")
	}
	raw, err := json.Marshal(in.Content)
	if err != nil {
		return Card{}, err
	}
	next := r.Revision + 1
	if _, err = tx.Exec(ctx, `INSERT INTO learning_revisions VALUES($1,$2,$3,$4,clock_timestamp())`, id, next, raw, a.ID); err != nil {
		return Card{}, err
	}
	if err = saveEvidence(ctx, tx, a, r.StudentID, id, next, in.Sources, in.FileIDs); err != nil {
		return Card{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE learning_records SET current_revision=$2,state=$3,published=false,closed_at=NULL WHERE id=$1`, id, next, initial(r.Kind)); err != nil {
		return Card{}, err
	}
	if err = event(ctx, tx, a, id, next, "corrected", in.Reason); err != nil {
		return Card{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Card{}, err
	}
	return repo.Get(ctx, id, 0)
}
