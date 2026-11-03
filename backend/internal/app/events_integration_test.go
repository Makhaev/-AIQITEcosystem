package app_test

import (
	"aiq.local/core/internal/modules/events"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func eventPath(id int64) string { return fmt.Sprintf("/events/%d", id) }
func eventCreate(t *testing.T, b *browser, in events.Input) events.Event {
	t.Helper()
	var e events.Event
	decode(t, expect(t, b, "POST", "/events", in, 201), &e)
	return e
}
func eventGet(t *testing.T, b *browser, id int64) events.Event {
	t.Helper()
	var e events.Event
	decode(t, expect(t, b, "GET", eventPath(id), nil, 200), &e)
	return e
}
func eventList(t *testing.T, b *browser, query string) events.Page[events.Event] {
	t.Helper()
	var p events.Page[events.Event]
	decode(t, expect(t, b, "GET", "/events"+query, nil, 200), &p)
	return p
}
func eventAction(t *testing.T, b *browser, e events.Event, action string) events.Event {
	t.Helper()
	var out events.Event
	decode(t, expect(t, b, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: action, Revision: e.Revision}, 200), &out)
	return out
}
func filledEvent(staff int64) events.Input {
	now := time.Now().UTC().Truncate(time.Second)
	start, end, open, close := now.Add(48*time.Hour), now.Add(50*time.Hour), now.Add(-time.Hour), now.Add(24*time.Hour)
	return events.Input{Title: "Бесплатный мастер-класс", Type: "Мастер-класс", Description: "Описание для семьи", Organizer: "AIQ", ResponsibleIDs: []int64{staff}, StartsAt: &start, EndsAt: &end, TimeZone: "Asia/Yekaterinburg", Location: "Зал 1", Capacity: 25, RegistrationStartsAt: &open, RegistrationEndsAt: &close, Conditions: "Для учеников и родителей", RequiredDocuments: "Согласие представителя"}
}
func TestFreeEventsPermissionsPublicationAndLifecycle(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	readers := map[string]*browser{}
	var teacherID int64
	for _, role := range []string{"leader", "teacher", "curator", "methodologist", "education_leader"} {
		u, b := provision(t, f, admin, "events-"+role+"@example.test", role)
		readers[role] = b
		if role == "teacher" {
			teacherID = u.ID
		}
	}
	parentCard := parentCreate(t, admin, map[string]any{"name": "Parent", "email": "events-parent@example.test"})
	studentCard := studentCreate(t, admin, map[string]any{"name": "Student"})
	readers["parent"] = familyUser(t, f, admin, mail, "events-parent@example.test", "parent", parentCard.ID)
	readers["student"] = familyUser(t, f, admin, mail, "events-student@example.test", "student", studentCard.ID)
	e := eventCreate(t, admin, events.Input{Title: "Черновик события"})
	if e.Status != "draft" || e.Revision != 1 || !e.CanEdit || e.RegistrationOpen {
		t.Fatal(e)
	}
	for role, b := range readers {
		want := 404
		total := 0
		if role == "leader" {
			want = 200
			total = 1
		}
		expect(t, b, "GET", eventPath(e.ID), nil, want)
		if eventList(t, b, "").Total != total {
			t.Fatal("draft count leak", role)
		}
		expect(t, b, "GET", "/events/staff", nil, 403)
		expect(t, b, "POST", "/events", filledEvent(teacherID), 403)
		expect(t, b, "PUT", eventPath(e.ID), e.Input, 403)
		expect(t, b, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 403)
	}
	expect(t, f.browser(t), "GET", "/events", nil, 401)
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 400)
	in := filledEvent(teacherID)
	in.Revision = e.Revision
	decode(t, expect(t, admin, "PUT", eventPath(e.ID), in, 200), &e)
	if e.Title != in.Title || len(e.Responsibles) != 1 || e.Responsibles[0].ID != teacherID || e.TimeZone != "Asia/Yekaterinburg" {
		t.Fatal("card fields lost", e)
	}
	var audit bool
	if err := f.pool.QueryRow(ctx, `SELECT changed_fields @> ARRAY['title','responsible_ids','time_zone'] FROM audit_logs WHERE action='event.updated' AND object_id=$1 ORDER BY id DESC LIMIT 1`, e.ID).Scan(&audit); err != nil || !audit {
		t.Fatal("audit fields missing", err)
	}
	// Audit failure must also roll back publication and timestamps.
	if _, err := f.pool.Exec(ctx, `CREATE FUNCTION reject_event_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='event.published' THEN RAISE EXCEPTION 'test'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_event_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_event_audit()`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 500)
	unchanged := eventGet(t, admin, e.ID)
	if unchanged.Revision != e.Revision || unchanged.PublishedAt != nil || unchanged.Status != "draft" {
		t.Fatal("partial publication", unchanged)
	}
	if _, err := f.pool.Exec(ctx, `DROP TRIGGER reject_event_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	e = eventAction(t, admin, e, "publish")
	if !e.RegistrationOpen || e.PublishedAt == nil {
		t.Fatal("publication", e)
	}
	for role, b := range readers {
		card := eventGet(t, b, e.ID)
		if card.CanEdit || len(card.AllowedActions) != 0 {
			t.Fatal("reader acquired management", role)
		}
		if eventList(t, b, "?search=МАСТЕР").Total != 1 {
			t.Fatal("published event invisible", role)
		}
		raw := string(expect(t, b, "GET", eventPath(e.ID), nil, 200))
		if strings.Contains(raw, "@example.test") || strings.Contains(raw, "password_hash") {
			t.Fatal("staff private data leaked")
		}
	}
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "complete", Revision: e.Revision}, 409)
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 409)
	e = eventAction(t, admin, e, "close_registration")
	if e.RegistrationOpen || e.RegistrationClosedAt == nil {
		t.Fatal("registration remains open")
	}
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "complete", Revision: e.Revision}, 409)
	// Advance the fixture's business dates without sleeping for two days.
	if _, err := f.pool.Exec(ctx, `UPDATE free_events SET registration_starts_at=clock_timestamp()-interval '5 days',registration_ends_at=clock_timestamp()-interval '4 days',starts_at=clock_timestamp()-interval '3 days',ends_at=clock_timestamp()-interval '2 days' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	e = eventAction(t, admin, e, "complete")
	if e.CompletedAt == nil || e.Status != "completed" || e.CanEdit || len(e.AllowedActions) != 0 {
		t.Fatal("completion", e)
	}
	expect(t, admin, "PUT", eventPath(e.ID), e.Input, 409)
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 409)
	if _, err := f.pool.Exec(ctx, `UPDATE free_events SET title='changed' WHERE id=$1`, e.ID); err == nil {
		t.Fatal("completed card mutable in DB")
	}
	if eventList(t, readers["parent"], "?status=completed").Total != 1 {
		t.Fatal("history unavailable")
	}
}

