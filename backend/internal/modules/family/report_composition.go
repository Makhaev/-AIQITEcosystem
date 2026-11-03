package family

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type ReportElement struct {
	AssignmentID int64  `json:"assignment_id"`
	TemplateKey  string `json:"template_key"`
	Title        string `json:"title"`
}
type ReportWork struct {
	ID       int64  `json:"id"`
	Revision int    `json:"revision"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Date     string `json:"date"`
}
type ReportSnapshot struct {
	StudentName string            `json:"student_name"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	CollectedAt time.Time         `json:"collected_at"`
	Attendance  AttendanceSummary `json:"attendance"`
	Elements    []ReportElement   `json:"elements"`
	Works       []ReportWork      `json:"works"`
}

func validateReportComposition(in ReportInput) error {
	if in.From != "" || in.To != "" || in.Generate {
		from, e1 := time.Parse("2006-01-02", in.From)
		to, e2 := time.Parse("2006-01-02", in.To)
		if e1 != nil || e2 != nil || from.After(to) || from.Year() < 1 || to.Year() > 9999 {
			return httpx.Invalid("period", "Укажите начало и окончание периода в правильном порядке.")
		}
	}
	if utf8.RuneCountInString(in.NextFocus) > 5000 || (in.ReviewerID != nil && *in.ReviewerID < 1) {
		return httpx.Invalid("report", "Проверьте ID проверяющего и следующий фокус (до 5000 символов).")
	}
	return nil
}

