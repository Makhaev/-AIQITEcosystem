package app_test

import (
	"aiq.local/core/internal/modules/groups"
	"aiq.local/core/internal/modules/lessons"
	"aiq.local/core/internal/modules/progress"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLearningProgressReportVersionWeightAndRevocation(t *testing.T) {
	a := setupAttendance(t)
	var programID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT program_id FROM groups WHERE id=$1`, a.groupID).Scan(&programID); err != nil {
		t.Fatal(err)
	}
	doc := curriculumDocument("Weighted program")
	weight := 3
	otherWeight := 1
	first := &doc.Sections[0].Modules[0].Lessons[0]
	first.Required = true
	first.Weight = &weight
	second := *first
	second.Key = "lesson-2"
	second.Weight = &otherWeight
	doc.Sections[0].Modules[0].Lessons = append(doc.Sections[0].Modules[0].Lessons, second)
	v := createVersion(t, a.f.methodologist, programID, nil)
	v = saveVersion(t, a.f.methodologist, v, doc)
	v = versionAction(t, a.f.methodologist, v, "submit", "")
	v = versionAction(t, a.f.educationLeader, v, "publish", "")
	var group groups.Group
	decode(t, expect(t, a.admin, "POST", "/groups", map[string]any{"name": "Weighted group", "program_id": programID, "program_version_id": v.ID, "capacity": 10, "status": "active"}, 201), &group)
	for _, sid := range []int64{a.firstID, a.secondID} {
		expect(t, a.admin, "POST", groupPath(group.ID)+"/enrollments", map[string]any{"student_id": sid, "starts_on": "2000-01-01"}, 201)
	}
	a.groupID = group.ID
	makeLesson := func(offset time.Duration) lessons.Lesson {
		body := lessonBody(group.ID, a.teacherID, a.base.Add(offset).Format(time.RFC3339Nano), a.base.Add(offset+time.Hour).Format(time.RFC3339Nano))
		body["version_module_key"] = "module-1"
		body["template_key"] = "lesson-1"
		return lessonCreate(t, a.admin, body)
	}
	l := makeLesson(-4 * time.Hour)
	read := func() progress.Overview {
		var out progress.Overview
		decode(t, expect(t, a.admin, "GET", groupPath(group.ID)+"/progress", nil, 200), &out)
		return out
	}
	p := read()
	if p.Percent == nil || *p.Percent != 0 || p.TotalWeight != 4 {
		t.Fatalf("initial: %+v", p)
	}
	in := progress.ReportInput{State: "submitted", Outcome: "Observed result", PlanResult: "complete", ConfirmElement: true}
	path := lessonPath(l.ID) + "/report"
	var report progress.Report
	decode(t, expect(t, a.own, "PUT", path, in, 200), &report)
	if p = read(); p.Percent == nil || *p.Percent != 0 {
		t.Fatal("unfinished lesson counted")
	}
	finishOverviewLesson(t, a, l, "present")
	if p = read(); p.Percent == nil || *p.Percent != 75 || len(p.Elements[0].Sources) != 1 {
		t.Fatalf("confirmed: %+v", p)
	}
	expect(t, a.own, "PUT", path, in, 409)
	in.Revision = report.Revision
	in.ConfirmElement = false
	in.Reason = "Retract erroneous confirmation"
	expect(t, a.own, "PUT", path, in, 200)
	if p = read(); *p.Percent != 0 {
		t.Fatal("revoked confirmation counted")
	}
	in.Revision++
	in.ConfirmElement = true
	in.Reason = "Corrected evidence"
	expect(t, a.own, "PUT", path, in, 200)
	repeat := makeLesson(-2 * time.Hour)
	in.Revision = 0
	in.Reason = ""
	expect(t, a.own, "PUT", lessonPath(repeat.ID)+"/report", in, 200)
	finishOverviewLesson(t, a, repeat, "present")
	if p = read(); *p.Percent != 75 || len(p.Elements[0].Sources) != 2 {
		t.Fatal("element counted twice")
	}
	var restricted progress.Overview
	decode(t, expect(t, a.own, "GET", groupPath(group.ID)+"/progress", nil, 200), &restricted)
	if restricted.State != "restricted" || restricted.Percent != nil || restricted.CompletedWeight != nil || len(restricted.Elements) != 0 {
		t.Fatal("aggregate escaped source scope")
	}
	expect(t, a.foreign, "GET", path, nil, 404)
	expect(t, a.foreign, "PUT", path, in, 404)
	var history progress.Reports
	decode(t, expect(t, a.own, "GET", path, nil, 200), &history)
	if len(history.History) != 3 {
		t.Fatal("report history lost")
	}
	if _, err := a.f.pool.Exec(context.Background(), `UPDATE lesson_reports SET outcome='changed' WHERE lesson_id=$1`, l.ID); err == nil {
		t.Fatal("history changed")
	}
}

func TestLearningProgressNoPlanIsNotZero(t *testing.T) {
	a := setupAttendance(t)
	var p progress.Overview
	decode(t, expect(t, a.admin, "GET", fmt.Sprintf("/groups/%d/progress", a.groupID), nil, 200), &p)
	if p.State != "no_plan" || p.Percent != nil || p.CompletedWeight != nil {
		t.Fatal("missing weights became a result")
	}
}
