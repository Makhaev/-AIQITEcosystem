package app_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/app"
	"aiq.local/core/internal/modules/invitations"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/config"
)

type invitationMail struct {
	mu         sync.Mutex
	links      []string
	recipients []string
	fail       bool
}

func (m *invitationMail) Ready() error { return nil }
func (m *invitationMail) Send(_ context.Context, email, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links = append(m.links, link)
	m.recipients = append(m.recipients, email)
	if m.fail {
		return errors.New("synthetic delivery failure")
	}
	return nil
}
func (m *invitationMail) token(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	u, e := url.Parse(m.links[len(m.links)-1])
	if e != nil {
		t.Fatal(e)
	}
	if u.RawQuery != "" || !strings.HasPrefix(u.Fragment, "/invitation?") {
		t.Fatal("token must only be in fragment")
	}
	q, e := url.ParseQuery(strings.SplitN(u.Fragment, "?", 2)[1])
	if e != nil {
		t.Fatal(e)
	}
	return q.Get("token")
}
func setupInvitations(t *testing.T) (*fixture, *browser, *invitationMail) {
	f := setup(t)
	mail := &invitationMail{}
	h, _, err := app.RouterWithInvitationSender(f.pool, config.Config{Origin: origin}, mail)
	if err != nil {
		t.Fatal(err)
	}
	f.server.Close()
	f.server = httptest.NewServer(h)
	t.Cleanup(f.server.Close)
	return f, f.adminBrowser(t), mail
}
func issue(t *testing.T, b *browser, email, role string, id int64) invitations.Invitation {
	t.Helper()
	var inv invitations.Invitation
	decode(t, expect(t, b, "POST", "/invitations", invitations.Input{Email: email, Role: role, CardID: id}, 201), &inv)
	return inv
}
func TestInvitationsUnconfiguredMailDoesNotCreateInvitation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := parentCreate(t, admin, map[string]any{"name": "No mail parent", "email": "no-mail@example.test"})
	expect(t, admin, "POST", "/invitations", invitations.Input{Email: "no-mail@example.test", Role: "parent", CardID: p.ID}, 503)
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM invitations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unconfigured mail must not persist invitations")
	}
}
func TestInvitationsNewAccountHashBindingAndScope(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	p := parentCreate(t, admin, map[string]any{"name": "Invited parent", "email": "same@example.test", "phone": "+79990001122"})
	p2 := parentCreate(t, admin, map[string]any{"name": "Different parent", "email": "other@example.test"})
	student := studentCreate(t, admin, map[string]any{"name": "Existing student"})
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", student.ID), map[string]any{"parent_id": p2.ID, "relationship": "parent", "is_primary": true}, 201)
	inv := issue(t, admin, " INVITED@EXAMPLE.TEST ", "parent", p.ID)
	token := mail.token(t)
	if inv.Email != "invited@example.test" || inv.ExpiresAt.Sub(inv.CreatedAt) != 48*time.Hour || inv.CreatedBy != f.admin.ID {
		t.Fatalf("incorrect invitation metadata: %+v", inv)
	}
	var stored []byte
	if err := f.pool.QueryRow(ctx, `SELECT token_hash FROM invitations WHERE id=$1`, inv.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(token))
	if !bytes.Equal(stored, hash[:]) {
		t.Fatal("token was not hashed")
	}
	list := expect(t, admin, "GET", "/invitations", nil, 200)
	if bytes.Contains(list, []byte(token)) || bytes.Contains(list, []byte("token_hash")) {
		t.Fatal("list discloses capability")
	}
	visitor := f.browser(t)
	var preview invitations.Preview
	decode(t, expect(t, visitor, "POST", "/invitations/inspect", invitations.TokenInput{Token: token}, 200), &preview)
	if preview.RequiresLogin || preview.Role != "parent" {
		t.Fatal("wrong preview")
	}
	expect(t, visitor, "POST", "/invitations/accept", map[string]any{"token": token, "password": userPassword, "role": "admin"}, 400)
	expect(t, visitor, "POST", "/invitations/accept", map[string]any{"token": token, "password": userPassword, "card_id": p2.ID}, 400)
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: "short"}, 400)
	visitor.csrf = "invalid"
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 403)
	visitor = f.browser(t)
	expect(t, admin, "POST", "/invitations/accept", invitations.AcceptInput{Token: token}, 403)
	var result invitations.Result
	decode(t, expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 200), &result)
	if !result.CreatedAccount || result.CardID != p.ID || result.Role != "parent" {
		t.Fatal("binding changed")
	}
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 410)
	visitor.login(t, inv.Email, userPassword)
	var u users.User
	decode(t, expect(t, visitor, "GET", "/auth/me", nil, 200), &u)
	if len(u.Roles) != 1 || u.Roles[0] != "parent" || u.MustChangePassword {
		t.Fatal("unexpected permissions")
	}
	var links struct {
		Items []invitations.Link `json:"items"`
	}
	decode(t, expect(t, visitor, "GET", "/account/links", nil, 200), &links)
	if len(links.Items) != 1 || links.Items[0].CardID != p.ID {
		t.Fatal("missing authorized binding")
	}
	for _, path := range []string{"/users", "/students", "/parents", "/programs", "/invitations", "/audit-logs"} {
		expect(t, visitor, "GET", path, nil, 403)
	}
	expect(t, visitor, "POST", "/invitations", invitations.Input{Email: "attacker@example.test", Role: "student", CardID: student.ID}, 403)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM student_parents`).Scan(&count); err != nil || count != 1 {
		t.Fatal("family relationships changed", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM parent_accounts WHERE parent_id=$1 AND user_id=$2`, p.ID, u.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("account binding missing", err)
	}
	expect(t, admin, "POST", fmt.Sprintf("/invitations/%d/revoke", inv.ID), map[string]any{}, 409)
	expect(t, admin, "POST", fmt.Sprintf("/invitations/%d/resend", inv.ID), map[string]any{}, 409)
	expect(t, admin, "POST", "/users", map[string]any{"name": "Bypass", "email": "bypass@example.test", "password": userPassword, "roles": []string{"parent"}}, 400)
	second := issue(t, admin, inv.Email, "student", student.ID)
	secondToken := mail.token(t)
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: secondToken}, 200)
	if second.Role != "student" {
		t.Fatal("wrong role")
	}
	decode(t, expect(t, visitor, "GET", "/account/links", nil, 200), &links)
	if len(links.Items) != 2 {
		t.Fatal("second explicit role binding missing")
	}
}
func TestInvitationsExistingAccountRequiresMatchingLiveSession(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	account := create(t, admin, "existing@example.test", "teacher")
	if _, err := f.pool.Exec(ctx, `UPDATE users SET must_change_password=false WHERE id=$1`, account.ID); err != nil {
		t.Fatal(err)
	}
	p := parentCreate(t, admin, map[string]any{"name": "Explicit parent", "email": "unrelated@example.test"})
	issue(t, admin, account.Email, "parent", p.ID)
	token := mail.token(t)
	anonymous := f.browser(t)
	expect(t, anonymous, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 401)
	expect(t, admin, "POST", "/invitations/accept", invitations.AcceptInput{Token: token}, 403)
	owner := f.browser(t)
	owner.login(t, account.Email, userPassword)
	expect(t, owner, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: "replacement-password"}, 400)
	if _, err := f.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, account.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, owner, "POST", "/invitations/accept", invitations.AcceptInput{Token: token}, 401)
	owner = f.browser(t)
	owner.login(t, account.Email, userPassword)
	expect(t, owner, "POST", "/invitations/accept", invitations.AcceptInput{Token: token}, 200)
	var current users.User
	decode(t, expect(t, owner, "GET", "/auth/me", nil, 200), &current)
	if current.ID != account.ID || len(current.Roles) != 2 {
		t.Fatal("existing account replaced or roles lost")
	}
	f.browser(t).login(t, account.Email, userPassword)
	p2 := parentCreate(t, admin, map[string]any{"name": "Other identity", "email": "different@example.test"})
	expect(t, admin, "POST", "/invitations", invitations.Input{Email: account.Email, Role: "parent", CardID: p2.ID}, 409)
	expect(t, admin, "POST", "/invitations", invitations.Input{Email: "different@example.test", Role: "parent", CardID: p.ID}, 409)
}
func TestInvitationsResendExpiryRevocationAndMailFailure(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	visitor := f.browser(t)
	s := studentCreate(t, admin, map[string]any{"name": "Invited student"})
	first := issue(t, admin, "student@example.test", "student", s.ID)
	old := mail.token(t)
	var next invitations.Invitation
	decode(t, expect(t, admin, "POST", fmt.Sprintf("/invitations/%d/resend", first.ID), map[string]any{}, 201), &next)
	token := mail.token(t)
	if token == old || next.ID == first.ID {
		t.Fatal("resend reused token or record")
	}
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: old, Password: userPassword}, 410)
	if _, err := f.pool.Exec(ctx, `UPDATE invitations SET created_at=created_at-interval '49 hours',expires_at=expires_at-interval '49 hours' WHERE id=$1`, next.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, visitor, "POST", "/invitations/inspect", invitations.TokenInput{Token: token}, 410)
	decode(t, expect(t, admin, "POST", fmt.Sprintf("/invitations/%d/resend", next.ID), map[string]any{}, 201), &next)
	token = mail.token(t)
	expect(t, admin, "POST", fmt.Sprintf("/invitations/%d/revoke", next.ID), map[string]any{}, 204)
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 410)
	mail.fail = true
	expect(t, admin, "POST", fmt.Sprintf("/invitations/%d/resend", next.ID), map[string]any{}, 502)
	expect(t, visitor, "POST", "/invitations/inspect", invitations.TokenInput{Token: mail.token(t)}, 410)
	var live int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM invitations WHERE used_at IS NULL AND revoked_at IS NULL`).Scan(&live); err != nil || live != 0 {
		t.Fatal("failed delivery left active token", err)
	}
	mail.fail = false
	issue(t, admin, "student@example.test", "student", s.ID)
	token = mail.token(t)
	if _, err := f.pool.Exec(ctx, `UPDATE students SET status='archived' WHERE id=$1`, s.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 409)
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE email='student@example.test'`).Scan(&live); err != nil || live != 0 {
		t.Fatal("failed acceptance created account", err)
	}
}
func TestInvitationsConcurrentAcceptanceAndRollback(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	s := studentCreate(t, admin, map[string]any{"name": "Race student"})
	inv := issue(t, admin, "race@example.test", "student", s.ID)
	token := mail.token(t)
	clients := []*browser{f.browser(t), f.browser(t)}
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for n := range clients {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			codes[n], _ = clients[n].do(t, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword})
		}(n)
	}
	wg.Wait()
	if !((codes[0] == 200 && codes[1] == 410) || (codes[1] == 200 && codes[0] == 410)) {
		t.Fatalf("duplicate token consumption: %v", codes)
	}
	var accounts, links, uses int
	if err := f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE email='race@example.test'),(SELECT count(*) FROM student_accounts WHERE student_id=$1),(SELECT count(*) FROM invitations WHERE id=$2 AND used_at IS NOT NULL)`, s.ID, inv.ID).Scan(&accounts, &links, &uses); err != nil || accounts != 1 || links != 1 || uses != 1 {
		t.Fatal("non-atomic acceptance", err)
	}
	p := parentCreate(t, admin, map[string]any{"name": "Rollback parent", "email": "rollback@example.test"})
	inv = issue(t, admin, "rollback@example.test", "parent", p.ID)
	token = mail.token(t)
	_, err := f.pool.Exec(ctx, `CREATE FUNCTION fail_invitation_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='invitation.accepted' THEN RAISE EXCEPTION 'synthetic rollback'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_invitation_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_invitation_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	visitor := f.browser(t)
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 500)
	if err = f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE email='rollback@example.test'),(SELECT count(*) FROM parent_accounts WHERE parent_id=$1),(SELECT count(*) FROM invitations WHERE id=$2 AND used_at IS NOT NULL)`, p.ID, inv.ID).Scan(&accounts, &links, &uses); err != nil || accounts != 0 || links != 0 || uses != 0 {
		t.Fatal("failed transaction leaked state", err)
	}
	if _, err = f.pool.Exec(ctx, `DROP TRIGGER fail_invitation_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	expect(t, visitor, "POST", "/invitations/accept", invitations.AcceptInput{Token: token, Password: userPassword}, 200)
}