func TestFreeEventsValidationConflictsAndCurrentStaff(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	teacher, _ := provision(t, f, admin, "event-responsible@example.test", "teacher")
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacher.ID), map[string]any{"name": "Анна Педагог"}, 200)
	var found events.Page[events.Person]
	decode(t, expect(t, admin, "GET", "/events/staff?search=%D0%B0%D0%BD%D0%BD", nil, 200), &found)
	if found.Total != 1 || len(found.Items) != 1 || found.Items[0].ID != teacher.ID {
		t.Fatal("Cyrillic staff search", found)
	}
	parentCard := parentCreate(t, admin, map[string]any{"name": "Parent", "email": "event-family@example.test"})
	familyUser(t, f, admin, mail, "event-family@example.test", "parent", parentCard.ID)
	var parentID int64
	if err := f.pool.QueryRow(ctx, `SELECT user_id FROM parent_accounts WHERE parent_id=$1`, parentCard.ID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	in := filledEvent(teacher.ID)
	for name, mutate := range map[string]func(*events.Input){
		"external": func(v *events.Input) { v.ResponsibleIDs = []int64{parentID} }, "missing": func(v *events.Input) { v.ResponsibleIDs = []int64{999999} },
		"duplicate": func(v *events.Input) { v.ResponsibleIDs = []int64{teacher.ID, teacher.ID} }, "bad zone": func(v *events.Input) { v.TimeZone = "Wrong/Zone" },
		"ends": func(v *events.Input) { v.EndsAt = v.StartsAt }, "registration": func(v *events.Input) { v.RegistrationEndsAt = v.EndsAt }, "capacity": func(v *events.Input) { v.Capacity = -1 },
	} {
		t.Run(name, func(t *testing.T) { bad := in; mutate(&bad); expect(t, admin, "POST", "/events", bad, 400) })
	}
	e := eventCreate(t, admin, in)
	csrf := admin.csrf
	admin.csrf = "bad"
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 403)
	admin.csrf = csrf
	expect(t, admin, "POST", "/events", map[string]any{"title": "Bypass", "status": "published"}, 400)
	expect(t, admin, "GET", "/events/not-an-id", nil, 400)
	expect(t, admin, "GET", "/events?page=0", nil, 400)
	expect(t, admin, "GET", "/events?status=unknown", nil, 400)
	in = e.Input
	in.Title = "Первый редактор"
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); code, _ := admin.do(t, "PUT", eventPath(e.ID), in); codes <- code }()
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("lost update", counts)
	}
	e = eventGet(t, admin, e.ID)
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: 1}, 409)
	// Blocked responsible is removed from picker and prevents publication.
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacher.ID), map[string]any{"blocked": true}, 200)
	var staff events.Page[events.Person]
	decode(t, expect(t, admin, "GET", "/events/staff?search=", nil, 200), &staff)
	for _, p := range staff.Items {
		if p.ID == teacher.ID || p.ID == parentID {
			t.Fatal("ineligible picker entry", p)
		}
	}
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 400)
	in = e.Input
	in.ResponsibleIDs = []int64{f.admin.ID}
	decode(t, expect(t, admin, "PUT", eventPath(e.ID), in, 200), &e)
	// Expired registration cannot be published.
	if _, err := f.pool.Exec(ctx, `UPDATE free_events SET registration_starts_at=clock_timestamp()-interval '2 hours',registration_ends_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", eventPath(e.ID)+"/actions", events.ActionInput{Action: "publish", Revision: e.Revision}, 400)
	in = e.Input
	in.RegistrationEndsAt = filledEvent(f.admin.ID).RegistrationEndsAt
	decode(t, expect(t, admin, "PUT", eventPath(e.ID), in, 200), &e)
	e = eventAction(t, admin, e, "publish")
	if _, err := f.pool.Exec(ctx, `UPDATE free_events SET registration_starts_at=clock_timestamp()-interval '2 hours',registration_ends_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	e = eventGet(t, admin, e.ID)
	if e.RegistrationOpen || e.Status != "published" {
		t.Fatal("window expiry semantics", e)
	}
	// Pagination and search count only the accessible set.
	if _, err := f.pool.Exec(ctx, `INSERT INTO free_events(title,title_search,created_by) SELECT 'Private '||v,'private '||v,$1 FROM generate_series(1,22) v`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	p := eventList(t, admin, "")
	if p.Total != 23 || len(p.Items) != 20 {
		t.Fatal(p)
	}
	if len(eventList(t, admin, "?page=2").Items) != 3 {
		t.Fatal("pagination")
	}
}
