package app_test

import (
	"aiq.local/core/internal/domain/people"
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/parents"
	"aiq.local/core/internal/modules/students"
	"aiq.local/core/internal/platform/httpx"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"sync"
	"testing"
	"time"
)

func studentCreate(t *testing.T, b *browser, body map[string]any) people.Student {
	t.Helper()
	var v people.Student
	decode(t, expect(t, b, "POST", "/students", body, 201), &v)
	return v
}
func parentCreate(t *testing.T, b *browser, body map[string]any) people.Parent {
	t.Helper()
	var v people.Parent
	decode(t, expect(t, b, "POST", "/parents", body, 201), &v)
	return v
}
func studentCard(t *testing.T, b *browser, id int64) people.StudentCard {
	t.Helper()
	var v people.StudentCard
	decode(t, expect(t, b, "GET", fmt.Sprintf("/students/%d", id), nil, 200), &v)
	return v
}
func linkPath(s, p int64) string { return fmt.Sprintf("/students/%d/parents/%d", s, p) }
func linkParent(t *testing.T, b *browser, s, p int64, primary bool) {
	t.Helper()
	expect(t, b, "POST", fmt.Sprintf("/students/%d/parents", s), map[string]any{"parent_id": p, "relationship": "Мать", "is_primary": primary}, 201)
}

