package app_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/modules/curriculum"
	"aiq.local/core/internal/modules/lessons"
)

func TestM2DetailedDocumentRoundTripAndSizeLimit(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Подробная программа", "status": "active"})
	v := createVersion(t, f.methodologist, p.ID, nil)
	d := curriculumDocument("Программа с подробным конспектом")
	d.Sections[0].Modules[0].Lessons[0].Outline = strings.Repeat("я", 20000)
	d.Sections[0].Modules[0].Lessons[0].Materials = strings.Repeat("м", 20000)
	body, err := json.Marshal(curriculum.SaveInput{Revision: v.Revision, Document: d})
	if err != nil || len(body) <= 32<<10 {
		t.Fatal("fixture must exceed the ordinary request limit", err)
	}
	v = saveVersion(t, f.methodologist, v, d)
	var card curriculum.Card
	decode(t, expect(t, f.methodologist, "GET", versionPath(p.ID, v.ID), nil, 200), &card)
	if !reflect.DeepEqual(card.Document, d) {
		t.Fatal("detailed content was truncated")
	}
	// Stay within each individual text field's limit, but exceed the aggregate
	// request budget. An oversized save must not alter the persisted revision.
	large := curriculumDocument("Слишком большая программа")
	lesson := large.Sections[0].Modules[0].Lessons[0]
	lesson.Outline = strings.Repeat("я", 20000)
	large.Sections[0].Modules[0].Lessons = nil
	for i := 0; i < 30; i++ {
		next := lesson
		next.Key = strings.Repeat("a", i+1)
		large.Sections[0].Modules[0].Lessons = append(large.Sections[0].Modules[0].Lessons, next)
	}
	expect(t, f.methodologist, "PUT", versionPath(p.ID, v.ID), curriculum.SaveInput{Revision: v.Revision, Document: large}, 400)
	decode(t, expect(t, f.methodologist, "GET", versionPath(p.ID, v.ID), nil, 200), &card)
	if card.Revision != v.Revision || !reflect.DeepEqual(card.Document, d) {
		t.Fatal("oversized request partially saved the document")
	}
	v = versionAction(t, f.methodologist, v, "submit", "")
	versionAction(t, f.educationLeader, v, "publish", "")
}

func TestM2HistoricalCorrectionRetainsVersionModuleAndOriginalPlan(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "История M2", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Педагог"})
	start := time.Now().Add(-2 * time.Hour)
	var l lessons.Lesson
	decode(t, expect(t, admin, "POST", "/lessons", map[string]any{
		"group_id": g.ID, "teacher_id": teacher.ID, "topic": "Первое занятие",
		"starts_at": start, "ends_at": start.Add(time.Hour), "version_module_key": "module-1", "template_key": "lesson-1",
	}, 201), &l)
	expect(t, admin, "POST", lessonPath(l.ID)+"/complete", nil, 200)
	input := map[string]any{"program_name": "Название в истории", "module_name": "Уточнённый модуль", "topic": "Уточнённая тема", "reason": "Сверено с журналом"}
	var corrected lessons.Lesson
	decode(t, expect(t, admin, "POST", lessonPath(l.ID)+"/snapshot", input, 200), &corrected)
	if corrected.ModuleName == nil || *corrected.ModuleName != input["module_name"] || !reflect.DeepEqual(corrected.PlanSnapshot, l.PlanSnapshot) {
		t.Fatal("historical correction lost the version module or rewrote original plan")
	}
	input["module_name"] = nil
	expect(t, admin, "POST", lessonPath(l.ID)+"/snapshot", input, 400)
	var history lessons.SnapshotPage
	decode(t, expect(t, admin, "GET", lessonPath(l.ID)+"/snapshot-history", nil, 200), &history)
	if history.Total != 1 || history.Items[0].ModuleName == nil || *history.Items[0].ModuleName != "Первый модуль" {
		t.Fatal("previous version module missing from correction history")
	}
}

func TestM2CrossProgramPreviewDoesNotAssumeMatchingKeysAreEquivalent(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Первая программа", "status": "active"})
	q := programCreate(t, admin, map[string]any{"name": "Независимая программа", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	target := groupCreate(t, admin, q.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Педагог"})
	start := time.Now().Add(24 * time.Hour)
	expect(t, admin, "POST", "/lessons", map[string]any{
		"group_id": g.ID, "teacher_id": teacher.ID, "topic": "Занятие",
		"starts_at": start, "ends_at": start.Add(time.Hour), "version_module_key": "module-1", "template_key": "lesson-1",
	}, 201)
	var preview curriculum.Preview
	decode(t, expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer/preview", map[string]any{"target_version_id": target.ProgramVersionID}, 200), &preview)
	if len(preview.Lessons) != 1 || !preview.Lessons[0].NeedsMapping {
		t.Fatal("keys unique only within a document were treated as cross-program identities")
	}
}