// Assignment gives an eligible reviewer access to this report, not the pupil directory.
func reportReviewer(ctx context.Context, tx pgx.Tx, id *int64) error {
	if id == nil {
		return nil
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=$1 AND NOT u.blocked AND EXISTS(
 SELECT 1 FROM user_roles ur WHERE ur.user_id=u.id AND ur.role IN ('admin','education_leader')))`, *id).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Invalid("reviewer_id", "Проверяющий должен быть действующим администратором или руководителем образования.")
	}
	return nil
}

// Only allowlisted educational facts enter this snapshot. Never read free-text
// attendance comments, lesson outcomes, observations, interpretations or decisions.
func collectReport(ctx context.Context, tx pgx.Tx, sid int64, from, to string) (*ReportSnapshot, error) {
	s := &ReportSnapshot{From: from, To: to, Elements: []ReportElement{}, Works: []ReportWork{}}
	if err := tx.QueryRow(ctx, `SELECT name,clock_timestamp() FROM students WHERE id=$1`, sid).Scan(&s.StudentName, &s.CollectedAt); err != nil {
		return nil, err
	}
	a := &s.Attendance
	a.From = from
	a.To = to
	err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE a.status='present'),count(*) FILTER(WHERE a.status='late'),count(*) FILTER(WHERE a.status='absent'),count(*) FILTER(WHERE a.status='excused')
 FROM lessons l JOIN lesson_participants p ON p.lesson_id=l.id JOIN attendance a ON a.id=p.id
 WHERE p.student_id=$1 AND p.included AND l.status='completed'
 AND l.starts_at>=($2::date::timestamp AT TIME ZONE 'Europe/Moscow') AND l.starts_at<(($3::date+1)::timestamp AT TIME ZONE 'Europe/Moscow')`, sid, from, to).Scan(&a.Present, &a.Late, &a.Absent, &a.Excused)
	if err != nil {
		return nil, err
	}
	a.Total = a.Present + a.Late + a.Absent + a.Excused
	if a.Total > 0 {
		p := float64(a.Present+a.Late) * 100 / float64(a.Total)
		a.Percent = &p
	}
	// Distinct plan elements of completed lessons in the pupil's frozen roster.
	// This describes what the group covered, not individual mastery or attendance.
	rows, err := tx.Query(ctx, `SELECT DISTINCT lr.plan_assignment_id,lr.template_key,COALESCE(el->>'topic','')
 FROM lessons l JOIN lesson_participants p ON p.lesson_id=l.id AND p.student_id=$1 AND p.included
 JOIN LATERAL (SELECT plan_assignment_id,template_key,state,confirm_element FROM lesson_reports WHERE lesson_id=l.id ORDER BY revision DESC LIMIT 1) lr ON true
 JOIN program_plan_assignments pa ON pa.id=lr.plan_assignment_id
 JOIN program_versions pv ON pv.id=pa.version_id
 CROSS JOIN LATERAL jsonb_array_elements(pv.document->'sections') sec
 CROSS JOIN LATERAL jsonb_array_elements(sec->'modules') mod
 CROSS JOIN LATERAL jsonb_array_elements(mod->'lessons') el
 WHERE l.status='completed' AND lr.state='submitted' AND lr.confirm_element AND el->>'key'=lr.template_key
 AND l.starts_at>=($2::date::timestamp AT TIME ZONE 'Europe/Moscow') AND l.starts_at<(($3::date+1)::timestamp AT TIME ZONE 'Europe/Moscow')
 ORDER BY 1,2`, sid, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e ReportElement
		if err = rows.Scan(&e.AssignmentID, &e.TemplateKey, &e.Title); err != nil {
			rows.Close()
			return nil, err
		}
		s.Elements = append(s.Elements, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Works use submission registration date; achievements use their fact date.
	// Reject stale/revoked evidence before including a confirmed achievement.
	rows, err = tx.Query(ctx, `SELECT r.id,r.current_revision,r.kind,COALESCE(v.content->>'title',''),
 CASE WHEN r.kind='achievements' THEN v.content->>'occurred_on' ELSE to_char(r.created_at AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD') END AS fact_date
 `+recordFrom+` WHERE r.student_id=$1 AND ((r.kind='attempts' AND r.state='accepted') OR
 (r.kind='achievements' AND r.state='confirmed' AND EXISTS(SELECT 1 FROM learning_sources x WHERE x.record_id=r.id AND x.revision=r.current_revision)
 AND NOT EXISTS(SELECT 1 FROM learning_sources x JOIN learning_records src ON src.id=x.source_id
 WHERE x.record_id=r.id AND x.revision=r.current_revision AND (src.student_id<>r.student_id OR src.current_revision<>x.source_revision OR NOT ((src.kind='attempts' AND src.state='accepted') OR (src.kind='projects' AND src.state='completed'))))))
 AND (CASE WHEN r.kind='achievements' THEN v.content->>'occurred_on' ELSE to_char(r.created_at AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD') END) BETWEEN $2 AND $3
 ORDER BY fact_date,r.id`, sid, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var w ReportWork
		if err = rows.Scan(&w.ID, &w.Revision, &w.Kind, &w.Title, &w.Date); err != nil {
			rows.Close()
			return nil, err
		}
		s.Works = append(s.Works, w)
	}
	err = rows.Err()
	rows.Close()
	return s, err
}

func reportTemplate(s *ReportSnapshot) string {
	attendance := "Нет учтённых отметок посещаемости."
	if s.Attendance.Percent != nil {
		attendance = fmt.Sprintf("Посещаемость: %.1f%%. Присутствовал: %d, опоздал: %d, отсутствовал: %d, уважительная причина: %d.", *s.Attendance.Percent, s.Attendance.Present, s.Attendance.Late, s.Attendance.Absent, s.Attendance.Excused)
	}
	works, achievements := 0, 0
	for _, w := range s.Works {
		if w.Kind == "achievements" {
			achievements++
		} else {
			works++
		}
	}
	return fmt.Sprintf("Отчёт за период %s — %s.\n%s\nПройдено элементов программы в группе: %d. Это не оценка личного освоения.\nПринятых работ: %d. Подтверждённых достижений: %d.", s.From, s.To, attendance, len(s.Elements), works, achievements)
}
