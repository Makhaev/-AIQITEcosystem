package app_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/modules/notifications"
	"aiq.local/core/internal/platform/notify"
)

type notificationMail struct {
	mu      sync.Mutex
	fail    bool
	sent    []string
	entered chan struct{}
	release chan struct{}
}

func (m *notificationMail) Ready() error { return nil }
func (m *notificationMail) SendNotification(ctx context.Context, email, subject, link string) error {
	if m.entered != nil {
		close(m.entered)
		select {
		case <-m.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("synthetic provider failure with private data")
	}
	m.sent = append(m.sent, email+"|"+subject+"|"+link)
	return nil
}
func inbox(t *testing.T, b *browser) notifications.Page {
	t.Helper()
	var out notifications.Page
	decode(t, expect(t, b, "GET", "/notifications", nil, 200), &out)
	return out
}
func notices(t *testing.T, b *browser, kind string) []notifications.Item {
	t.Helper()
	out := []notifications.Item{}
	for _, n := range inbox(t, b).Items {
		if n.Kind == kind {
			out = append(out, n)
		}
	}
	return out
}
func TestNotificationsRequestPrivacyAtomicityRetryAndConcurrentDelivery(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	child := studentCreate(t, admin, map[string]any{"name": "PRIVATE child"})
	parents := []*browser{}
	var firstParent int64
	for i := 0; i < 2; i++ {
		email := fmt.Sprintf("notice-parent-%d@example.test", i)
		p := parentCreate(t, admin, map[string]any{"name": "PRIVATE parent", "email": email})
		linkParent(t, admin, child.ID, p.ID, i == 0)
		familyLink(t, admin, child.ID, p.ID, true)
		parents = append(parents, familyUser(t, f, admin, mail, email, "parent", p.ID))
		if i == 0 {
			firstParent = p.ID
		}
	}
	var q family.Request
	decode(t, expect(t, parents[0], "POST", "/family/requests", family.RequestInput{StudentID: &child.ID, Subject: "PRIVATE subject", Body: "PRIVATE message"}, 201), &q)
	q = changeRequest(t, admin, q, true, "assign", &f.admin.ID, "", "")
	q = changeRequest(t, admin, q, true, "start", nil, "", "")
	// Failure to persist a notification rolls back the business change as well.
	if _, err := f.pool.Exec(ctx, `CREATE FUNCTION reject_notice() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test'; END $$; CREATE TRIGGER reject_notice BEFORE INSERT ON notifications FOR EACH ROW EXECUTE FUNCTION reject_notice()`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", fmt.Sprintf("/family-requests/%d/actions", q.ID), family.RequestChange{Action: "answer", Revision: q.Revision, Body: "PRIVATE answer"}, 500)
	var saved family.Request
	decode(t, expect(t, parents[0], "GET", fmt.Sprintf("/family/requests/%d", q.ID), nil, 200), &saved)
	if saved.Revision != q.Revision || saved.Status != "in_progress" {
		t.Fatal("notification failure partially committed")
	}
	if _, err := f.pool.Exec(ctx, `DROP TRIGGER reject_notice ON notifications`); err != nil {
		t.Fatal(err)
	}
	q = changeRequest(t, admin, q, true, "answer", nil, "PRIVATE answer", "")
	own := notices(t, parents[0], "request_answered")
	if len(own) != 1 || len(notices(t, parents[1], "request_answered")) != 0 {
		t.Fatal("request recipient isolation")
	}
	if own[0].URL != fmt.Sprintf("#/my-requests/%d", q.ID) {
		t.Fatal("incorrect cabinet link")
	}
	raw := string(expect(t, parents[0], "GET", "/notifications", nil, 200))
	if strings.Contains(raw, "PRIVATE") {
		t.Fatal("private content in notification")
	}
	path := fmt.Sprintf("/notifications/%d/read", own[0].ID)
	expect(t, parents[1], "POST", path, nil, 404)
	csrf := parents[0].csrf
	parents[0].csrf = "wrong"
	expect(t, parents[0], "POST", path, nil, 403)
	parents[0].csrf = csrf
	expect(t, parents[0], "POST", path, nil, 200)
	expect(t, parents[0], "POST", path, nil, 200)
	if notices(t, parents[0], "request_answered")[0].ReadAt == nil {
		t.Fatal("read mark missing")
	}
	sender := &notificationMail{fail: true}
	w := &notifications.Worker{Repo: &notifications.Repository{Pool: f.pool}, Sender: sender, Origin: "https://cabinet.example.test"}
	if done, err := w.DeliverOne(ctx); err != nil || !done {
		t.Fatal(done, err)
	}
	var retry bool
	if err := f.pool.QueryRow(ctx, `SELECT d.attempts=1 AND d.status='pending' AND d.next_attempt_at>clock_timestamp() FROM notification_deliveries d JOIN notifications n USING(kind,object_id,event_version,user_id) WHERE n.id=$1 AND d.channel='email'`, own[0].ID).Scan(&retry); err != nil || !retry {
		t.Fatal("retry not durable", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE notification_deliveries d SET next_attempt_at=clock_timestamp() FROM notifications n WHERE n.id=$1 AND d.channel='email' AND (d.kind,d.object_id,d.event_version,d.user_id)=(n.kind,n.object_id,n.event_version,n.user_id)`, own[0].ID); err != nil {
		t.Fatal(err)
	}
	sender.fail = false
	sender.entered = make(chan struct{})
	sender.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := w.DeliverOne(ctx); done <- err }()
	select {
	case <-sender.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not claim job")
	}
	var processing bool
	if err := f.pool.QueryRow(ctx, `SELECT status='processing' AND attempts=2 AND claim_token IS NOT NULL FROM notification_deliveries WHERE channel='email'`).Scan(&processing); err != nil || !processing {
		t.Fatal("processing claim was not committed before network I/O", err)
	}
	if claimed, err := w.DeliverOne(ctx); err != nil || claimed {
		t.Fatal("concurrent worker claimed locked delivery", claimed, err)
	}
	close(sender.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 || strings.Contains(sender.sent[0], "PRIVATE") || !strings.Contains(sender.sent[0], "https://cabinet.example.test/#/notifications") {
		t.Fatal("unsafe or duplicate email", sender.sent)
	}
	sender.entered = nil
	if claimed, err := w.DeliverOne(ctx); err != nil || claimed {
		t.Fatal("already delivered job repeated")
	}
	// A second answer is queued, then access is revoked before delivery.
	q = changeRequest(t, parents[0], q, false, "message", nil, "Follow-up", "")
	q = changeRequest(t, admin, q, true, "answer", nil, "Second private answer", "")
	familyLink(t, admin, child.ID, firstParent, false)
	if len(notices(t, parents[0], "request_answered")) != 0 {
		t.Fatal("revoked inbox leak")
	}
	expect(t, parents[0], "POST", path, nil, 404)
	if claimed, err := w.DeliverOne(ctx); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if len(sender.sent) != 1 {
		t.Fatal("email after revocation")
	}
}

func TestNotificationsLessonsAssignmentsReviewsReportsAndParticipationAdapter(t *testing.T) {
	a := setupAttendance(t)
	mail := familyMail(t, a.f)
	a.admin = a.f.adminBrowser(t)
	a.own = a.f.browser(t)
	a.own.login(t, "attendance-own@example.test", "TestOnly-New-Account-9372")
	ctx := context.Background()
	pupil := familyUser(t, a.f, a.admin, mail, "notify-pupil@example.test", "student", a.firstID)
	l := a.lesson(t, time.Hour, 2*time.Hour)
	expect(t, a.admin, "PATCH", lessonPath(l.ID), map[string]any{"starts_at": l.StartsAt.Add(time.Minute), "ends_at": l.EndsAt.Add(time.Minute)}, 200)
	if len(notices(t, pupil, "lesson_rescheduled")) != 1 || len(notices(t, a.own, "lesson_rescheduled")) != 1 {
		t.Fatal("lesson reschedule recipients")
	}
	expect(t, a.admin, "POST", lessonPath(l.ID)+"/cancel", nil, 200)
	if len(notices(t, pupil, "lesson_cancelled")) != 1 {
		t.Fatal("lesson cancellation missing")
	}
	lesson := a.lesson(t, -time.Hour, time.Hour)
	var reviewer, version int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&reviewer); err != nil {
		t.Fatal(err)
	}
	if err := a.f.pool.QueryRow(ctx, `SELECT program_version_id FROM groups WHERE id=$1`, a.groupID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	late := true
	assignment := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "assignments", LessonID: &lesson.ID, ReviewerID: &reviewer, Content: learning.Content{Title: "PRIVATE assignment", Instruction: "PRIVATE instruction", Criteria: "PRIVATE criteria", DueOn: "2099-01-01", AllowLate: &late, Format: "text", ProgramVersionID: &version, TemplateKey: "lesson-1"}})
	if len(notices(t, pupil, "assignment_assigned")) != 1 {
		t.Fatal("assignment notification missing")
	}
	var result struct{ ID int64 }
	decode(t, expect(t, pupil, "POST", fmt.Sprintf("/family/assignments/%d/attempts", assignment.ID), learning.StudentAttempt{Revision: assignment.Revision, Answer: "PRIVATE response"}, 201), &result)
	var attempt learning.Card
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", result.ID), nil, 200), &attempt)
	attempt = learningAction(t, a.own, attempt, "return")
	if len(notices(t, pupil, "attempt_reviewed")) != 1 {
		t.Fatal("review notification missing")
	}
	learningCreate(t, a.own, a.firstID, learning.Input{Kind: "observations", LessonID: &lesson.ID, ReviewerID: &a.f.admin.ID, Content: learning.Content{Title: "PRIVATE observation", Fact: "PRIVATE fact", Context: "PRIVATE context", OccurredOn: "2020-01-01", ModelVersion: "v1", SkillKey: "skill", SkillName: "PRIVATE skill"}})
	var report family.Report
	decode(t, expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/family-reports", a.firstID), family.ReportInput{Title: "PRIVATE report", Body: "PRIVATE report body"}, 201), &report)
	report = publishFamilyReport(t, a.f, a.admin, a.admin, report)
	if len(notices(t, pupil, "report_published")) != 1 {
		t.Fatal("report notification missing")
	}
	reportAction(t, a.admin, report, "publish", "")
	if len(notices(t, pupil, "report_published")) != 1 {
		t.Fatal("duplicate report notification")
	}
	reportAction(t, a.admin, report, "withdraw", "PRIVATE withdrawal")
	if len(notices(t, pupil, "report_published")) != 0 {
		t.Fatal("withdrawn report notification visible")
	}
	tx, err := a.f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, confirmed := range []bool{true, true, false} {
		if err = notify.Participation(ctx, tx, 42, "1", a.firstID, confirmed); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if len(notices(t, pupil, "participation_confirmed")) != 1 || len(notices(t, pupil, "participation_cancelled")) != 1 {
		t.Fatal("participation adapter deduplication")
	}
	if raw := string(expect(t, pupil, "GET", "/notifications", nil, 200)); strings.Contains(raw, "PRIVATE") || strings.Contains(raw, "observation") {
		t.Fatal("notification disclosed source content")
	}
}

func TestNotificationsAccountEventsAndPagination(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	child := studentCreate(t, admin, map[string]any{"name": "Child"})
	pupil := familyUser(t, f, admin, mail, "notification-account@example.test", "student", child.ID)
	if len(notices(t, pupil, "invitation")) != 1 {
		t.Fatal("accepted invitation notification missing")
	}
	// Exercise the same token-issuance repository used by the asynchronous reset.
	if issued, err := (&auth.Repository{Pool: f.pool}).IssueReset(ctx, "notification-account@example.test", auth.Hash("synthetic-notification-token")); err != nil || !issued {
		t.Fatal(issued, err)
	}
	if len(notices(t, pupil, "password_reset")) != 1 {
		t.Fatal("reset notification missing")
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for i := 0; i < 23; i++ {
		if err = notify.Participation(ctx, tx, int64(100+i), "1", child.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	first := inbox(t, pupil)
	if first.Total != 25 || first.Unread != 25 || len(first.Items) != 20 {
		t.Fatal("pagination counts", first)
	}
	var second notifications.Page
	decode(t, expect(t, pupil, "GET", "/notifications?page=2", nil, 200), &second)
	if len(second.Items) != 5 {
		t.Fatal("second page")
	}
	expect(t, pupil, "GET", "/notifications?page=0", nil, 400)
	if len(inbox(t, admin).Items) != 0 {
		t.Fatal("admin reads others' inbox")
	}
}
