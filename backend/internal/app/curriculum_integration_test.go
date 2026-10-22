package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/modules/curriculum"
	"aiq.local/core/internal/modules/programs"
)

func curriculumDocument(name string) curriculum.Document {
	return curriculum.Document{Name: name, Description: "Учебная программа", Sections: []curriculum.Section{{
		Key: "section-1", Name: "Основы", Modules: []curriculum.Module{{
			Key: "module-1", Name: "Первый модуль", Lessons: []curriculum.Template{{
				Key: "lesson-1", Topic: "Первое занятие", Goals: "Освоить основы", DurationMinutes: 45,
				Outline: "Объяснение и практика", Exercises: "Самостоятельная работа",
				Equipment: "Компьютер", Materials: "Конспект преподавателя", Criteria: "Выполненное упражнение",
			}},
		}},
	}}}
}
func versionPath(programID, id int64) string {
	return fmt.Sprintf("/programs/%d/versions/%d", programID, id)
}
func createVersion(t *testing.T, b *browser, p int64, source *int64) curriculum.Version {
	t.Helper()
	var v curriculum.Version
	decode(t, expect(t, b, "POST", fmt.Sprintf("/programs/%d/versions", p), curriculum.CreateInput{SourceVersionID: source}, 201), &v)
	return v
}
func saveVersion(t *testing.T, b *browser, v curriculum.Version, d curriculum.Document) curriculum.Version {
	t.Helper()
	var out curriculum.Version
	decode(t, expect(t, b, "PUT", versionPath(v.ProgramID, v.ID), curriculum.SaveInput{Revision: v.Revision, Document: d}, 200), &out)
	return out
}
func versionAction(t *testing.T, b *browser, v curriculum.Version, action, comment string) curriculum.Version {
	t.Helper()
	var out curriculum.Version
	decode(t, expect(t, b, "POST", versionPath(v.ProgramID, v.ID)+"/actions", curriculum.ActionInput{Revision: v.Revision, Action: action, Comment: comment}, 200), &out)
	return out
}

// Core fixtures also have a real approved version. This is fixture preparation,
// using the same HTTP role/transition rules as the product, not a production
// auto-publication path.
func ensureFixtureVersion(t *testing.T, b *browser, p programs.Program) {
	t.Helper()
	f := b.fixture
	if f.methodologist == nil {
		_, f.methodologist = provision(t, f, b, "fixture-methodologist@example.test", "methodologist")
		_, f.educationLeader = provision(t, f, b, "fixture-education-leader@example.test", "education_leader")
	}
	v := createVersion(t, f.methodologist, p.ID, nil)
	v = saveVersion(t, f.methodologist, v, curriculumDocument(p.Name))
	v = versionAction(t, f.methodologist, v, "submit", "")
	versionAction(t, f.educationLeader, v, "publish", "")
}

func TestM2VersionWorkflowRolesAndImmutability(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	authorUser, author := provision(t, f, admin, "author@example.test", "methodologist")
	_, reviewer := provision(t, f, admin, "reviewer@example.test", "education_leader")
	var p programs.Program
	decode(t, expect(t, author, "POST", "/programs", map[string]any{"name": "Версионная программа", "status": "active"}, 201), &p)
	expect(t, admin, "POST", fmt.Sprintf("/programs/%d/versions", p.ID), map[string]any{}, 403)
	v := createVersion(t, author, p.ID, nil)
	expect(t, reviewer, "PUT", versionPath(p.ID, v.ID), curriculum.SaveInput{Revision: v.Revision, Document: curriculumDocument("Версия 1")}, 403)
	v = saveVersion(t, author, v, curriculumDocument("Версия 1"))
	expect(t, author, "PUT", versionPath(p.ID, v.ID), curriculum.SaveInput{Revision: 1, Document: v.Document}, 409)
	v = versionAction(t, author, v, "submit", "")
	expect(t, reviewer, "POST", versionPath(p.ID, v.ID)+"/actions", curriculum.ActionInput{Revision: v.Revision, Action: "return"}, 400)
	v = versionAction(t, reviewer, v, "return", "Добавить упражнение")
	v = saveVersion(t, author, v, v.Document)
	v = versionAction(t, author, v, "submit", "")
	// Grant both roles and log in again after role changes revoke the session.
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", authorUser.ID), map[string]any{"roles": []string{"methodologist", "education_leader"}}, 200)
	author = f.browser(t)
	author.login(t, "author@example.test", "TestOnly-New-Account-9372")
	expect(t, author, "POST", versionPath(p.ID, v.ID)+"/actions", curriculum.ActionInput{Revision: v.Revision, Action: "publish"}, 403)
	v = versionAction(t, reviewer, v, "publish", "Утверждено")
	expect(t, author, "PUT", versionPath(p.ID, v.ID), curriculum.SaveInput{Revision: v.Revision, Document: v.Document}, 409)
	_, err := f.pool.Exec(context.Background(), `UPDATE program_versions SET document=jsonb_set(document,'{name}','"Changed"') WHERE id=$1`, v.ID)
	dbConstraint(t, err, "23514", "program_versions_immutable")
	_, err = f.pool.Exec(context.Background(), `UPDATE program_versions SET status='draft' WHERE id=$1`, v.ID)
	dbConstraint(t, err, "23514", "program_versions_transition")
	clone := createVersion(t, author, p.ID, &v.ID)
	if clone.Number != 2 || clone.Status != "draft" || clone.Document.Sections[0].Modules[0].Lessons[0].Key != "lesson-1" {
		t.Fatalf("clone: %+v", clone)
	}
	var card curriculum.Card
	decode(t, expect(t, reviewer, "GET", versionPath(p.ID, v.ID), nil, 200), &card)
	found := false
	for _, event := range card.Events {
		if event.Action == "return" && event.Comment == "Добавить упражнение" {
			found = true
		}
	}
	if !found {
		t.Fatal("return comment lost")
	}
	versionAction(t, reviewer, v, "archive", "")
}

