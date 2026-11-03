package app_test

import (
	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/groups"
	"aiq.local/core/internal/modules/progress"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStudentHomeUsesAssignedWeightedProgress(t *testing.T) {
	a := setupAttendance(t)
	var pid int64
	if e := a.f.pool.QueryRow(context.Background(), `SELECT program_id FROM groups WHERE id=$1`, a.groupID).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	doc := curriculumDocument("Student weighted program")
	weight := 3
	other := 1
	first := &doc.Sections[0].Modules[0].Lessons[0]
	first.Required = true
	first.Weight = &weight
	second := *first
	second.Key = "lesson-2"
	second.Weight = &other
	doc.Sections[0].Modules[0].Lessons = append(doc.Sections[0].Modules[0].Lessons, second)
	v := createVersion(t, a.f.methodologist, pid, nil)
	v = saveVersion(t, a.f.methodologist, v, doc)
	v = versionAction(t, a.f.methodologist, v, "submit", "")
	v = versionAction(t, a.f.educationLeader, v, "publish", "")
	var group groups.Group
	decode(t, expect(t, a.admin, "POST", "/groups", map[string]any{"name": "Student weighted group", "program_id": pid, "program_version_id": v.ID, "capacity": 10, "status": "active"}, 201), &group)
	expect(t, a.admin, "POST", groupPath(group.ID)+"/enrollments", map[string]any{"student_id": a.firstID, "starts_on": "2000-01-01"}, 201)
	expect(t, a.admin, "POST", groupPath(group.ID)+"/enrollments", map[string]any{"student_id": a.secondID, "starts_on": "2000-01-01"}, 201)
	a.groupID = group.ID
	body := lessonBody(group.ID, a.teacherID, a.base.Add(-3*time.Hour).Format(time.RFC3339Nano), a.base.Add(-2*time.Hour).Format(time.RFC3339Nano))
	body["version_module_key"] = "module-1"
	body["template_key"] = "lesson-1"
	l := lessonCreate(t, a.admin, body)
	input := progress.ReportInput{State: "submitted", Outcome: "INTERNAL OUTCOME", Comment: "INTERNAL COMMENT", PlanResult: "complete", ConfirmElement: true}
	expect(t, a.own, "PUT", lessonPath(l.ID)+"/report", input, 200)
	finishOverviewLesson(t, a, l, "present")
	mail := familyMail(t, a.f)
	a.admin = a.f.adminBrowser(t)
	pupil := familyUser(t, a.f, a.admin, mail, "weighted-student@example.test", "student", a.firstID)
	parentCard := parentCreate(t, a.admin, map[string]any{"name": "Weighted parent", "email": "weighted-parent@example.test"})
	linkParent(t, a.admin, a.firstID, parentCard.ID, true)
	parent := familyUser(t, a.f, a.admin, mail, "weighted-parent@example.test", "parent", parentCard.ID)
	familyLink(t, a.admin, a.firstID, parentCard.ID, true)
	check := func(percent float64) {
		t.Helper()
		raw := expect(t, pupil, "GET", fmt.Sprintf("/family/students/%d/home", a.firstID), nil, 200)
		if strings.Contains(string(raw), "INTERNAL") || strings.Contains(string(raw), "actor_id") || strings.Contains(string(raw), "elements") {
			t.Fatal("report details leaked")
		}
		var home family.Home
		decode(t, raw, &home)
		found := false
		for _, p := range home.Programs {
			if p.Percent != nil {
				found = true
				if *p.Percent != percent {
					t.Fatal("wrong weighted progress", *p.Percent)
				}
			}
		}
		if !found {
			t.Fatal("missing program progress")
		}
		var parentHome family.Home
		decode(t, expect(t, parent, "GET", fmt.Sprintf("/family/students/%d/parent-home", a.firstID), nil, 200), &parentHome)
		if len(parentHome.Programs) != len(home.Programs) {
			t.Fatal("parent program scope differs")
		}
		for i, p := range parentHome.Programs {
			if (p.Percent == nil) != (home.Programs[i].Percent == nil) || (p.Percent != nil && *p.Percent != *home.Programs[i].Percent) {
				t.Fatal("parent weighted progress differs")
			}
		}
	}
	check(75)
	input.Revision = 1
	input.ConfirmElement = false
	input.Reason = "Correct confirmation"
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/report", input, 200)
	check(0)
}
