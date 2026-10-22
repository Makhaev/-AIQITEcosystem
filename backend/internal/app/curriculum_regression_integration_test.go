package app_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"aiq.local/core/internal/modules/curriculum"
	"aiq.local/core/internal/modules/lessons"
	"aiq.local/core/internal/modules/programs"
)

func TestM2HistoricalProgramAccessAfterCrossProgramTransfer(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Историческая программа", "status": "active"})
	q := programCreate(t, admin, map[string]any{"name": "Новая программа", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	target := groupCreate(t, admin, q.ID, 10, "active")
	user, teacher := provision(t, f, admin, "historical-teacher@example.test", "teacher")
	profile := teacherCreate(t, admin, map[string]any{"name": "Педагог", "user_id": user.ID})
	_, stranger := provision(t, f, admin, "historical-stranger@example.test", "teacher")
	curatorUser, curator := provision(t, f, admin, "historical-curator@example.test", "curator")
	lateUser, lateCurator := provision(t, f, admin, "late-curator@example.test", "curator")
	student := studentCreate(t, admin, map[string]any{"name": "Участник", "curator_id": curatorUser.ID})
	lateStudent := studentCreate(t, admin, map[string]any{"name": "Позднее зачисление", "curator_id": lateUser.ID})
	start := time.Now().Add(-48 * time.Hour)
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": student.ID, "starts_on": start.Add(-24 * time.Hour).Format("2006-01-02")}, 201)
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": lateStudent.ID, "starts_on": time.Now().Format("2006-01-02")}, 201)
	var l lessons.Lesson
	decode(t, expect(t, admin, "POST", "/lessons", map[string]any{
		"group_id": g.ID, "teacher_id": profile.ID, "topic": "Старое занятие",
		"starts_at": start, "ends_at": start.Add(time.Hour),
		"version_module_key": "module-1", "template_key": "lesson-1",
	}, 201), &l)
	// Freeze the actual participants. Later group membership must not grant
	// access to an old version after that group moves to another program.
	expect(t, admin, "GET", lessonPath(l.ID)+"/attendance", nil, 200)
	var preview curriculum.Preview
	decode(t, expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer/preview", map[string]any{"target_version_id": target.ProgramVersionID}, 200), &preview)
	expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer", curriculum.TransferInput{
		TargetVersionID: target.ProgramVersionID, PreviewToken: preview.Token, Reason: "Смена программы", Mappings: []curriculum.Mapping{},
	}, 200)
	for _, viewer := range []*browser{teacher, curator} {
		expect(t, viewer, "GET", lessonPath(l.ID), nil, 200)
		expect(t, viewer, "GET", versionPath(p.ID, g.ProgramVersionID), nil, 200)
		expect(t, viewer, "GET", fmt.Sprintf("/programs/%d", p.ID), nil, 200)
		var page programs.Page
		decode(t, expect(t, viewer, "GET", "/programs", nil, 200), &page)
		found := false
		for _, item := range page.Items {
			found = found || item.ID == p.ID
		}
		if !found {
			t.Fatal("historical program missing from accessible catalog")
		}
	}
	for _, viewer := range []*browser{stranger, lateCurator} {
		expect(t, viewer, "GET", lessonPath(l.ID), nil, 404)
		expect(t, viewer, "GET", versionPath(p.ID, g.ProgramVersionID), nil, 404)
		expect(t, viewer, "GET", fmt.Sprintf("/programs/%d", p.ID), nil, 404)
	}
}

func TestM2ModuleOnlyPlanSurvivesCatalogChangesAndCompletion(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Опубликованное название", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Педагог"})
	start := time.Now().Add(-2 * time.Hour)
	var l lessons.Lesson
	decode(t, expect(t, admin, "POST", "/lessons", map[string]any{
		"group_id": g.ID, "teacher_id": teacher.ID, "topic": "Тема без шаблона",
		"starts_at": start, "ends_at": start.Add(time.Hour), "version_module_key": "module-1",
	}, 201), &l)
	if l.ModuleName == nil || *l.ModuleName != "Первый модуль" {
		t.Fatal("selected version module is missing from lesson")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", p.ID), map[string]any{"name": "Изменённый каталог"}, 200)
	var completed lessons.Lesson
	decode(t, expect(t, admin, "POST", lessonPath(l.ID)+"/complete", nil, 200), &completed)
	if completed.ProgramName != p.Name || completed.ModuleName == nil || *completed.ModuleName != "Первый модуль" {
		t.Fatal("completion replaced the versioned names with catalog names")
	}
	if !reflect.DeepEqual(l.PlanSnapshot, completed.PlanSnapshot) {
		t.Fatal("completion changed the teaching plan")
	}
}

func TestM2PlanSelectionIsAudited(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Аудит плана", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Педагог"})
	start := time.Now().Add(24 * time.Hour)
	var l lessons.Lesson
	decode(t, expect(t, admin, "POST", "/lessons", map[string]any{
		"group_id": g.ID, "teacher_id": teacher.ID, "topic": "Первое занятие",
		"starts_at": start, "ends_at": start.Add(time.Hour),
	}, 201), &l)
	expect(t, admin, "PATCH", lessonPath(l.ID), map[string]any{"version_module_key": "module-1", "template_key": "lesson-1"}, 200)
	var fields []string
	if err := f.pool.QueryRow(context.Background(), `SELECT changed_fields FROM audit_logs WHERE action='lesson.updated' AND object_id=$1 ORDER BY id DESC LIMIT 1`, l.ID).Scan(&fields); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"version_module_key", "template_key", "plan_snapshot"} {
		found := false
		for _, field := range fields {
			found = found || field == expected
		}
		if !found {
			t.Fatalf("audit lost changed field %s: %v", expected, fields)
		}
	}
}