func TestM2GroupTransferPreservesPastAndRejectsStalePreview(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Программа", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Педагог"})
	newLesson := func(start time.Time) int64 {
		var result struct {
			ID int64 `json:"id"`
		}
		decode(t, expect(t, admin, "POST", "/lessons", map[string]any{"group_id": g.ID, "teacher_id": teacher.ID, "topic": "Тема", "starts_at": start, "ends_at": start.Add(time.Hour), "version_module_key": "module-1", "template_key": "lesson-1"}, 201), &result)
		return result.ID
	}
	past := newLesson(time.Now().Add(-48 * time.Hour))
	future := newLesson(time.Now().Add(48 * time.Hour))
	v := createVersion(t, f.methodologist, p.ID, &g.ProgramVersionID)
	d := v.Document
	d.Sections[0].Modules[0].Lessons[0].Outline = "Новый конспект"
	v = saveVersion(t, f.methodologist, v, d)
	v = versionAction(t, f.methodologist, v, "submit", "")
	v = versionAction(t, f.educationLeader, v, "publish", "")
	if groupCard(t, admin, g.ID).ProgramVersionID != g.ProgramVersionID {
		t.Fatal("publication moved group")
	}
	previewPath := groupPath(g.ID) + "/program-transfer/preview"
	var preview curriculum.Preview
	decode(t, expect(t, admin, "POST", previewPath, map[string]any{"target_version_id": v.ID}, 200), &preview)
	if len(preview.Lessons) != 1 || preview.Lessons[0].ID != future {
		t.Fatalf("future selection: %+v", preview.Lessons)
	}
	mappings := []curriculum.Mapping{preview.Lessons[0].Suggested}
	in := curriculum.TransferInput{TargetVersionID: v.ID, PreviewToken: preview.Token, Reason: "Новая редакция", Mappings: mappings}
	expect(t, admin, "PATCH", fmt.Sprintf("/lessons/%d", future), map[string]any{"location": "Новый кабинет"}, 200)
	expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer", in, 409)
	decode(t, expect(t, admin, "POST", previewPath, map[string]any{"target_version_id": v.ID}, 200), &preview)
	in.PreviewToken = preview.Token
	invalid := in
	invalid.Mappings = []curriculum.Mapping{{LessonID: future, ModuleKey: "foreign", TemplateKey: "lesson-1"}}
	expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer", invalid, 400)
	var rec curriculum.TransferRecord
	decode(t, expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer", in, 200), &rec)
	if !strings.Contains(string(rec.PreviousLessons), "Объяснение и практика") || !strings.Contains(string(rec.ResultingLessons), "Новый конспект") {
		t.Fatal("transfer snapshots missing")
	}
	var pastVersion, futureVersion int64
	if err := f.pool.QueryRow(context.Background(), `SELECT program_version_id FROM lessons WHERE id=$1`, past).Scan(&pastVersion); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT program_version_id FROM lessons WHERE id=$1`, future).Scan(&futureVersion); err != nil {
		t.Fatal(err)
	}
	if pastVersion != g.ProgramVersionID || futureVersion != v.ID {
		t.Fatal("historical version changed")
	}
	_, err := f.pool.Exec(context.Background(), `UPDATE lessons SET plan_snapshot='{}' WHERE id=$1`, past)
	dbConstraint(t, err, "23514", "lessons_plan_immutable")
}
