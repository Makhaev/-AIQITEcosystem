package app_test

import (
	"aiq.local/core/internal/modules/family"
	"context"
	"fmt"
	"strings"
	"testing"
)

func changeRequest(t *testing.T, b *browser, v family.Request, staff bool, action string, assignee *int64, body, reason string) family.Request {
	t.Helper()
	base := "/family/requests"
	if staff {
		base = "/family-requests"
	}
	var out family.Request
	decode(t, expect(t, b, "POST", fmt.Sprintf("%s/%d/actions", base, v.ID), family.RequestChange{Action: action, Revision: v.Revision, AssigneeID: assignee, Body: body, Reason: reason}, 200), &out)
	return out
}
func TestFamilyRequestsLifecyclePrivacyAttachmentsAndReopen(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	cu, curator := provision(t, f, admin, "request-curator@example.test", "curator")
	nextUser, next := provision(t, f, admin, "request-next@example.test", "curator")
	_, teacher := provision(t, f, admin, "request-teacher@example.test", "teacher")
	child := studentCreate(t, admin, map[string]any{"name": "Request child", "curator_id": cu.ID})
	pc := parentCreate(t, admin, map[string]any{"name": "First parent", "email": "request-parent@example.test"})
	linkParent(t, admin, child.ID, pc.ID, true)
	familyLink(t, admin, child.ID, pc.ID, true)
	parent := familyUser(t, f, admin, mail, "request-parent@example.test", "parent", pc.ID)
	pc2 := parentCreate(t, admin, map[string]any{"name": "Other parent", "email": "request-other@example.test"})
	linkParent(t, admin, child.ID, pc2.ID, false)
	familyLink(t, admin, child.ID, pc2.ID, true)
	other := familyUser(t, f, admin, mail, "request-other@example.test", "parent", pc2.ID)
	var v family.Request
	decode(t, expect(t, parent, "POST", "/family/requests", family.RequestInput{StudentID: &child.ID, Subject: "PRIVATE topic", Body: "Private opening message", Files: []family.RequestUpload{{Name: "evidence.txt", Data: []byte("Private attachment")}}}, 201), &v)
	own := fmt.Sprintf("/family/requests/%d", v.ID)
	staff := fmt.Sprintf("/family-requests/%d", v.ID)
	if v.Status != "new" || v.AuthorID == 0 || v.AssigneeID != nil || v.CreatedAt.IsZero() || len(v.Messages) != 1 || len(v.Messages[0].Files) != 1 {
		t.Fatalf("missing card fields: %+v", v)
	}
	fid := v.Messages[0].Files[0].ID
	download := fmt.Sprintf("%s/files/%d", own, fid)
	expect(t, parent, "GET", download, nil, 200)
	expect(t, other, "GET", download, nil, 404)
	expect(t, other, "GET", own, nil, 404)
	familyTotal(t, other, "/family/requests?search=PRIVATE", 0)
	// A second parent of the same child cannot mutate the author's thread,
	// even when the request ID and current revision are known.
	for _, action := range []string{"assign", "start", "message", "answer", "close", "reopen"} {
		in := family.RequestChange{Action: action, Revision: v.Revision, AssigneeID: &cu.ID, Reason: "Another parent's request"}
		if action == "message" || action == "answer" {
			in.Body = "Unauthorized message"
		}
		expect(t, other, "POST", own+"/actions", in, 404)
	}
	// Access to one thread must not permit fetching an attachment from another.
	var otherRequest family.Request
	decode(t, expect(t, other, "POST", "/family/requests", family.RequestInput{StudentID: &child.ID, Subject: "Other question", Body: "Separate correspondence"}, 201), &otherRequest)
	expect(t, other, "GET", fmt.Sprintf("/family/requests/%d/files/%d", otherRequest.ID, fid), nil, 404)
	expect(t, next, "GET", staff, nil, 404)
	expect(t, teacher, "GET", "/family-requests", nil, 403)
	expect(t, parent, "GET", staff, nil, 403)
	expect(t, curator, "POST", staff+"/actions", family.RequestChange{Action: "answer", Revision: v.Revision, Body: "Skip states"}, 403)
	expect(t, parent, "POST", own+"/actions", family.RequestChange{Action: "assign", Revision: v.Revision, AssigneeID: &cu.ID}, 403)
	expect(t, curator, "POST", staff+"/actions", family.RequestChange{Action: "assign", Revision: v.Revision, AssigneeID: &nextUser.ID}, 400)
	v = changeRequest(t, curator, v, true, "assign", &cu.ID, "", "")
	if v.Status != "assigned" {
		t.Fatal(v.Status)
	}
	stale := v.Revision
	v = changeRequest(t, curator, v, true, "start", nil, "", "")
	expect(t, parent, "POST", own+"/actions", family.RequestChange{Action: "message", Revision: stale, Body: "Stale"}, 409)
	v = changeRequest(t, parent, v, false, "message", nil, "Additional information", "")
	v = changeRequest(t, curator, v, true, "answer", nil, "First answer", "")
	if v.AnsweredAt == nil || v.Status != "answered" {
		t.Fatal("answer date/status")
	}
	firstAnswer := *v.AnsweredAt
	v = changeRequest(t, parent, v, false, "message", nil, "Follow-up after answer", "")
	if v.Status != "in_progress" {
		t.Fatal("follow-up status")
	}
	v = changeRequest(t, curator, v, true, "answer", nil, "Second answer", "")
	v = changeRequest(t, parent, v, false, "close", nil, "", "")
	if v.ClosedAt == nil || v.Status != "closed" {
		t.Fatal("close date/status")
	}
	expect(t, parent, "POST", own+"/actions", family.RequestChange{Action: "message", Revision: v.Revision, Body: "Closed message"}, 403)
	expect(t, parent, "POST", own+"/actions", family.RequestChange{Action: "reopen", Revision: v.Revision}, 400)
	v = changeRequest(t, parent, v, false, "reopen", nil, "", "Need clarification")
	if v.Status != "assigned" || v.ClosedAt != nil || v.AnsweredAt.Before(firstAnswer) || len(v.Messages) != 5 {
		t.Fatal("reopen lost history")
	}
	v = changeRequest(t, curator, v, true, "start", nil, "", "")
	v = changeRequest(t, curator, v, true, "answer", nil, "Third answer", "")
	v = changeRequest(t, curator, v, true, "close", nil, "", "")
	// A new curator does not inherit the old assignment silently.
	if _, err := f.pool.Exec(ctx, `UPDATE students SET curator_id=$2 WHERE id=$1`, child.ID, nextUser.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, curator, "GET", staff, nil, 404)
	expect(t, next, "GET", staff, nil, 200)
	v = changeRequest(t, parent, v, false, "reopen", nil, "", "New issue after transfer")
	if v.Status != "new" || v.AssigneeID != nil {
		t.Fatal("invalid assignee retained")
	}
	v = changeRequest(t, next, v, true, "assign", &nextUser.ID, "", "")
	// Failed audit must roll back both message and status/revision.
	if _, err := f.pool.Exec(ctx, `CREATE FUNCTION reject_request_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='family.request_message' THEN RAISE EXCEPTION 'test failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_request_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_request_audit()`); err != nil {
		t.Fatal(err)
	}
	expect(t, parent, "POST", own+"/actions", family.RequestChange{Action: "message", Revision: v.Revision, Body: "Must rollback", Files: []family.RequestUpload{{Name: "rollback.txt", Data: []byte("rollback")}}}, 500)
	var after family.Request
	decode(t, expect(t, parent, "GET", own, nil, 200), &after)
	if after.Revision != v.Revision || len(after.Messages) != len(v.Messages) {
		t.Fatal("partial transaction")
	}
	if _, err := f.pool.Exec(ctx, `DROP TRIGGER reject_request_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	familyLink(t, admin, child.ID, pc.ID, false)
	for _, p := range []string{own, download} {
		expect(t, parent, "GET", p, nil, 404)
	}
	familyTotal(t, parent, "/family/requests?search=PRIVATE", 0)
	expect(t, parent, "POST", own+"/actions", family.RequestChange{Action: "message", Revision: v.Revision, Body: "Revoked"}, 404)
	for _, table := range []string{"family_request_messages", "family_request_events", "family_request_files"} {
		if _, err := f.pool.Exec(ctx, `DELETE FROM `+table); err == nil {
			t.Fatal("mutable history", table)
		}
	}
	var audit string
	if err := f.pool.QueryRow(ctx, `SELECT string_agg(action,',') FROM audit_logs WHERE object_type='family_request' AND object_id=$1`, v.ID).Scan(&audit); err != nil || !strings.Contains(audit, "family.request_reopen") {
		t.Fatal("audit missing", err)
	}
}

func TestFamilyRequestsWithoutChildAndAttachmentValidation(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	_, curator := provision(t, f, admin, "general-curator@example.test", "curator")
	pc := parentCreate(t, admin, map[string]any{"name": "Parent without child", "email": "general-parent@example.test"})
	parent := familyUser(t, f, admin, mail, "general-parent@example.test", "parent", pc.ID)
	var me struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, admin, "GET", "/auth/me", nil, 200), &me)
	var v family.Request
	decode(t, expect(t, parent, "POST", "/family/requests", family.RequestInput{Subject: "General question", Body: "No child", Files: []family.RequestUpload{{Name: "question.txt", Data: []byte("Question")}}}, 201), &v)
	if v.StudentID != nil {
		t.Fatal("invented child")
	}
	familyTotal(t, parent, "/family/requests", 1)
	familyTotal(t, curator, "/family-requests", 0)
	p := fmt.Sprintf("/family-requests/%d", v.ID)
	expect(t, curator, "GET", p, nil, 404)
	v = changeRequest(t, admin, v, true, "assign", &me.ID, "", "")
	v = changeRequest(t, admin, v, true, "start", nil, "", "")
	v = changeRequest(t, admin, v, true, "answer", nil, "General answer", "")
	v = changeRequest(t, admin, v, true, "close", nil, "", "")
	v = changeRequest(t, admin, v, true, "reopen", nil, "", "Administrative clarification")
	if v.Status != "assigned" {
		t.Fatal(v.Status)
	}
	for _, in := range []family.RequestInput{
		{Subject: "Bad", Body: "Bad", Files: []family.RequestUpload{{Name: "../escape.txt", Data: []byte("x")}}},
		{Subject: "Bad", Body: "Bad", Files: []family.RequestUpload{{Name: "empty.txt"}}},
		{Subject: "Bad", Body: "Bad", StudentID: new(int64)},
	} {
		expect(t, parent, "POST", "/family/requests", in, 400)
	}
	expect(t, parent, "POST", "/family/requests", family.RequestInput{Subject: "Bad", Body: "Bad", Files: []family.RequestUpload{{Name: "script.html", Data: []byte("<html><script>alert(1)</script></html>")}}}, 415)
	familyTotal(t, parent, "/family/requests", 1)
}
