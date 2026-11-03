package app_test

import (
	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/files"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func reportAction(t *testing.T, b *browser, v family.Report, action, reason string) family.Report {
	t.Helper()
	version := v.Version
	if action == "withdraw" && v.PublishedVersion != nil {
		version = *v.PublishedVersion
	}
	var out family.Report
	decode(t, expect(t, b, "POST", fmt.Sprintf("/family-reports/%d/actions", v.ID), family.ReportAction{Action: action, Version: version, Revision: v.Revision, Reason: reason}, 200), &out)
	return out
}
func publishFamilyReport(t *testing.T, f *fixture, admin, author *browser, v family.Report) family.Report {
	t.Helper()
	reviewer, b := provision(t, f, admin, fmt.Sprintf("report-review-%d-%d@example.test", v.ID, v.Revision), "education_leader")
	in := family.ReportInput{Title: v.Title, Body: v.Body, ReviewerID: &reviewer.ID, NextFocus: v.NextFocus, Revision: v.Revision}
	if v.Snapshot != nil {
		in.From = v.Snapshot.From
		in.To = v.Snapshot.To
		if in.NextFocus == "" {
			in.NextFocus = "Next learning focus"
		}
	}
	for _, file := range v.Files {
		in.FileIDs = append(in.FileIDs, file.ID)
	}
	decode(t, expect(t, author, "PUT", fmt.Sprintf("/family-reports/%d", v.ID), in, 200), &v)
	v = reportAction(t, author, v, "submit", "")
	v = reportAction(t, b, v, "approve", "")
	return reportAction(t, b, v, "publish", "")
}

