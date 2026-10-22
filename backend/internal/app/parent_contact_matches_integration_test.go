package app_test

import (
	"aiq.local/core/internal/domain/people"
	"aiq.local/core/internal/modules/parents"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/platform/httpx"
)

func contactMatches(t *testing.T, b *browser, query string, body any) people.Page[parents.ContactMatch] {
	t.Helper()
	raw := expect(t, b, "POST", "/parents/contact-matches"+query, body, 200)
	var shape struct {
		Items []map[string]any `json:"items"`
	}
	decode(t, raw, &shape)
	for _, item := range shape.Items {
		if len(item) != 4 || item["id"] == nil || item["name"] == nil || item["status"] == nil || item["matched_fields"] == nil {
			t.Fatalf("match contains unexpected fields: %s", raw)
		}
	}
	var result people.Page[parents.ContactMatch]
	decode(t, raw, &result)
	if result.Items == nil {
		t.Fatal("items must be an array, including empty results")
	}
	return result
}

func TestParentContactMatchesNormalizationAndIndependentCards(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	first := parentCreate(t, admin, map[string]any{"name": "Одно имя", "phone": "8 (999) 123-45-67", "email": "  Shared@Example.test  "})
	phoneOnly := parentCreate(t, admin, map[string]any{"name": "Одно имя", "phone": "+7 999 123 45 67", "email": "other@example.test", "status": "archived"})
	emailOnly := parentCreate(t, admin, map[string]any{"name": "Одно имя", "phone": "+44 20 1234 5678", "email": "shared@example.test"})
	nameOnly := parentCreate(t, admin, map[string]any{"name": "Одно имя", "email": "distinct@example.test"})
	if first.Phone != "+79991234567" || first.Email != "shared@example.test" {
		t.Fatal("creation did not use the common contact format")
	}
	want := []parents.ContactMatch{
		{ID: first.ID, Name: first.Name, Status: "active", MatchedFields: []string{"phone", "email"}},
		{ID: phoneOnly.ID, Name: phoneOnly.Name, Status: "archived", MatchedFields: []string{"phone"}},
		{ID: emailOnly.ID, Name: emailOnly.Name, Status: "active", MatchedFields: []string{"email"}},
	}
	for _, phone := range []string{"8 (999) 123-45-67", "79991234567", "+79991234567", " +7\u00a0999\u00a0123-45-67 "} {
		page := contactMatches(t, admin, "", map[string]any{"phone": phone, "email": " SHARED@EXAMPLE.TEST "})
		if page.Total != 3 || page.Page != 1 || page.PageSize != 20 || !reflect.DeepEqual(page.Items, want) {
			t.Fatalf("normalized OR match/archive/name-only/deduplication: %+v", page)
		}
	}
	page := contactMatches(t, admin, "", map[string]any{"phone": "89991234567", "email": "shared@example.test", "exclude_id": first.ID})
	if page.Total != 2 || !reflect.DeepEqual(page.Items, want[1:]) {
		t.Fatal("editing did not exclude exactly its own card")
	}
	page = contactMatches(t, admin, "", map[string]any{"email": "DISTINCT@example.test", "exclude_id": nameOnly.ID})
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatal("own contact or identical names produced a warning")
	}
	page = contactMatches(t, admin, "", map[string]any{"phone": "+44 (20) 1234-5678"})
	if page.Total != 1 || page.Items[0].ID != emailOnly.ID || !reflect.DeepEqual(page.Items[0].MatchedFields, []string{"phone"}) {
		t.Fatal("international phone normalization differs from parent saving")
	}

	// Saving does not require a check token or merge people sharing a contact/name.
	student := studentCreate(t, admin, map[string]any{"name": "Child"})
	linkParent(t, admin, student.ID, first.ID, true)
	duplicate := parentCreate(t, admin, map[string]any{"name": first.Name, "phone": "8 999 1234567", "email": "SHARED@example.test"})
	if duplicate.ID == first.ID {
		t.Fatal("creation merged two parents")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", nameOnly.ID), map[string]any{"phone": "7 (999) 123-45-67", "email": " Shared@Example.test "}, 200)
	page = contactMatches(t, admin, "", map[string]any{"phone": "89991234567", "email": "shared@example.test", "exclude_id": nameOnly.ID})
	if page.Total != 4 {
		t.Fatal("editing common contacts was blocked or merged existing cards")
	}
	var parentCount, linkCount int
	var parentID int64
	if err := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM parents),count(*),min(parent_id) FROM student_parents WHERE student_id=$1`, student.ID).Scan(&parentCount, &linkCount, &parentID); err != nil {
		t.Fatal(err)
	}
	if parentCount != 5 || linkCount != 1 || parentID != first.ID {
		t.Fatal("duplicate handling changed people or family links")
	}
}

func TestParentContactMatchesPaginationAndValidation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	var ids []int64
	for i := 0; i < 25; i++ {
		parent := parentCreate(t, admin, map[string]any{"name": "Одинаковое имя", "email": "page@example.test"})
		ids = append(ids, parent.ID)
	}
	body := map[string]any{"email": "PAGE@EXAMPLE.TEST"}
	var got []int64
	for number := 1; number <= 2; number++ {
		page := contactMatches(t, admin, fmt.Sprintf("?page=%d", number), body)
		if page.Page != number || page.PageSize != 20 || page.Total != 25 {
			t.Fatal("incorrect pagination metadata")
		}
		for _, item := range page.Items {
			got = append(got, item.ID)
		}
	}
	if !reflect.DeepEqual(got, ids) {
		t.Fatal("identical names caused unstable pagination")
	}
	page := contactMatches(t, admin, "?page=2&page_size=100", body)
	if page.Page != 2 || page.PageSize != 100 || page.Total != 25 || len(page.Items) != 0 {
		t.Fatal("page past the end must retain the total and return []")
	}
	for _, query := range []string{"?page=0", "?page=-1", "?page=bad", "?page=1000001", "?page_size=0", "?page_size=101", "?page_size=bad", "?phone=89991234567", "?email=page%40example.test", "?search=page%40example.test", "?status=active"} {
		expect(t, admin, "POST", "/parents/contact-matches"+query, body, 400)
	}
	for _, invalid := range []map[string]any{{}, {"phone": " "}, {"phone": "bad", "email": "page@example.test"}, {"email": "bad"}, {"email": "page@example.test", "exclude_id": 0}, {"email": "page@example.test", "exclude_id": -1}, {"email": "page@example.test", "exclude_id": "1"}, {"email": "page@example.test", "name": "Одинаковое имя"}, {"phone": strings.Repeat("1", 101)}, {"email": strings.Repeat("a", 255) + "@example.test"}} {
		expect(t, admin, "POST", "/parents/contact-matches", invalid, 400)
	}
}

func TestParentContactMatchesPermissionsAndPrivacy(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	const phone, email = "+79994561234", "private-match@example.test"
	parent := parentCreate(t, admin, map[string]any{"name": "Private contact holder", "phone": phone, "email": email})
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", parent.ID), map[string]any{"email": strings.ToUpper(email)}, 200)
	body := map[string]any{"phone": "8 (999) 456-12-34", "email": strings.ToUpper(email)}
	ctx := context.Background()
	var before, after int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	contactMatches(t, admin, "", body)
	expect(t, admin, "POST", "/parents/contact-matches", map[string]any{"email": email, "exclude_id": -1}, 400)
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&after); err != nil || before != after {
		t.Fatal("contact lookup wrote audit")
	}
	for _, role := range []string{"teacher", "curator", "leader"} {
		_, reader := provision(t, f, admin, "r03-"+role+"@example.test", role)
		expect(t, reader, "POST", "/parents/contact-matches", body, 403)
	}
	expect(t, f.browser(t), "POST", "/parents/contact-matches", body, 401)
	csrf := admin.csrf
	admin.csrf = ""
	expect(t, admin, "POST", "/parents/contact-matches", body, 403)
	admin.csrf = csrf
	var audit string
	if err := f.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a))::text FROM audit_logs a`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{phone, "9994561234", "8 (999) 456-12-34", email} {
		if strings.Contains(strings.ToLower(output.String()), strings.ToLower(private)) || strings.Contains(strings.ToLower(audit), strings.ToLower(private)) {
			t.Fatal("contacts leaked into technical logs or audit")
		}
	}
	if !strings.Contains(output.String(), "POST /api/v1/parents/contact-matches") {
		t.Fatal("privacy assertion did not observe endpoint logs")
	}

	// Repository authorization re-reads the account even with a stale admin actor.
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))}
	for _, change := range []struct {
		sql  string
		want int
	}{
		{`UPDATE user_roles SET role='leader' WHERE user_id=$1`, 403},
		{`UPDATE users SET blocked=true WHERE id=$1`, 401},
		{`UPDATE users SET blocked=false WHERE id=$1;`, 403},
		{`DELETE FROM sessions WHERE user_id=$1`, 401},
	} {
		if _, err := f.pool.Exec(ctx, change.sql, actor.ID); err != nil {
			t.Fatal(err)
		}
		v, err := (&parents.Repository{Pool: f.pool}).ContactMatches(httpx.WithActor(ctx, actor), parents.ContactMatchInput{Email: email}, people.Filter{Page: 1, Size: 20})
		var denied *httpx.Error
		if !errors.As(err, &denied) || denied.Status != change.want || len(v.Items) != 0 {
			t.Fatalf("stale authorization allowed contact lookup: %v", err)
		}
	}
}
