package app_test

import (
	"aiq.local/core/internal/app"
	"aiq.local/core/internal/modules/events"
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/config"
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func registrationPath(eid, rid int64) string {
	return fmt.Sprintf("/events/%d/registrations/%d", eid, rid)
}
func registerEvent(t *testing.T, b *browser, eid, sid int64) events.Registration {
	t.Helper()
	var v events.Registration
	decode(t, expect(t, b, "POST", eventPath(eid)+"/registrations", events.RegistrationInput{StudentID: sid}, 201), &v)
	return v
}
func registrationAction(t *testing.T, b *browser, eid int64, v events.Registration, action string) events.Registration {
	t.Helper()
	var out events.Registration
	decode(t, expect(t, b, "POST", registrationPath(eid, v.ID)+"/actions", events.RegistrationInput{Action: action, Revision: v.Revision}, 200), &out)
	return out
}
func TestEventParticipationAccessCapacityHistoryAndDocuments(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	staff, responsible := provision(t, f, admin, "event-manager@example.test", "teacher")
	_, outsider := provision(t, f, admin, "event-outsider@example.test", "teacher")
	child := studentCreate(t, admin, map[string]any{"name": "Алексей"})
	other := studentCreate(t, admin, map[string]any{"name": "Other"})
	p := parentCreate(t, admin, map[string]any{"name": "Parent", "email": "registration-parent@example.test"})
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", child.ID), map[string]any{"parent_id": p.ID, "relationship": "parent", "is_primary": true}, 201)
	parent := familyUser(t, f, admin, mail, "registration-parent@example.test", "parent", p.ID)
	student := familyUser(t, f, admin, mail, "registration-student@example.test", "student", child.ID)
	in := filledEvent(staff.ID)
	in.Capacity = 1
	e := eventAction(t, admin, eventCreate(t, admin, in), "publish")
	path := eventPath(e.ID) + "/registrations"
	expect(t, parent, "POST", path, events.RegistrationInput{StudentID: child.ID}, 403)
	familyLink(t, admin, child.ID, p.ID, true)
	var candidates events.Page[events.Person]
	decode(t, expect(t, parent, "GET", eventPath(e.ID)+"/candidates?search=%D0%90%D0%9B%D0%95%D0%9A", nil, 200), &candidates)
	if candidates.Total != 1 || len(candidates.Items) != 1 || !candidates.Items[0].Active {
		t.Fatal("candidate Cyrillic search", candidates)
	}
	expect(t, parent, "POST", path, events.RegistrationInput{StudentID: other.ID}, 403)
	expect(t, student, "POST", path, events.RegistrationInput{StudentID: child.ID}, 403) // no agreed age configured
	v := registerEvent(t, parent, e.ID, child.ID)
	if v.Status != "requested" {
		t.Fatal(v)
	}
	expect(t, parent, "POST", path, events.RegistrationInput{StudentID: child.ID}, 409)
	expect(t, parent, "POST", registrationPath(e.ID, v.ID)+"/actions", events.RegistrationInput{Action: "confirmed", Revision: v.Revision}, 409)
	expect(t, outsider, "POST", registrationPath(e.ID, v.ID)+"/actions", events.RegistrationInput{Action: "confirmed", Revision: v.Revision}, 404)
	wait := registerEvent(t, admin, e.ID, other.ID)
	if wait.Status != "waitlisted" {
		t.Fatal(wait)
	}
	expect(t, responsible, "POST", registrationPath(e.ID, wait.ID)+"/actions", events.RegistrationInput{Action: "confirmed", Revision: wait.Revision}, 409)
	var list events.ParticipationPage
	decode(t, expect(t, parent, "GET", path, nil, 200), &list)
	if list.Total != 1 || list.Occupied != 1 {
		t.Fatal(list)
	}
	decode(t, expect(t, outsider, "GET", path, nil, 200), &list)
	if list.Total != 0 || list.CanManage {
		t.Fatal("roster leak", list)
	}
	v = registrationAction(t, responsible, e.ID, v, "confirmed")
	var doc files.File
	base := registrationPath(e.ID, v.ID)
	decode(t, expect(t, parent, "POST", base+"/documents", files.Input{Name: "consent.txt", Data: []byte("Private participation consent")}, 201), &doc)
	download := fmt.Sprintf("%s/documents/%d", base, doc.ID)
	for _, b := range []*browser{parent, student, admin, responsible} {
		if string(expect(t, b, "GET", download, nil, 200)) != "Private participation consent" {
			t.Fatal("document content")
		}
	}
	expect(t, outsider, "GET", download, nil, 404)
	expect(t, parent, "GET", fmt.Sprintf("%s/documents/%d", registrationPath(e.ID, wait.ID), doc.ID), nil, 404)
	familyLink(t, admin, child.ID, p.ID, false)
	expect(t, parent, "GET", download, nil, 404)
	expect(t, parent, "GET", base+"/history", nil, 404)
	expect(t, parent, "POST", base+"/actions", events.RegistrationInput{Action: "cancelled", Revision: v.Revision}, 404)
	familyLink(t, admin, child.ID, p.ID, true)
	v = registrationAction(t, parent, e.ID, v, "cancelled")
	wait = registrationAction(t, responsible, e.ID, wait, "confirmed")
	again := registerEvent(t, parent, e.ID, child.ID)
	if again.ID == v.ID || again.Status != "waitlisted" {
		t.Fatal("re-registration lost history", again)
	}
	var history []events.History
	decode(t, expect(t, parent, "GET", base+"/history", nil, 200), &history)
	if len(history) != 3 || history[0].Status != "requested" || history[2].Status != "cancelled" {
		t.Fatal(history)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM event_registrations WHERE id=$1`, v.ID); err == nil {
		t.Fatal("history deleted")
	}
	// A capacity edit cannot invalidate existing reservations.
	in = e.Input
	in.Capacity = 0
	expect(t, admin, "PUT", eventPath(e.ID), in, 400)
	in = e.Input
	start := in.StartsAt.Add(time.Hour)
	end := in.EndsAt.Add(time.Hour)
	in.StartsAt = &start
	in.EndsAt = &end
	decode(t, expect(t, admin, "PUT", eventPath(e.ID), in, 200), &e)
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE object_id=$1 AND kind='event_rescheduled' AND user_id=(SELECT user_id FROM parent_accounts WHERE parent_id=$2)`, e.ID, p.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("reschedule notification", n, err)
	}
	e = eventAction(t, admin, e, "cancel")
	if e.Status != "cancelled" || e.CanEdit {
		t.Fatal(e)
	}
	expect(t, parent, "POST", path, events.RegistrationInput{StudentID: child.ID}, 409)
	decode(t, expect(t, parent, "GET", registrationPath(e.ID, again.ID)+"/history", nil, 200), &history)
	if len(history) != 2 || history[1].Status != "cancelled" {
		t.Fatal(history)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE object_id=$1 AND kind='event_cancelled' AND channel='email'`, e.ID).Scan(&n); err != nil || n != 2 {
		t.Fatal("cancel outbox", n, err)
	}
	expect(t, parent, "GET", download, nil, 200)
	expect(t, parent, "GET", "/notifications", nil, 200)
}

func TestEventParticipationConcurrentLastSeatAndPromotion(t *testing.T) {
	f, admin, _ := setupInvitations(t)
	in := filledEvent(f.admin.ID)
	in.Capacity = 1
	e := eventAction(t, admin, eventCreate(t, admin, in), "publish")
	ids := []int64{}
	for i := 0; i < 3; i++ {
		ids = append(ids, studentCreate(t, admin, map[string]any{"name": fmt.Sprintf("Racer %d", i)}).ID)
	}
	var wg sync.WaitGroup
	result := make(chan events.Registration, 3)
	for _, sid := range ids {
		wg.Add(1)
		go func(sid int64) { defer wg.Done(); result <- registerEvent(t, admin, e.ID, sid) }(sid)
	}
	wg.Wait()
	close(result)
	var reserved events.Registration
	waiting := []events.Registration{}
	for v := range result {
		if v.Status == "requested" {
			if reserved.ID != 0 {
				t.Fatal("double booking")
			}
			reserved = v
		} else if v.Status == "waitlisted" {
			waiting = append(waiting, v)
		} else {
			t.Fatal(v)
		}
	}
	if len(waiting) != 2 || reserved.ID == 0 {
		t.Fatal("unexpected allocation")
	}
	registrationAction(t, admin, e.ID, reserved, "cancelled")
	codes := make(chan int, 2)
	for _, v := range waiting {
		wg.Add(1)
		go func(v events.Registration) {
			defer wg.Done()
			code, _ := admin.do(t, "POST", registrationPath(e.ID, v.ID)+"/actions", events.RegistrationInput{Action: "confirmed", Revision: v.Revision})
			codes <- code
		}(v)
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("promotion oversold", counts)
	}
	// Same participant racing twice yields one active registration.
	in = filledEvent(f.admin.ID)
	e = eventAction(t, admin, eventCreate(t, admin, in), "publish")
	codes = make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := admin.do(t, "POST", eventPath(e.ID)+"/registrations", events.RegistrationInput{StudentID: ids[0]})
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	counts = map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatal("duplicate race", counts)
	}
}

func TestEventParticipationAgeWindowAttendanceAndRollback(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	h, _, err := app.RouterWithInvitationSender(f.pool, config.Config{Origin: origin, EventSelfRegistrationMinAge: 14}, mail)
	if err != nil {
		t.Fatal(err)
	}
	f.server.Close()
	f.server = httptest.NewServer(h)
	t.Cleanup(f.server.Close)
	admin = f.browser(t)
	admin.login(t, "admin@example.test", adminPassword)
	child := studentCreate(t, admin, map[string]any{"name": "Birthday student"})
	student := familyUser(t, f, admin, mail, "birthday@example.test", "student", child.ID)
	e := eventAction(t, admin, eventCreate(t, admin, filledEvent(f.admin.ID)), "publish")
	path := eventPath(e.ID) + "/registrations"
	expect(t, student, "POST", path, events.RegistrationInput{StudentID: child.ID}, 403)
	setBirth := func(offset string) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, `UPDATE students SET birth_date=((clock_timestamp() AT TIME ZONE 'Asia/Yekaterinburg')::date-interval '14 years'+$2::interval)::date WHERE id=$1`, child.ID, offset); err != nil {
			t.Fatal(err)
		}
	}
	setBirth("1 day")
	expect(t, student, "POST", path, events.RegistrationInput{StudentID: child.ID}, 403)
	setBirth("0 days")
	v := registerEvent(t, student, e.ID, child.ID)
	// Audit failure must roll back status, history, and outbox together.
	if _, err := f.pool.Exec(ctx, `CREATE FUNCTION reject_registration_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='event.registration_confirmed' THEN RAISE EXCEPTION 'test'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_registration_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_registration_audit()`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", registrationPath(e.ID, v.ID)+"/actions", events.RegistrationInput{Action: "confirmed", Revision: v.Revision}, 500)
	var n int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM event_registration_history WHERE registration_id=$1`, v.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal("partial history", n, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE kind='participation_confirmed' AND object_id=$1`, e.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial notification", n, err)
	}
	if _, err := f.pool.Exec(ctx, `DROP TRIGGER reject_registration_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	v = registrationAction(t, admin, e.ID, v, "confirmed")
	other := studentCreate(t, admin, map[string]any{"name": "Absent student"})
	absent := registrationAction(t, admin, e.ID, registerEvent(t, admin, e.ID, other.ID), "confirmed")
	shrink := e.Input
	shrink.Capacity = 1
	expect(t, admin, "PUT", eventPath(e.ID), shrink, 400)
	expect(t, admin, "POST", registrationPath(e.ID, v.ID)+"/actions", events.RegistrationInput{Action: "attended", Revision: v.Revision}, 409)
	if _, err := f.pool.Exec(ctx, `UPDATE free_events SET registration_starts_at=clock_timestamp()-interval '4 days',registration_ends_at=clock_timestamp()-interval '3 days',starts_at=clock_timestamp()-interval '2 days',ends_at=clock_timestamp()-interval '1 day' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, student, "POST", path, events.RegistrationInput{StudentID: child.ID}, 409)
	expect(t, student, "POST", registrationPath(e.ID, v.ID)+"/actions", events.RegistrationInput{Action: "attended", Revision: v.Revision}, 409)
	v = registrationAction(t, admin, e.ID, v, "attended")
	absent = registrationAction(t, admin, e.ID, absent, "no_show")
	if absent.Status != "no_show" {
		t.Fatal(absent)
	}
	if v.Status != "attended" {
		t.Fatal(v)
	}
	// Future registration opening is enforced independently of publication.
	in := filledEvent(f.admin.ID)
	future := time.Now().Add(time.Hour)
	in.RegistrationStartsAt = &future
	e = eventAction(t, admin, eventCreate(t, admin, in), "publish")
	expect(t, student, "POST", eventPath(e.ID)+"/registrations", events.RegistrationInput{StudentID: child.ID}, 409)
}
