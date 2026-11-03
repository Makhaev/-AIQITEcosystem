package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/progress"
)

func TestFamilyReportCompositionSnapshotPrivacyAndLifecycle(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	mail := familyMail(t, a.f)
	a.admin = a.f.adminBrowser(t)
	pupil := familyUser(t, a.f, a.admin, mail, "report-pupil@example.test", "student", a.firstID)
	for i, status := range []string{"present", "late", "absent", "excused", "absent"} {
		start := time.Date(2020, 3, 31, 21, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
		if i == 4 {
			start = start.Add(20 * time.Hour)
		}
		body := lessonBody(a.groupID, a.teacherID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339))
		body["template_key"] = "lesson-1"
		body["version_module_key"] = "module-1"
		l := lessonCreate(t, a.admin, body)
		finishOverviewLesson(t, a, l, status)
		expect(t, a.admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, status, "PRIVATE JOURNAL")), 204)
		expect(t, a.admin, "PUT", lessonPath(l.ID)+"/report", progress.ReportInput{State: "submitted", Outcome: "PRIVATE OUTCOME", Comment: "PRIVATE COMMENT", Reason: "PRIVATE REASON", PlanResult: "complete", ConfirmElement: true}, 200)
	}
	// Deliberately seed excluded drafts, other pupils, out-of-period and stale evidence.
	seed := func(sid int64, kind, state, title, day string) int64 {
		var id int64
		if err := a.f.pool.QueryRow(ctx, `INSERT INTO learning_records(student_id,kind,scope,created_by,state,created_at) VALUES($1,$2,'explicit',$3,$4,$5::date::timestamp AT TIME ZONE 'Europe/Moscow') RETURNING id`, sid, kind, a.f.admin.ID, state, day).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := a.f.pool.Exec(ctx, `INSERT INTO learning_revisions(record_id,revision,author_id,content) VALUES($1,1,$2,jsonb_build_object('title',$3::text,'occurred_on',$4::text,'answer','PRIVATE ANSWER','interpretation','PRIVATE NOTE','context','PRIVATE CONTEXT'))`, id, a.f.admin.ID, title, day); err != nil {
			t.Fatal(err)
		}
		return id
	}
	work := seed(a.firstID, "attempts", "accepted", "Accepted work", "2020-04-01")
	achievement := seed(a.firstID, "achievements", "confirmed", "Confirmed result", "2020-04-01")
	if _, err := a.f.pool.Exec(ctx, `INSERT INTO learning_sources(record_id,revision,source_id,source_revision) VALUES($1,1,$2,1)`, achievement, work); err != nil {
		t.Fatal(err)
	}
	seed(a.firstID, "attempts", "submitted", "PRIVATE UNACCEPTED", "2020-04-01")
	seed(a.firstID, "attempts", "accepted", "PRIVATE OUTSIDE", "2020-04-02")
	seed(a.secondID, "attempts", "accepted", "PRIVATE OTHER", "2020-04-01")
	seed(a.firstID, "observations", "reviewed", "PRIVATE OBSERVATION", "2020-04-01")
	seed(a.firstID, "achievements", "confirmed", "PRIVATE NO BASIS", "2020-04-01")
	staleWork := seed(a.firstID, "attempts", "withdrawn", "PRIVATE WITHDRAWN", "2020-04-01")
	stale := seed(a.firstID, "achievements", "confirmed", "PRIVATE STALE", "2020-04-01")
	if _, err := a.f.pool.Exec(ctx, `INSERT INTO learning_sources(record_id,revision,source_id,source_revision) VALUES($1,1,$2,1)`, stale, staleWork); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/students/%d/family-reports", a.firstID)
	in := family.ReportInput{Generate: true, From: "2020-04-01", To: "2020-04-01"}
	var report family.Report
	raw := expect(t, a.admin, "POST", path, in, 201)
	decode(t, raw, &report)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatalf("private text leaked: %s", raw)
	}
	s := report.Snapshot
	if s == nil || s.Attendance.Percent == nil || *s.Attendance.Percent != 50 || s.Attendance.Total != 4 || len(s.Elements) != 1 || len(s.Works) != 2 {
		t.Fatalf("bad composition: %+v", s)
	}
	if report.AuthorID != a.f.admin.ID || report.CreatedAt == nil || report.UpdatedBy != a.f.admin.ID || report.Published || report.Revision != 1 {
		t.Fatalf("bad metadata: %+v", report)
	}
	edit := fmt.Sprintf("/family-reports/%d", report.ID)
	familyPath := fmt.Sprintf("/family/reports/%d", report.ID)
	yes := true
	expect(t, pupil, "GET", familyPath, nil, 404)
	expect(t, pupil, "POST", path, in, 403)
	expect(t, a.foreign, "POST", path, in, 403)
	expect(t, a.admin, "POST", edit+"/publication", family.Publication{Revision: 1, Published: &yes}, 400)
	in.Generate = false
	in.Title = "Family result"
	in.Body = "Prepared family text"
	in.NextFocus = "Next: explain solutions"
	in.ReviewerID = &a.f.admin.ID
	in.Revision = 1
	bad := in
	bad.ReviewerID = &a.foreignUserID
	expect(t, a.admin, "PUT", edit, bad, 400)
	decode(t, expect(t, a.admin, "PUT", edit, in, 200), &report)
	report = publishFamilyReport(t, a.f, a.admin, a.admin, report)
	raw = expect(t, pupil, "GET", familyPath, nil, 200)
	if strings.Contains(string(raw), "PRIVATE") || report.PublishedAt == nil {
		t.Fatalf("publication: %s", raw)
	}
	// Later source changes do not silently rewrite an already assembled report.
	if _, err := a.f.pool.Exec(ctx, `UPDATE learning_records SET state='withdrawn' WHERE id=$1`, work); err != nil {
		t.Fatal(err)
	}
	report = reportAction(t, a.admin, report, "correct", "")
	in.ReviewerID = report.ReviewerID
	in.Revision = report.Revision
	decode(t, expect(t, a.admin, "PUT", edit, in, 200), &report)
	if len(report.Snapshot.Works) != 2 || !report.Snapshot.CollectedAt.Equal(s.CollectedAt) || report.WithdrawnAt != nil {
		t.Fatal("ordinary edit changed snapshot or lost action date")
	}
	expect(t, pupil, "GET", familyPath, nil, 200)
	expect(t, a.admin, "PUT", edit, in, 409)
	in.Generate = true
	in.Revision = report.Revision
	decode(t, expect(t, a.admin, "PUT", edit, in, 200), &report)
	if len(report.Snapshot.Works) != 0 || report.Body == in.Body {
		t.Fatal("explicit regeneration did not refresh snapshot/template")
	}
	var history int
	if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM family_report_history WHERE report_id=$1 AND content ? 'snapshot' AND content ? 'updated_at'`, report.ID).Scan(&history); err != nil || history != report.Revision {
		t.Fatalf("history=%d revision=%d err=%v", history, report.Revision, err)
	}
	in.From = "1999-01-01"
	in.To = in.From
	in.Revision = report.Revision
	decode(t, expect(t, a.admin, "PUT", edit, in, 200), &report)
	if report.Snapshot.Attendance.Percent != nil || len(report.Snapshot.Elements) != 0 {
		t.Fatal("empty period is not empty")
	}
	for _, dates := range [][2]string{{"2020-02-30", "2020-03-01"}, {"2020-04-02", "2020-04-01"}, {"2020-04-01", ""}} {
		in.From = dates[0]
		in.To = dates[1]
		expect(t, a.admin, "POST", path, in, 400)
	}
}
