package app_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/modules/notifications"
	"aiq.local/core/internal/platform/notify"
)

func deliveryQueue(t *testing.T, b *browser, status string) notifications.DeliveryPage {
	t.Helper()
	var out notifications.DeliveryPage
	decode(t, expect(t, b, "GET", "/notification-deliveries?status="+status, nil, 200), &out)
	return out
}

func TestNotificationOutboxBoundedRetryRecoveryAndManualRetry(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	child := studentCreate(t, admin, map[string]any{"name": "Child"})
	pupil := familyUser(t, f, admin, mail, "outbox@example.test", "student", child.ID)
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = notify.Participation(ctx, tx, 51, "1", child.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	initial := deliveryQueue(t, admin, "pending").Items[0]
	sender := &notificationMail{fail: true}
	repo := &notifications.Repository{Pool: f.pool}
	worker := &notifications.Worker{Repo: repo, Sender: sender, Origin: "https://cabinet.example.test"}
	for attempt := 1; attempt <= 5; attempt++ {
		if done, e := worker.DeliverOne(ctx); e != nil || !done {
			t.Fatal(done, e)
		}
		d := deliveryQueue(t, admin, "").Items[0]
		if d.Attempts != attempt || d.CycleAttempts != attempt {
			t.Fatal("incorrect attempts", d)
		}
		if attempt < 5 {
			if d.Status != "pending" || d.NextAttemptAt == nil {
				t.Fatal("missing retry", d)
			}
			delay := time.Until(*d.NextAttemptAt).Seconds()
			want := float64(30 * (int64(1) << (attempt - 1)))
			if delay < want-5 || delay > want+2 {
				t.Fatal("incorrect backoff", delay, want)
			}
			if done, e := worker.DeliverOne(ctx); e != nil || done {
				t.Fatal("early retry", done, e)
			}
			if _, e := f.pool.Exec(ctx, `UPDATE notification_deliveries SET next_attempt_at=clock_timestamp() WHERE id=$1`, d.ID); e != nil {
				t.Fatal(e)
			}
		} else if d.Status != "failed" || d.NextAttemptAt != nil || !d.CanRetry || d.LastError != "provider_error" {
			t.Fatal("not dead letter", d)
		}
	}
	if done, e := worker.DeliverOne(ctx); e != nil || done {
		t.Fatal("unbounded retries", done, e)
	}
	failed := deliveryQueue(t, admin, "failed").Items[0]
	path := fmt.Sprintf("/notification-deliveries/%d/retry", failed.ID)
	input := notifications.RetryInput{Revision: failed.Revision, Reason: "SMTP restored", AcknowledgeDuplicateRisk: true}
	expect(t, pupil, "GET", "/notification-deliveries", nil, 403)
	expect(t, pupil, "POST", path, input, 403)
	expect(t, admin, "POST", "/notification-deliveries/bad/retry", input, 400)
	expect(t, admin, "GET", "/notification-deliveries?status=delivered", nil, 400)
	expect(t, admin, "POST", path, notifications.RetryInput{Revision: failed.Revision, Reason: "missing acknowledgement"}, 400)
	expect(t, admin, "POST", path, notifications.RetryInput{Revision: failed.Revision, AcknowledgeDuplicateRisk: true}, 400)
	input.Revision = initial.Revision
	expect(t, admin, "POST", path, input, 409)
	input.Revision = failed.Revision
	csrf := admin.csrf
	admin.csrf = "bad"
	expect(t, admin, "POST", path, input, 403)
	admin.csrf = csrf
	// History/audit and state must roll back together.
	if _, err = f.pool.Exec(ctx, `CREATE FUNCTION reject_retry_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='notification.delivery_retry' THEN RAISE EXCEPTION 'test'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_retry_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_retry_audit()`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", path, input, 500)
	after := deliveryQueue(t, admin, "failed").Items[0]
	if after.Revision != failed.Revision || len(after.Retries) != 0 {
		t.Fatal("partial retry", after)
	}
	if _, err = f.pool.Exec(ctx, `DROP TRIGGER reject_retry_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	// Concurrent retries of one revision can enqueue only once.
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); status, _ := admin.do(t, "POST", path, input); results <- status }()
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	for s := range results {
		statuses[s]++
	}
	if statuses[200] != 1 || statuses[409] != 1 {
		t.Fatal("unsafe concurrent retry", statuses)
	}
	pending := deliveryQueue(t, admin, "pending").Items[0]
	if pending.Attempts != 5 || pending.CycleAttempts != 0 || len(pending.Retries) != 1 || pending.Retries[0].Attempts != 5 {
		t.Fatal("lost retry history", pending)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE notification_delivery_retries SET reason='changed'`); err == nil {
		t.Fatal("mutable history")
	}
	// Simulate a process crash after committing the claim. Recovery is bounded.
	if _, err = f.pool.Exec(ctx, `UPDATE notification_deliveries SET status='processing',next_attempt_at=NULL,attempts=attempts+1,cycle_attempts=1,claim_token=gen_random_uuid(),lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, pending.ID); err != nil {
		t.Fatal(err)
	}
	sender.fail = false
	if done, e := worker.DeliverOne(ctx); e != nil || !done {
		t.Fatal(done, e)
	}
	sent := deliveryQueue(t, admin, "sent").Items[0]
	if sent.Attempts != 7 || sent.CycleAttempts != 2 || sent.SentAt == nil || sent.DeliveredAt != nil || len(sender.sent) != 1 {
		t.Fatal("recovery or sent semantics", sent)
	}
	input.Revision = sent.Revision
	expect(t, admin, "POST", path, input, 409)
	if err = repo.ConfirmDelivery(ctx, "smtp", "", "fabricated", time.Now()); err == nil {
		t.Fatal("invented delivery confirmation")
	}
	if strings.Contains(string(expect(t, admin, "GET", "/notification-deliveries", nil, 200)), "private data") {
		t.Fatal("raw provider error disclosed")
	}
}

func TestNotificationOutboxDeduplicationRevocationAndRollback(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	p := parentCreate(t, admin, map[string]any{"name": "Parent", "email": "outbox-parent@example.test"})
	parent := familyUser(t, f, admin, mail, "outbox-parent@example.test", "parent", p.ID)
	children := []int64{}
	for i := 0; i < 2; i++ {
		child := studentCreate(t, admin, map[string]any{"name": fmt.Sprintf("Child %d", i)})
		linkParent(t, admin, child.ID, p.ID, true)
		familyLink(t, admin, child.ID, p.ID, true)
		children = append(children, child.ID)
	}
	emit := func(id int64, commit bool) {
		t.Helper()
		tx, e := f.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		for _, sid := range children {
			if e = notify.Participation(ctx, tx, id, "1", sid, true); e != nil {
				t.Fatal(e)
			}
		}
		if commit {
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
		}
	}
	emit(99, false)
	if deliveryQueue(t, admin, "").Total != 0 {
		t.Fatal("rollback left delivery")
	}
	// A delivery insert failure also aborts the notification transaction.
	if _, err := f.pool.Exec(ctx, `CREATE FUNCTION reject_delivery() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test'; END $$; CREATE TRIGGER reject_delivery BEFORE INSERT ON notification_deliveries FOR EACH ROW EXECUTE FUNCTION reject_delivery()`); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = notify.Participation(ctx, tx, 99, "1", children[0], true); err == nil {
		t.Fatal("expected failure")
	}
	_ = tx.Rollback(ctx)
	if _, err = f.pool.Exec(ctx, `DROP TRIGGER reject_delivery ON notification_deliveries`); err != nil {
		t.Fatal(err)
	}
	emit(99, true)
	emit(99, true)
	if deliveryQueue(t, admin, "").Total != 1 || len(notices(t, parent, "participation_confirmed")) != 1 {
		t.Fatal("duplicate channel delivery")
	}
	item := notices(t, parent, "participation_confirmed")[0]
	expect(t, parent, "POST", fmt.Sprintf("/notifications/%d/read", item.ID), nil, 200)
	familyLink(t, admin, children[0], p.ID, false)
	remaining := notices(t, parent, "participation_confirmed")
	if len(remaining) != 1 || remaining[0].ReadAt == nil {
		t.Fatal("remaining context/read mark lost")
	}
	sender := &notificationMail{}
	worker := &notifications.Worker{Repo: &notifications.Repository{Pool: f.pool}, Sender: sender, Origin: "https://cabinet.example.test"}
	if _, err = worker.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatal("live second context should allow one email")
	}
	emit(100, true)
	familyLink(t, admin, children[1], p.ID, false)
	if _, err = worker.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	failed := deliveryQueue(t, admin, "failed").Items[0]
	if failed.CanRetry || failed.LastError != "recipient_unavailable" || len(sender.sent) != 1 {
		t.Fatal("revocation ignored", failed)
	}
	input := notifications.RetryInput{Revision: failed.Revision, Reason: "retry", AcknowledgeDuplicateRisk: true}
	path := fmt.Sprintf("/notification-deliveries/%d/retry", failed.ID)
	expect(t, admin, "POST", path, input, 409)
	familyLink(t, admin, children[1], p.ID, true)
	expect(t, admin, "POST", path, input, 200)
	familyLink(t, admin, children[1], p.ID, false)
	if _, err = worker.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatal("retry bypassed current access")
	}
	// Crashes at the limit must become failed without another provider call.
	if _, err = f.pool.Exec(ctx, `UPDATE notification_deliveries SET status='processing',next_attempt_at=NULL,attempts=GREATEST(attempts,max_attempts),cycle_attempts=max_attempts,claim_token=gen_random_uuid(),lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, failed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	if deliveryQueue(t, admin, "failed").Items[0].LastError != "attempts_exhausted" || len(sender.sent) != 1 {
		t.Fatal("unbounded crash recovery")
	}
}

type idempotentNotificationMail struct {
	notificationMail
	keys []string
	fail bool
}

func (s *idempotentNotificationMail) SendNotificationIdempotent(_ context.Context, _, _, _, key string) (notifications.Handoff, error) {
	s.keys = append(s.keys, key)
	if s.fail {
		return notifications.Handoff{}, errors.New("uncertain acceptance")
	}
	return notifications.Handoff{Provider: "test-provider", MessageID: "message-1"}, nil
}
func TestNotificationOutboxProviderKeyAndConfirmedDelivery(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	child := studentCreate(t, admin, map[string]any{"name": "Child"})
	familyUser(t, f, admin, mail, "provider-key@example.test", "student", child.ID)
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = notify.Participation(ctx, tx, 1, "1", child.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE notification_deliveries SET max_attempts=1 WHERE channel='email'`); err != nil {
		t.Fatal(err)
	}
	sender := &idempotentNotificationMail{fail: true}
	repo := &notifications.Repository{Pool: f.pool}
	w := &notifications.Worker{Repo: repo, Sender: sender, Origin: "https://cabinet.example.test"}
	if _, err = w.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	d := deliveryQueue(t, admin, "failed").Items[0]
	expect(t, admin, "POST", fmt.Sprintf("/notification-deliveries/%d/retry", d.ID), notifications.RetryInput{Revision: d.Revision, Reason: "provider recovered", AcknowledgeDuplicateRisk: true}, 200)
	sender.fail = false
	if _, err = w.DeliverOne(ctx); err != nil {
		t.Fatal(err)
	}
	if len(sender.keys) != 2 || sender.keys[0] == "" || sender.keys[0] != sender.keys[1] {
		t.Fatal("unstable idempotency key", sender.keys)
	}
	d = deliveryQueue(t, admin, "sent").Items[0]
	if d.DeliveredAt != nil {
		t.Fatal("handoff counted as delivery")
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err = repo.ConfirmDelivery(ctx, "test-provider", "wrong", "evidence", at); err == nil {
		t.Fatal("unmatched evidence")
	}
	if err = repo.ConfirmDelivery(ctx, "test-provider", "message-1", "", at); err == nil {
		t.Fatal("missing evidence")
	}
	for i := 0; i < 2; i++ {
		if err = repo.ConfirmDelivery(ctx, "test-provider", "message-1", "receipt-1", at); err != nil {
			t.Fatal(err)
		}
	}
	d = deliveryQueue(t, admin, "sent").Items[0]
	if d.DeliveredAt == nil || !d.DeliveredAt.Equal(at) {
		t.Fatal("confirmation missing", d)
	}
	if err = repo.ConfirmDelivery(ctx, "test-provider", "message-1", "receipt-2", at); err == nil {
		t.Fatal("conflicting evidence accepted")
	}
}
