package progress

import (
	"aiq.local/core/internal/modules/curriculum"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

func notfound() error { return httpx.E(404, "not_found", "Объект не найден.") }
func check(ctx context.Context, tx pgx.Tx, a httpx.Actor, kind string, id int64) error {
	ok, err := access.ObjectReadable(ctx, tx, a, kind, id)
	if err != nil {
		return err
	}
	if !ok {
		return notfound()
	}
	return nil
}

const reportColumns = `lesson_id,revision,state,outcome,plan_result,comment,confirm_element,plan_assignment_id,template_key,actor_id,created_at,reason`

func scanReport(row pgx.Row) (Report, error) {
	var v Report
	err := row.Scan(&v.LessonID, &v.Revision, &v.State, &v.Outcome, &v.PlanResult, &v.Comment, &v.ConfirmElement, &v.PlanAssignmentID, &v.TemplateKey, &v.ActorID, &v.CreatedAt, &v.Reason)
	return v, err
}
func (r *Repository) Report(ctx context.Context, id int64) (Reports, error) {
	out := Reports{History: []Report{}}
	tx, a, err := access.Begin(ctx, r.Pool, access.Lessons, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = check(ctx, tx, a, "lesson", id); err != nil {
		return out, err
	}
	var teacher *int64
	err = tx.QueryRow(ctx, `SELECT t.user_id FROM lessons l JOIN teachers t ON t.id=l.teacher_id WHERE l.id=$1`, id).Scan(&teacher)
	if err != nil {
		return out, err
	}
	out.CanEdit = access.LessonAllowed(a, teacher, true)
	rows, err := tx.Query(ctx, `SELECT `+reportColumns+` FROM lesson_reports WHERE lesson_id=$1 ORDER BY revision DESC`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scanReport(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.History = append(out.History, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.History) > 0 {
		out.Current = &out.History[0]
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) SaveReport(ctx context.Context, id int64, in ReportInput) (Report, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Attendance, access.Mark, true)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback(ctx)
	var teacher *int64
	var status, key string
	var assignment *int64
	var started bool
	err = tx.QueryRow(ctx, `SELECT t.user_id,l.status,l.plan_assignment_id,l.template_key,l.starts_at<=clock_timestamp() FROM lessons l JOIN teachers t ON t.id=l.teacher_id WHERE l.id=$1 FOR UPDATE OF l`, id).Scan(&teacher, &status, &assignment, &key, &started)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, notfound()
	}
	if err != nil {
		return Report{}, err
	}
	if !access.LessonAllowed(a, teacher, true) {
		return Report{}, notfound()
	}
	if status == "cancelled" || in.State == "submitted" && !started {
		return Report{}, httpx.E(409, "report_unavailable", "Отчёт можно отправить после начала неотменённого занятия.")
	}
	var revision int
	var submitted bool
	err = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0),coalesce(bool_or(state='submitted'),false) FROM lesson_reports WHERE lesson_id=$1`, id).Scan(&revision, &submitted)
	if err != nil {
		return Report{}, err
	}
	if revision != in.Revision {
		return Report{}, httpx.E(409, "revision_conflict", "Отчёт изменён. Обновите карточку; введённый текст можно сохранить отдельно.")
	}
	if (submitted || a.Admin()) && in.Reason == "" {
		return Report{}, httpx.Invalid("reason", "Для исправления отправленного отчёта или записи администратором нужна причина.")
	}
	if submitted && in.State != "submitted" {
		return Report{}, httpx.Invalid("state", "Отправленный отчёт исправляется новой отправленной редакцией.")
	}
	if in.ConfirmElement && (assignment == nil || key == "") {
		return Report{}, httpx.Invalid("confirm_element", "У занятия нет элемента назначенного плана.")
	}
	v, err := scanReport(tx.QueryRow(ctx, `INSERT INTO lesson_reports(lesson_id,revision,state,outcome,plan_result,comment,confirm_element,plan_assignment_id,template_key,actor_id,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+reportColumns, id, revision+1, in.State, in.Outcome, in.PlanResult, in.Comment, in.ConfirmElement, assignment, key, a.ID, in.Reason))
	if err == nil {
		err = database.AuditObject(ctx, tx, &a.ID, "lesson.report_saved", "lesson", id, "report", "confirmation")
	}
	if err != nil {
		return Report{}, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Overview(ctx context.Context, id, assignment int64) (Overview, error) {
	out := Overview{GroupID: id, State: "no_plan", Elements: []Element{}}
	tx, a, err := access.Begin(ctx, r.Pool, access.Groups, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = check(ctx, tx, a, "group", id); err != nil {
		return out, err
	}
	// Aggregate access is explicit and independent from permission to open a group.
	// The constant restricted state does not disclose hidden report counts.
	if access.ScopeFor(a, access.Groups, access.Read) != access.All {
		out.State = "restricted"
		return out, tx.Commit(ctx)
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT p.id,p.version_id,p.assigned_at,v.number,v.document FROM program_plan_assignments p JOIN program_versions v ON v.id=p.version_id WHERE p.group_id=$1 AND (($2::bigint=0 AND p.ended_at IS NULL) OR p.id=$2)`, id, assignment).Scan(&out.AssignmentID, &out.VersionID, &out.AssignedAt, &out.VersionNumber, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		if assignment != 0 {
			return out, notfound()
		}
		return out, tx.Commit(ctx)
	}
	if err != nil {
		return out, err
	}
	var document curriculum.Document
	if err = json.Unmarshal(raw, &document); err != nil {
		return out, err
	}
	index := map[string]int{}
	for _, section := range document.Sections {
		for _, module := range section.Modules {
			for _, element := range module.Lessons {
				if element.Required && element.Weight != nil {
					index[element.Key] = len(out.Elements)
					out.Elements = append(out.Elements, Element{Key: element.Key, Title: element.Topic, Weight: *element.Weight, Sources: []Report{}})
					out.TotalWeight += *element.Weight
				}
			}
		}
	}
	if out.TotalWeight == 0 {
		return out, tx.Commit(ctx)
	}
	rows, err := tx.Query(ctx, `SELECT `+reportColumns+` FROM lesson_reports r WHERE r.plan_assignment_id=$1 AND r.state='submitted' AND r.confirm_element AND r.revision=(SELECT max(r2.revision) FROM lesson_reports r2 WHERE r2.lesson_id=r.lesson_id) AND EXISTS(SELECT 1 FROM lessons l WHERE l.id=r.lesson_id AND l.status='completed' AND l.plan_assignment_id=r.plan_assignment_id AND l.template_key=r.template_key) ORDER BY r.created_at,r.lesson_id`, out.AssignmentID)
	if err != nil {
		return out, err
	}
	done := 0
	for rows.Next() {
		report, e := scanReport(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		if i, ok := index[report.TemplateKey]; ok {
			if !out.Elements[i].Completed {
				out.Elements[i].Completed = true
				done += out.Elements[i].Weight
			}
			out.Elements[i].Sources = append(out.Elements[i].Sources, report)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	percent := 100 * float64(done) / float64(out.TotalWeight)
	out.Percent = &percent
	out.CompletedWeight = &done
	out.State = "ready"
	return out, tx.Commit(ctx)
}