func TestFamiliesLifecycleValidationAndHistory(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	curator, _ := provision(t, f, admin, "families-curator@example.test", "curator")
	s := studentCreate(t, admin, map[string]any{"name": "  Иван Иванов  ", "birth_date": "2013-05-10", "curator_id": curator.ID})
	same := studentCreate(t, admin, map[string]any{"name": "Иван Иванов"})
	p := parentCreate(t, admin, map[string]any{"name": "  Анна Иванова ", "phone": "8 (900) 123-45-67", "email": " PARENT@example.test "})
	second := parentCreate(t, admin, map[string]any{"name": "Анна Иванова", "email": "parent@example.test"})
	if s.Name != "Иван Иванов" || s.CuratorID == nil || *s.CuratorID != curator.ID || s.BirthDate == nil || *s.BirthDate != "2013-05-10" || s.ID == same.ID || p.ID == second.ID || p.Phone != "+79001234567" || p.Email != "parent@example.test" {
		t.Fatalf("normalization or merging: %+v %+v", s, p)
	}
	linkParent(t, admin, s.ID, p.ID, true)
	linkParent(t, admin, s.ID, second.ID, false)
	linkParent(t, admin, same.ID, p.ID, true)
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", s.ID), map[string]any{"parent_id": p.ID, "relationship": "Родитель"}, 409)
	expect(t, admin, "PATCH", linkPath(s.ID, second.ID), map[string]any{"is_primary": true}, 409)
	expect(t, admin, "PATCH", linkPath(s.ID, p.ID), map[string]any{"is_primary": false}, 200)
	expect(t, admin, "PATCH", linkPath(s.ID, second.ID), map[string]any{"is_primary": true, "relationship": "Опекун"}, 200)
	c := studentCard(t, admin, s.ID)
	if len(c.Parents) != 2 || !c.Parents[0].IsPrimary || c.Parents[0].ParentID != second.ID || c.Parents[0].Relationship != "Опекун" {
		t.Fatalf("many parents/primary: %+v", c)
	}
	var parent people.ParentCard
	decode(t, expect(t, admin, "GET", fmt.Sprintf("/parents/%d", p.ID), nil, 200), &parent)
	if len(parent.Children) != 2 {
		t.Fatal("shared parent lost child")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", s.ID), map[string]any{"birth_date": nil, "curator_id": nil}, 200)
	c = studentCard(t, admin, s.ID)
	if c.BirthDate != nil || c.CuratorID != nil || c.CuratorName != nil {
		t.Fatalf("nullable clear: %+v", c)
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", p.ID), map[string]any{"phone": nil}, 200)
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", p.ID), map[string]any{"email": nil}, 400)
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", p.ID), map[string]any{"email": nil, "phone": "+79009998877"}, 200)
	var page people.Page[people.Student]
	decode(t, expect(t, admin, "GET", "/students?search="+url.QueryEscape("ИВАН")+"&page_size=1&page=2", nil, 200), &page)
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != same.ID {
		t.Fatalf("Cyrillic search/pagination: %+v", page)
	}
	studentCreate(t, admin, map[string]any{"name": "Имя %_ буквально"})
	decode(t, expect(t, admin, "GET", "/students?search="+url.QueryEscape("%_"), nil, 200), &page)
	if page.Total != 1 {
		t.Fatal("wildcard search is not literal")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", s.ID), map[string]any{"status": "archived"}, 200)
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", p.ID), map[string]any{"status": "archived"}, 200)
	if len(studentCard(t, admin, s.ID).Parents) != 2 {
		t.Fatal("archive destroyed links")
	}
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", s.ID), map[string]any{"parent_id": second.ID, "relationship": "Отец"}, 409)
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", same.ID), map[string]any{"parent_id": p.ID, "relationship": "Отец"}, 409)
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", s.ID), map[string]any{"status": "active", "curator_id": curator.ID}, 200)
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", p.ID), map[string]any{"status": "active"}, 200)
	for _, body := range []map[string]any{{}, {"name": " "}, {"name": "X", "birth_date": "2013-02-30"}, {"name": "X", "birth_date": "2999-01-01"}, {"name": "X", "birth_date": "2013-5-10"}, {"name": "X", "curator_id": f.admin.ID}, {"name": "X", "curator_id": 99999}, {"name": "X", "user_id": f.admin.ID}, {"name": "X", "status": "deleted"}} {
		expect(t, admin, "POST", "/students", body, 400)
	}
	for _, body := range []map[string]any{{"name": "X"}, {"name": "X", "phone": "  ", "email": ""}, {"name": "X", "phone": "123"}, {"name": "X", "email": "invalid"}, {"name": "X", "email": "x@example.test", "user_id": 1}} {
		expect(t, admin, "POST", "/parents", body, 400)
	}
	for _, body := range []map[string]any{{}, {"name": nil}, {"status": nil}, {"status": ""}, {"birth_date": ""}} {
		expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", s.ID), body, 400)
	}
	for _, body := range []map[string]any{{}, {"name": nil}, {"status": nil}, {"phone": "", "email": nil}} {
		expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", p.ID), body, 400)
	}
	for _, body := range []map[string]any{{}, {"relationship": nil}, {"relationship": " "}, {"is_primary": nil}} {
		expect(t, admin, "PATCH", linkPath(s.ID, p.ID), body, 400)
	}
	expect(t, admin, "PATCH", linkPath(s.ID, 99999), map[string]any{"is_primary": false}, 404)
	for _, path := range []string{"/students/99999", "/parents/99999"} {
		expect(t, admin, "GET", path, nil, 404)
	}
	for _, path := range []string{"/students/bad", "/parents/0", "/students?page=0", "/parents?page_size=101", "/parents?status=deleted"} {
		expect(t, admin, "GET", path, nil, 400)
	}
	expect(t, admin, "DELETE", fmt.Sprintf("/students/%d", s.ID), nil, 404)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_type='student' AND object_id=$1 AND action='student.archived'`, s.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("archive audit: %d %v", count, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("cards created accounts: %d %v", count, err)
	}
	// Contacts and names are deliberately non-unique. Constraints protect relationships independently of HTTP.
	_, err := f.pool.Exec(ctx, `INSERT INTO parents(name,name_search) VALUES('X','x')`)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" {
		t.Fatalf("DB permits parent without contacts: %v", err)
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO student_parents(student_id,parent_id,relationship) VALUES($1,$2,'Отец')`, s.ID, p.ID)
	if !errors.As(err, &pg) || pg.Code != "23505" {
		t.Fatalf("DB permits duplicate link: %v", err)
	}
	_, err = f.pool.Exec(ctx, `UPDATE student_parents SET is_primary=true WHERE student_id=$1 AND parent_id=$2`, s.ID, p.ID)
	if !errors.As(err, &pg) || pg.ConstraintName != "student_parents_one_primary" {
		t.Fatalf("DB permits two primary contacts: %v", err)
	}
}

func TestFamiliesRoleAndRelatedObjectPermissions(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ownCurator, ownBrowser := provision(t, f, admin, "families-own@example.test", "curator")
	otherCurator, otherBrowser := provision(t, f, admin, "families-other@example.test", "curator")
	_, leader := provision(t, f, admin, "families-leader@example.test", "leader")
	_, teacher := provision(t, f, admin, "families-teacher@example.test", "teacher")
	own := studentCreate(t, admin, map[string]any{"name": "Свой ученик", "curator_id": ownCurator.ID})
	other := studentCreate(t, admin, map[string]any{"name": "Чужой ученик", "curator_id": otherCurator.ID})
	shared := parentCreate(t, admin, map[string]any{"name": "Общий родитель", "email": "shared@example.test"})
	private := parentCreate(t, admin, map[string]any{"name": "Чужой родитель", "phone": "+79001112233"})
	unlinked := parentCreate(t, admin, map[string]any{"name": "Без связи", "email": "none@example.test"})
	linkParent(t, admin, own.ID, shared.ID, true)
	linkParent(t, admin, other.ID, shared.ID, true)
	linkParent(t, admin, other.ID, private.ID, false)
	var students people.Page[people.Student]
	decode(t, expect(t, ownBrowser, "GET", "/students?curator_id="+fmt.Sprint(otherCurator.ID), nil, 200), &students)
	if students.Total != 1 || len(students.Items) != 1 || students.Items[0].ID != own.ID {
		t.Fatalf("curator list scope: %+v", students)
	}
	studentCard(t, ownBrowser, own.ID)
	expect(t, ownBrowser, "GET", fmt.Sprintf("/students/%d", other.ID), nil, 404)
	var parents people.Page[people.Parent]
	decode(t, expect(t, ownBrowser, "GET", "/parents", nil, 200), &parents)
	if parents.Total != 1 || parents.Items[0].ID != shared.ID {
		t.Fatalf("parent scope: %+v", parents)
	}
	body := expect(t, ownBrowser, "GET", fmt.Sprintf("/parents/%d", shared.ID), nil, 200)
	var card people.ParentCard
	decode(t, body, &card)
	if len(card.Children) != 1 || card.Children[0].ID != own.ID || bytes.Contains(body, []byte(other.Name)) {
		t.Fatalf("shared parent exposes sibling: %s", body)
	}
	for _, p := range []people.Parent{private, unlinked} {
		expect(t, ownBrowser, "GET", fmt.Sprintf("/parents/%d", p.ID), nil, 404)
	}
	decode(t, expect(t, leader, "GET", "/students", nil, 200), &students)
	if students.Total != 2 {
		t.Fatal("leader cannot read students")
	}
	decode(t, expect(t, leader, "GET", fmt.Sprintf("/parents/%d", shared.ID), nil, 200), &card)
	if len(card.Children) != 2 {
		t.Fatal("leader cannot read parent children")
	}
	decode(t, expect(t, teacher, "GET", "/students", nil, 200), &students)
	if students.Total != 0 || len(students.Items) != 0 {
		t.Fatal("teacher sees full student records without lessons")
	}
	expect(t, teacher, "GET", fmt.Sprintf("/students/%d", own.ID), nil, 404)
	expect(t, teacher, "GET", "/parents", nil, 403)
	for _, b := range []*browser{ownBrowser, otherBrowser, leader, teacher} {
		expect(t, b, "POST", "/students", map[string]any{"name": "Forbidden"}, 403)
		expect(t, b, "POST", "/parents", map[string]any{"name": "Forbidden", "email": "forbidden@example.test"}, 403)
		expect(t, b, "PATCH", fmt.Sprintf("/students/%d", own.ID), map[string]any{"curator_id": otherCurator.ID}, 403)
		expect(t, b, "PATCH", fmt.Sprintf("/parents/%d", shared.ID), map[string]any{"phone": "+79001112233"}, 403)
		expect(t, b, "POST", fmt.Sprintf("/students/%d/parents", own.ID), map[string]any{"parent_id": private.ID, "relationship": "Родитель"}, 403)
		expect(t, b, "PATCH", linkPath(own.ID, shared.ID), map[string]any{"is_primary": false}, 403)
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", own.ID), map[string]any{"curator_id": otherCurator.ID}, 200)
	expect(t, ownBrowser, "GET", fmt.Sprintf("/students/%d", own.ID), nil, 404)
	expect(t, ownBrowser, "GET", fmt.Sprintf("/parents/%d", shared.ID), nil, 404)
	decode(t, expect(t, otherBrowser, "GET", "/students", nil, 200), &students)
	if students.Total != 2 {
		t.Fatal("curator reassignment not reflected")
	}
	// Role union must widen only according to fresh server-side roles.
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) VALUES($1,'leader')`, ownCurator.ID); err != nil {
		t.Fatal(err)
	}
	decode(t, expect(t, ownBrowser, "GET", "/parents", nil, 200), &parents)
	if parents.Total != 3 {
		t.Fatal("role union ignored")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", ownCurator.ID), map[string]any{"blocked": true}, 200)
	expect(t, ownBrowser, "GET", "/students", nil, 401)
	expect(t, admin, "POST", "/students", map[string]any{"name": "Blocked curator", "curator_id": ownCurator.ID}, 400)
	anonymous := f.browser(t)
	expect(t, anonymous, "GET", "/students", nil, 401)
	expect(t, anonymous, "GET", fmt.Sprintf("/parents/%d", shared.ID), nil, 401)
	admin.csrf = ""
	expect(t, admin, "POST", "/parents", map[string]any{"name": "CSRF", "email": "csrf@example.test"}, 403)
	expect(t, admin, "PATCH", linkPath(own.ID, shared.ID), map[string]any{"is_primary": false}, 403)
}

func TestFamilyLinksConcurrentPrimaryAndDuplicate(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	s := studentCreate(t, admin, map[string]any{"name": "Concurrent"})
	p := parentCreate(t, admin, map[string]any{"name": "First", "email": "first@example.test"})
	q := parentCreate(t, admin, map[string]any{"name": "Second", "email": "second@example.test"})
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))}
	ctx, cancel := context.WithTimeout(httpx.WithActor(context.Background(), actor), 10*time.Second)
	defer cancel()
	repo := students.Repository{Pool: f.pool}
	run := func(ids []int64, primary bool) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan error, len(ids))
		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				<-start
				_, err := repo.SaveLink(ctx, s.ID, people.LinkInput{ParentID: id, Relationship: "Родитель", IsPrimary: primary}, nil)
				results <- err
			}(id)
		}
		close(start)
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else {
				var pg *pgconn.PgError
				if errors.As(err, &pg) && pg.Code == "23505" {
					conflict++
				} else {
					t.Fatal(err)
				}
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("concurrent writes: success %d conflict %d", success, conflict)
		}
	}
	run([]int64{p.ID, q.ID}, true)
	c := studentCard(t, admin, s.ID)
	if len(c.Parents) != 1 || !c.Parents[0].IsPrimary {
		t.Fatal("primary invariant broken")
	}
	missing := p.ID
	if c.Parents[0].ParentID == p.ID {
		missing = q.ID
	}
	run([]int64{missing, missing}, false)
	if len(studentCard(t, admin, s.ID).Parents) != 2 {
		t.Fatal("duplicate invariant broken")
	}
	expect(t, admin, "POST", "/auth/logout", nil, 204)
	_, err := repo.SaveStudent(ctx, 0, people.StudentInput{Name: "Revoked"}, nil)
	var appErr *httpx.Error
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("write reused revoked session: %v", err)
	}
	_, err = repo.Student(ctx, s.ID)
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("read reused revoked session: %v", err)
	}
	_, err = (&parents.Repository{Pool: f.pool}).Parent(ctx, p.ID)
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("parent read reused revoked session: %v", err)
	}
	_, err = repo.SaveLink(ctx, s.ID, people.LinkInput{ParentID: p.ID, Relationship: "Родитель"}, nil)
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("link reused revoked session: %v", err)
	}
}
