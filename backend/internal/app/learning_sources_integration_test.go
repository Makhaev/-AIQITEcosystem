package app_test

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/modules/learning"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func learningCreate(t *testing.T, b *browser, sid int64, in learning.Input) learning.Card {
	t.Helper()
	var r learning.Card
	decode(t, expect(t, b, "POST", fmt.Sprintf("/students/%d/education", sid), in, 200), &r)
	return r
}
func learningAction(t *testing.T, b *browser, r learning.Card, action string, refs ...learning.Reference) learning.Card {
	t.Helper()
	var out learning.Card
	decode(t, expect(t, b, "POST", fmt.Sprintf("/learning/%d/actions", r.ID), learning.ActionInput{Revision: r.Revision, Action: action, Reason: "Verified decision", Sources: refs}, 200), &out)
	return out
}
func learningList(t *testing.T, b *browser, sid int64, kind, query string) learning.Block {
	t.Helper()
	var out learning.Block
	decode(t, expect(t, b, "GET", fmt.Sprintf("/students/%d/education/%s%s", sid, kind, query), nil, 200), &out)
	return out
}

func TestStudent360LearningSourcesObjectACLAndEvidence(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -time.Hour, time.Hour)
	ctx := context.Background()
	var adminID, teacherUserID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT id FROM users WHERE email=$1`, a.f.admin.Email).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherUserID); err != nil {
		t.Fatal(err)
	}
	today := time.Now().In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02")
	in := learning.Input{Kind: "observations", LessonID: &l.ID, ReviewerID: &adminID, Content: learning.Content{Title: "Allowed observed result", Fact: "Explained own solution", Interpretation: "Possible understanding", Context: "Own lesson", OccurredOn: today, ModelVersion: "communication-v1", SkillKey: "explain", SkillName: "Explanation"}}
	obs := learningCreate(t, a.own, a.firstID, in)
	if got := learningList(t, a.own, a.firstID, "observations", ""); got.Total != 0 {
		t.Fatal("draft entered timeline")
	}
	obs = learningAction(t, a.own, obs, "submit")
	expect(t, a.own, "POST", fmt.Sprintf("/learning/%d/actions", obs.ID), learning.ActionInput{Revision: obs.Revision, Action: "approve"}, 403)
	obs = learningAction(t, a.admin, obs, "approve")
	obs = learningAction(t, a.admin, obs, "publish")
	expect(t, a.admin, "POST", fmt.Sprintf("/learning/%d/actions", obs.ID), learning.ActionInput{Revision: 1, Action: "unpublish", Reason: "stale"}, 409)
	if got := learningList(t, a.own, a.firstID, "observations", ""); got.Total != 1 || got.Items[0].Content.Interpretation != in.Content.Interpretation {
		t.Fatalf("timeline: %+v", got)
	}
	oldPeriod := "?education_from=2001-01-01&education_to=2001-01-31"
	if got := learningList(t, a.own, a.firstID, "observations", oldPeriod); got.Total != 0 {
		t.Fatal("observation date filter ignored")
	}
	if got := learningList(t, a.own, a.firstID, "skills", oldPeriod); got.Total != 1 || got.Items[0].State != "no_methodology" || !got.Items[0].Sources[0].OutsidePeriod {
		t.Fatal("skill chronology fabricated a level or used period as completeness")
	}
	// A supervisor record on the same pupil grants no teacher access by itself.
	hiddenInput := in
	hiddenInput.LessonID = nil
	hiddenInput.Content.Title = "PRIVATE SOURCE NAME"
	hidden := learningCreate(t, a.admin, a.firstID, hiddenInput)
	hidden = learningAction(t, a.admin, hidden, "submit")
	// A different authorized reviewer is required; authors cannot self-approve.
	expect(t, a.admin, "POST", fmt.Sprintf("/learning/%d/actions", hidden.ID), learning.ActionInput{Revision: hidden.Revision, Action: "approve"}, 403)
	expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", hidden.ID), nil, 404)
	if _, err := a.f.pool.Exec(ctx, `UPDATE learning_records SET reviewer_id=$2 WHERE id=$1`, hidden.ID, teacherUserID); err != nil {
		t.Fatal(err)
	}
	hidden = learningAction(t, a.own, hidden, "approve")
	hidden = learningAction(t, a.own, hidden, "publish")
	if _, err := a.f.pool.Exec(ctx, `UPDATE learning_records SET reviewer_id=$2 WHERE id=$1`, hidden.ID, adminID); err != nil {
		t.Fatal(err)
	}
	var file files.File
	decode(t, expect(t, a.admin, "POST", "/files", files.Input{StudentID: a.firstID, Name: "PRIVATE FILE.txt", Data: []byte("Private evidence")}, 201), &file)
	project := learningCreate(t, a.admin, a.firstID, learning.Input{Kind: "projects", LessonID: &l.ID, ReviewerID: &teacherUserID, Content: learning.Content{Title: "Personal project contribution", StartsOn: "2000-01-01", Contribution: "Implemented parser", Criteria: "Handles input"}, Sources: []learning.Reference{{ID: hidden.ID, Revision: hidden.Revision}}, FileIDs: []int64{file.ID}})
	// Parent permission does not disclose denied source IDs, titles, versions or file metadata.
	var visible learning.Card
	raw := expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", project.ID), nil, 200)
	decode(t, raw, &visible)
	if !visible.BasisUnavailable || len(visible.Sources) != 0 || len(visible.Files) != 0 || strings.Contains(string(raw), "PRIVATE") {
		t.Fatalf("nested leak: %s", raw)
	}
	project = learningAction(t, a.admin, project, "submit")
	project = learningAction(t, a.own, project, "approve")
	achievement := learningCreate(t, a.admin, a.firstID, learning.Input{Kind: "achievements", ReviewerID: &teacherUserID, Content: learning.Content{Title: "Parser completed", OccurredOn: today}, Sources: []learning.Reference{{ID: project.ID, Revision: project.Revision}}})
	achievement = learningAction(t, a.own, achievement, "approve")
	project = learningAction(t, a.own, project, "withdraw")
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", achievement.ID), nil, 200), &visible)
	if !visible.NeedsReview || len(visible.Sources) != 1 {
		t.Fatal("withdrawn basis not identified")
	}
	// Observation correction preserves historical content and removes stale publication.
	in.Revision = obs.Revision
	in.Reason = "Correct fact"
	in.Content.Fact = "Corrected observation"
	var corrected learning.Card
	decode(t, expect(t, a.own, "PUT", fmt.Sprintf("/learning/%d", obs.ID), in, 200), &corrected)
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/learning/%d?revision=%d", obs.ID, obs.Revision), nil, 200), &visible)
	if visible.Content.Fact != "Explained own solution" || len(visible.Actions) != 0 {
		t.Fatal("historical revision changed")
	}
	if got := learningList(t, a.own, a.firstID, "skills", ""); got.Total != 0 {
		t.Fatal("unpublished observation still determined skill")
	}
	// Self-review follows the author of corrected content, not only the creator.
	in.Revision = corrected.Revision
	decode(t, expect(t, a.admin, "PUT", fmt.Sprintf("/learning/%d", obs.ID), in, 200), &corrected)
	corrected = learningAction(t, a.admin, corrected, "submit")
	expect(t, a.admin, "POST", fmt.Sprintf("/learning/%d/actions", obs.ID), learning.ActionInput{Revision: corrected.Revision, Action: "approve"}, 403)
	in.Revision = corrected.Revision
	decode(t, expect(t, a.own, "PUT", fmt.Sprintf("/learning/%d", obs.ID), in, 200), &corrected)
	// SQL-level guards and audit transaction rollback.
	if _, err := a.f.pool.Exec(ctx, `UPDATE learning_revisions SET content='{}' WHERE record_id=$1`, obs.ID); err == nil {
		t.Fatal("mutable historical content")
	}
	if _, err := a.f.pool.Exec(ctx, `CREATE FUNCTION reject_learning_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action LIKE 'learning.%' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_learning_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_learning_audit()`); err != nil {
		t.Fatal(err)
	}
	expect(t, a.own, "POST", fmt.Sprintf("/learning/%d/actions", corrected.ID), learning.ActionInput{Revision: corrected.Revision, Action: "submit"}, 500)
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", corrected.ID), nil, 200), &visible)
	if visible.Revision != corrected.Revision || visible.State != "draft" {
		t.Fatal("audit failure committed state")
	}
	// An independent remaining lesson keeps Core access, but replacing the
	// source lesson teacher removes this educational object's scope.
	a.lesson(t, 2*time.Hour, 3*time.Hour)
	foreignTeacher := teacherCreate(t, a.admin, map[string]any{"name": "Replacement", "user_id": a.foreignUserID})
	if _, err := a.f.pool.Exec(ctx, `UPDATE lessons SET teacher_id=$2 WHERE id=$1`, l.ID, foreignTeacher.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.own, "GET", fmt.Sprintf("/students/%d/overview", a.firstID), nil, 200)
	expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", obs.ID), nil, 404)
}

func TestStudent360GoalsCuratorReplacementAndAssignmentAttempts(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -time.Hour, time.Hour)
	ctx := context.Background()
	cu, cb := provision(t, a.f, a.admin, "learning-curator@example.test", "curator")
	next, _ := provision(t, a.f, a.admin, "learning-next@example.test", "curator")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": cu.ID}, 200)
	var teacherID, versionID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	if err := a.f.pool.QueryRow(ctx, `SELECT program_version_id FROM groups WHERE id=$1`, a.groupID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	goalIn := learning.Input{Kind: "goals", Content: learning.Content{Title: "Explain the solution", DueOn: tomorrow, ModelVersion: "v1", SkillKey: "explain"}}
	goal := learningCreate(t, cb, a.firstID, goalIn)
	goal = learningAction(t, cb, goal, "activate")
	expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", goal.ID), nil, 404)
	expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/education", a.firstID), goalIn, 403)
	late := true
	assignment := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "assignments", LessonID: &l.ID, ReviewerID: &teacherID, Content: learning.Content{Title: "Individual assignment", Instruction: "Explain your answer", Criteria: "Reason given", DueOn: tomorrow, ProgramVersionID: &versionID, TemplateKey: "lesson-1", Format: "text", AllowLate: &late}})
	attempt := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "attempts", ParentID: &assignment.ID, Content: learning.Content{Title: "First response", Answer: "My solution", Sender: "Student through parent", Channel: "In person"}})
	attempt = learningAction(t, a.own, attempt, "approve")
	if got := learningList(t, a.own, a.firstID, "assignments", "?current=true"); got.Total != 0 {
		t.Fatal("accepted assignment still open")
	}
	// Explicit source grant never grants family access or curator mutation authority.
	if _, err := a.f.pool.Exec(ctx, `INSERT INTO learning_grants VALUES($1,$2)`, attempt.ID, cu.ID); err != nil {
		t.Fatal(err)
	}
	goal = learningAction(t, cb, goal, "achieve", learning.Reference{ID: attempt.ID, Revision: attempt.Revision})
	attempt = learningAction(t, a.own, attempt, "withdraw")
	if got := learningList(t, cb, a.firstID, "goals", ""); got.Total != 1 || !got.Items[0].NeedsReview {
		t.Fatal("goal ignored withdrawn evidence")
	}
	if got := learningList(t, a.own, a.firstID, "assignments", "?current=true"); got.Total != 1 {
		t.Fatal("withdrawal did not reopen assignment")
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": next.ID}, 200)
	expect(t, cb, "GET", fmt.Sprintf("/learning/%d", goal.ID), nil, 404)
	expect(t, a.foreign, "GET", fmt.Sprintf("/students/%d/education/projects", a.firstID), nil, 404)
	// More than one page; scope is applied before totals and pagination.
	for i := 0; i < 21; i++ {
		learningCreate(t, a.admin, a.firstID, learning.Input{Kind: "projects", ReviewerID: &teacherID, Content: learning.Content{Title: fmt.Sprintf("Visible contribution %d", i), StartsOn: "2000-01-01", Contribution: "Individual", Criteria: "Reviewed"}})
	}
	got := learningList(t, a.own, a.firstID, "projects", "?page=2")
	if got.Total != 21 || len(got.Items) != 1 {
		t.Fatalf("pagination: %+v", got)
	}
}
