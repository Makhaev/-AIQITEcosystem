package app_test

import (
	"aiq.local/core/internal/modules/events"
	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/files"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestMonth4PaginationAndCompatibility(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	child := studentCreate(t, admin, map[string]any{"name": "Own child"})
	student := familyUser(t, f, admin, mail, "api-contract@example.test", "student", child.ID)
	own := expect(t, student, "GET", "/family/home", nil, 200)
	byID := expect(t, student, "GET", fmt.Sprintf("/family/students/%d/home", child.ID), nil, 200)
	if string(own) != string(byID) {
		t.Fatal("own summary alias differs")
	}
	expect(t, admin, "GET", "/family/home", nil, 403)
	if _, err := f.pool.Exec(ctx, `INSERT INTO free_events(title,title_search,created_by) SELECT 'Event '||n,'event '||n,$1 FROM generate_series(1,33)n`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO notifications(user_id,kind,object_id,event_version,audience) SELECT $1,'password_reset',n,'api11','account' FROM generate_series(1,33)n`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO invitations(email,role,student_id,token_hash,created_by,created_at,expires_at,revoked_at) SELECT 'old'||n||'@example.test','student',$1,decode(lpad(n::text,64,'0'),'hex'),$2,'2026-10-01T10:00Z','2026-10-03T10:00Z','2026-10-01T11:00Z' FROM generate_series(1,33)n`, child.ID, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/events", "/invitations", "/notifications", "/notification-deliveries", "/events/staff", "/family-reports", "/family-requests", fmt.Sprintf("/students/%d/family-reports", child.ID), fmt.Sprintf("/students/%d/family-requests", child.ID), "/account/links"}
	type page struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
		Page  int               `json:"page"`
		Size  int               `json:"page_size"`
	}
	check := func(b *browser, path string) {
		t.Helper()
		for _, size := range []int{1, 7, 100} {
			var p page
			decode(t, expect(t, b, "GET", fmt.Sprintf("%s?page_size=%d", path, size), nil, 200), &p)
			if p.Page != 1 || p.Size != size || p.Items == nil || len(p.Items) > size {
				t.Fatalf("%s %+v", path, p)
			}
		}
		for _, query := range []string{"?page_size=0", "?page_size=101", "?page_size=bad", "?page_size=1&page_size=2", "?page=0"} {
			expect(t, b, "GET", path+query, nil, 400)
		}
	}
	for _, path := range paths {
		check(admin, path)
	}
	base := fmt.Sprintf("/family/students/%d", child.ID)
	for _, path := range []string{"/family/students", "/family/requests", base + "/lessons", base + "/learning", base + "/reports", base + "/requests", base + "/events", base + "/notifications"} {
		check(student, path)
	}
	var first, second, all, defaultPage page
	decode(t, expect(t, admin, "GET", "/events", nil, 200), &defaultPage)
	if defaultPage.Size != 20 || len(defaultPage.Items) != 20 {
		t.Fatal("legacy default", defaultPage)
	}
	decode(t, expect(t, admin, "GET", "/events?page_size=7", nil, 200), &first)
	decode(t, expect(t, admin, "GET", "/events?page=2&page_size=7", nil, 200), &second)
	decode(t, expect(t, admin, "GET", "/events?page_size=100", nil, 200), &all)
	if all.Total != 33 || len(all.Items) != 33 || string(first.Items[0]) != string(all.Items[0]) || string(second.Items[0]) != string(all.Items[7]) {
		t.Fatal("unstable pagination")
	}
	decode(t, expect(t, admin, "GET", "/events?page=100&page_size=7", nil, 200), &first)
	if first.Total != 33 || len(first.Items) != 0 {
		t.Fatal("empty page loses total")
	}
	e := eventAction(t, admin, eventCreate(t, admin, filledEvent(f.admin.ID)), "publish")
	v := registerEvent(t, admin, e.ID, child.ID)
	for _, path := range []string{eventPath(e.ID) + "/candidates", eventPath(e.ID) + "/registrations", registrationPath(e.ID, v.ID) + "/documents", registrationPath(e.ID, v.ID) + "/history"} {
		check(admin, path)
	}
	var history []events.History
	decode(t, expect(t, student, "GET", registrationPath(e.ID, v.ID)+"/history", nil, 200), &history)
	if len(history) != 1 {
		t.Fatal("legacy history changed")
	}
	var legacy []family.ReportNotification
	decode(t, expect(t, student, "GET", base+"/notifications", nil, 200), &legacy)
	var doc files.File
	decode(t, expect(t, student, "POST", registrationPath(e.ID, v.ID)+"/documents", files.Input{Name: "result.txt", Data: []byte("Result")}, 201), &doc)
	check(student, registrationPath(e.ID, v.ID)+"/documents")
}

func TestMonth4StrictJSONErrorsAndAtomicAttendanceCorrection(t *testing.T) {
	f, admin, _ := setupInvitations(t)
	ctx := context.Background()
	e := eventAction(t, admin, eventCreate(t, admin, filledEvent(f.admin.ID)), "publish")
	child := studentCreate(t, admin, map[string]any{"name": "Participant"})
	v := registerEvent(t, admin, e.ID, child.ID)
	v = registrationAction(t, admin, e.ID, v, "confirmed")
	if _, err := f.pool.Exec(ctx, `UPDATE free_events SET registration_starts_at=clock_timestamp()-interval '4 days',registration_ends_at=clock_timestamp()-interval '3 days',starts_at=clock_timestamp()-interval '2 days',ends_at=clock_timestamp()-interval '1 day' WHERE id=$1`, e.ID); err != nil {
		t.Fatal(err)
	}
	v = registrationAction(t, admin, e.ID, v, "attended")
	v = registrationAction(t, admin, e.ID, v, "no_show")
	v = registrationAction(t, admin, e.ID, v, "attended")
	var h []events.History
	decode(t, expect(t, admin, "GET", registrationPath(e.ID, v.ID)+"/history", nil, 200), &h)
	if len(h) != 5 || h[3].Status != "no_show" || h[4].Status != "attended" {
		t.Fatal("correction lost history", h)
	}
	for _, tc := range []struct{ path, body string }{{"/events", `null`}, {"/events", `{"title":"one","title":"two"}`}, {"/events", `{"title":"one","Title":"two"}`}, {"/events", `{"title":"new","unknown":true}`}, {"/invitations/1/resend", `{"email":"injected@example.test"}`}, {"/invitations/1/revoke", `null`}, {"/notifications/1/read", `{"read":true}`}, {"/auth/password-reset/request", `{"email":"first@example.test","email":"other@example.test"}`}} {
		req, err := http.NewRequest("POST", f.server.URL+"/api/v1"+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", admin.csrf)
		resp, err := admin.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Code      string            `json:"code"`
			Message   string            `json:"message"`
			Fields    map[string]string `json:"field_errors"`
			RequestID string            `json:"request_id"`
		}
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 400 || payload.Code == "" || payload.Message == "" || payload.Fields == nil || payload.RequestID == "" || payload.RequestID != resp.Header.Get("X-Request-ID") {
			t.Fatalf("%s: %d %+v %v", tc.path, resp.StatusCode, payload, err)
		}
	}
}