func TestFamilyReportWorkflowVersionsPermissionsNotificationsAndAudit(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	cu, curator := provision(t, f, admin, "workflow-curator@example.test", "curator")
	reviewer, review := provision(t, f, admin, "workflow-review@example.test", "education_leader")
	_, foreign := provision(t, f, admin, "workflow-other-review@example.test", "education_leader")
	_, leader := provision(t, f, admin, "workflow-leader@example.test", "leader")
	child := studentCreate(t, admin, map[string]any{"name": "Workflow child", "curator_id": cu.ID})
	pupil := familyUser(t, f, admin, mail, "workflow-child@example.test", "student", child.ID)
	parentCard := parentCreate(t, admin, map[string]any{"name": "Workflow parent", "email": "workflow-parent@example.test"})
	linkParent(t, admin, child.ID, parentCard.ID, true)
	parent := familyUser(t, f, admin, mail, "workflow-parent@example.test", "parent", parentCard.ID)
	familyLink(t, admin, child.ID, parentCard.ID, true)
	base := fmt.Sprintf("/students/%d/family-reports", child.ID)
	var file1, file2 files.File
	for i, p := range []*files.File{&file1, &file2} {
		decode(t, expect(t, curator, "POST", "/files", files.Input{StudentID: child.ID, Name: fmt.Sprintf("version-%d.txt", i), Data: []byte(fmt.Sprintf("version %d", i))}, 201), p)
		expect(t, curator, "PUT", fmt.Sprintf("/files/%d/family-access", p.ID), family.Audience{Students: true, Parents: true}, 204)
	}
	in := family.ReportInput{Title: "Published title", Body: "Version one family text", ReviewerID: &reviewer.ID, FileIDs: []int64{file1.ID}}
	var v family.Report
	decode(t, expect(t, curator, "POST", base, in, 201), &v)
	path := fmt.Sprintf("/family-reports/%d", v.ID)
	public := fmt.Sprintf("/family/reports/%d", v.ID)
	expect(t, review, "GET", path, nil, 404)
	expect(t, foreign, "GET", path, nil, 404)
	expect(t, leader, "GET", "/family-reports", nil, 403)
	expect(t, review, "GET", "/students", nil, 403)
	deny := func(b *browser, action string, status int) {
		t.Helper()
		expect(t, b, "POST", path+"/actions", family.ReportAction{Action: action, Version: v.Version, Revision: v.Revision, Reason: "Reason"}, status)
	}
	deny(curator, "publish", 403)
	deny(admin, "approve", 403)
	yes := true
	expect(t, admin, "POST", path+"/publication", family.Publication{Version: v.Version, Revision: v.Revision, Published: &yes}, 403)
	expect(t, curator, "POST", path+"/actions", family.ReportAction{Action: "submit", Version: v.Version, Revision: v.Revision, Reason: strings.Repeat("я", 5001)}, 400)
	v = reportAction(t, curator, v, "submit", "")
	familyTotal(t, review, "/family-reports", 1)
	familyTotal(t, foreign, "/family-reports", 0)
	expect(t, review, "GET", path, nil, 200)
	expect(t, review, "GET", fmt.Sprintf("/files/%d/content", file1.ID), nil, 403)
	in.Revision = v.Revision
	expect(t, curator, "PUT", path, in, 409)
	deny(curator, "approve", 403)
	deny(foreign, "approve", 404)
	expect(t, review, "POST", path+"/actions", family.ReportAction{Action: "return", Version: v.Version, Revision: v.Revision}, 400)
	v = reportAction(t, review, v, "return", "Explain next steps clearly")
	expect(t, review, "GET", path, nil, 404)
	decode(t, expect(t, curator, "GET", path, nil, 200), &v)
	if len(v.Decisions) != 2 || v.Decisions[1].Reason != "Explain next steps clearly" {
		t.Fatal("return comment lost")
	}
	v = reportAction(t, curator, v, "submit", "")
	v = reportAction(t, review, v, "approve", "")
	if v.State != "approved" || v.ApprovedBy == nil || *v.ApprovedBy != reviewer.ID {
		t.Fatal("approval metadata")
	}
	expect(t, parent, "GET", public, nil, 404)
	if _, err := f.pool.Exec(ctx, `UPDATE family_reports SET body='mutated' WHERE id=$1`, v.ID); err == nil {
		t.Fatal("approved text mutable in DB")
	}
	publishIn := family.ReportAction{Action: "publish", Version: v.Version, Revision: v.Revision}
	// Two identical concurrent publications commit only one notification per user.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _ := review.do(t, "POST", path+"/actions", publishIn)
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != 200 {
			t.Fatalf("repeat publish=%d", s)
		}
	}
	decode(t, expect(t, curator, "GET", path, nil, 200), &v)
	firstVersion := v.Version
	for _, b := range []*browser{parent, pupil} {
		raw := expect(t, b, "GET", public, nil, 200)
		if strings.Contains(string(raw), "Explain next steps") || strings.Contains(string(raw), "decisions") {
			t.Fatal("private review leaked")
		}
		if string(expect(t, b, "GET", public+fmt.Sprintf("/files/%d", file1.ID), nil, 200)) != "version 0" {
			t.Fatal("wrong edition file")
		}
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM family_report_notifications WHERE report_id=$1`, v.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("notifications=%d %v", count, err)
	}
	v = reportAction(t, curator, v, "correct", "")
	in.Revision = v.Revision
	in.Title = "SECRET NEW TITLE"
	in.Body = "New version text"
	in.FileIDs = []int64{file2.ID}
	decode(t, expect(t, curator, "PUT", path, in, 200), &v)
	raw := expect(t, parent, "GET", public, nil, 200)
	if strings.Contains(string(raw), "SECRET") || !strings.Contains(string(raw), "Version one") {
		t.Fatal("draft replaced family publication")
	}
	familyTotal(t, parent, fmt.Sprintf("/family/students/%d/reports?search=SECRET", child.ID), 0)
	familyTotal(t, parent, fmt.Sprintf("/family/students/%d/reports?search=Published", child.ID), 1)
	expect(t, parent, "GET", public+fmt.Sprintf("/files/%d", file1.ID), nil, 200)
	expect(t, parent, "GET", public+fmt.Sprintf("/files/%d", file2.ID), nil, 404)
	var reviewerView family.Report
	decode(t, expect(t, review, "GET", path, nil, 200), &reviewerView)
	if reviewerView.Version != firstVersion || strings.Contains(reviewerView.Body, "New version") {
		t.Fatal("reviewer saw unsubmitted correction")
	}
	v = reportAction(t, curator, v, "submit", "")
	v = reportAction(t, review, v, "approve", "")
	v = reportAction(t, review, v, "publish", "")
	expect(t, review, "POST", path+"/actions", publishIn, 409)
	expect(t, parent, "GET", public+fmt.Sprintf("/files/%d", file1.ID), nil, 404)
	expect(t, parent, "GET", public+fmt.Sprintf("/files/%d", file2.ID), nil, 200)
	notifications := fmt.Sprintf("/family/students/%d/notifications", child.ID)
	var ns []family.ReportNotification
	decode(t, expect(t, parent, "GET", notifications, nil, 200), &ns)
	if len(ns) != 1 || ns[0].Version != v.Version {
		t.Fatal("notification wrong version")
	}
	deny(curator, "withdraw", 403)
	v = reportAction(t, review, v, "withdraw", "Incorrect period")
	expect(t, parent, "GET", public, nil, 404)
	expect(t, parent, "GET", public+fmt.Sprintf("/files/%d", file2.ID), nil, 404)
	decode(t, expect(t, parent, "GET", notifications, nil, 200), &ns)
	if len(ns) != 0 {
		t.Fatal("withdrawn notification visible")
	}
	for action, want := range map[string]int{"family.report_publish": 2, "family.report_approve": 2, "family.report_withdraw": 1} {
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_id=$1 AND action=$2`, v.ID, action).Scan(&count); err != nil || count != want {
			t.Fatalf("audit %s=%d %v", action, count, err)
		}
	}
	familyLink(t, admin, child.ID, parentCard.ID, false)
	expect(t, parent, "GET", notifications, nil, 404)
}

func TestFamilyReportWorkflowSelfApprovalRevocationAndAtomicity(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	reviewer, review := provision(t, f, admin, "atomic-review@example.test", "education_leader")
	cu, curator := provision(t, f, admin, "atomic-curator@example.test", "curator")
	child := studentCreate(t, admin, map[string]any{"name": "Atomic child", "curator_id": cu.ID})
	pupil := familyUser(t, f, admin, mail, "atomic-pupil@example.test", "student", child.ID)
	in := family.ReportInput{Title: "Report", Body: "First", ReviewerID: &reviewer.ID}
	var v family.Report
	decode(t, expect(t, curator, "POST", fmt.Sprintf("/students/%d/family-reports", child.ID), in, 201), &v)
	path := fmt.Sprintf("/family-reports/%d", v.ID)
	// An administrator who edits somebody else's draft becomes an editor too.
	in.Revision = v.Revision
	in.Body = "Edited by admin"
	decode(t, expect(t, admin, "PUT", path, in, 200), &v)
	v = reportAction(t, curator, v, "submit", "")
	expect(t, admin, "POST", path+"/actions", family.ReportAction{Action: "approve", Version: v.Version, Revision: v.Revision}, 403)
	// Live role checks must not rely on the session's former reviewer role.
	if _, err := f.pool.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1 AND role='education_leader'`, reviewer.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, review, "POST", path+"/actions", family.ReportAction{Action: "approve", Version: v.Version, Revision: v.Revision}, 403)
	if _, err := f.pool.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'education_leader')`, reviewer.ID); err != nil {
		t.Fatal(err)
	}
	v = reportAction(t, review, v, "approve", "")
	if _, err := f.pool.Exec(ctx, `CREATE FUNCTION fail_report_publish_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='family.report_publish' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_report_publish_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_report_publish_audit()`); err != nil {
		t.Fatal(err)
	}
	input := family.ReportAction{Action: "publish", Version: v.Version, Revision: v.Revision}
	expect(t, review, "POST", path+"/actions", input, 500)
	var state string
	var published bool
	var publications, notifications, revision int
	if err := f.pool.QueryRow(ctx, `SELECT state,published,revision,(SELECT count(*) FROM family_report_publications WHERE report_id=$1),(SELECT count(*) FROM family_report_notifications WHERE report_id=$1) FROM family_reports WHERE id=$1`, v.ID).Scan(&state, &published, &revision, &publications, &notifications); err != nil {
		t.Fatal(err)
	}
	if state != "approved" || published || revision != v.Revision || publications != 0 || notifications != 0 {
		t.Fatal("audit failure did not roll back all publication effects")
	}
	if _, err := f.pool.Exec(ctx, `DROP TRIGGER fail_report_publish_audit ON audit_logs; DROP FUNCTION fail_report_publish_audit()`); err != nil {
		t.Fatal(err)
	}
	v = reportAction(t, review, v, "publish", "")
	v = reportAction(t, curator, v, "correct", "")
	in.Revision = v.Revision
	in.Body = "PRIVATE correction"
	decode(t, expect(t, curator, "PUT", path, in, 200), &v)
	var old family.Report
	decode(t, expect(t, review, "GET", path, nil, 200), &old)
	if old.PublishedAt == nil {
		t.Fatal("published edition lost publication date while correction was private")
	}
	// Withdrawal of an old publication must not return the unsubmitted correction.
	raw := expect(t, review, "POST", path+"/actions", family.ReportAction{Action: "withdraw", Version: *old.PublishedVersion, Revision: old.Revision, Reason: "Withdraw old publication"}, 200)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("withdrawal exposed correction")
	}
	expect(t, pupil, "GET", fmt.Sprintf("/family/reports/%d", v.ID), nil, 404)
	expect(t, review, "GET", path, nil, 404)
	decode(t, expect(t, curator, "GET", path, nil, 200), &v)
	if v.State != "draft" || v.Body != "PRIVATE correction" {
		t.Fatal("withdrawal destroyed correction")
	}
	// Combined roles do not bypass the record of participation in this version.
	if _, err := f.pool.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'education_leader')`, cu.ID); err != nil {
		t.Fatal(err)
	}
	in.Revision = v.Revision
	in.ReviewerID = &cu.ID
	decode(t, expect(t, curator, "PUT", path, in, 200), &v)
	expect(t, curator, "POST", path+"/actions", family.ReportAction{Action: "submit", Version: v.Version, Revision: v.Revision}, 400)
}
