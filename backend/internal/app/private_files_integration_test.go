package app_test

import (
	"aiq.local/core/internal/modules/files"
	"context"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestPrivateFilesObjectGrantsRevocationAndPrivacy(t *testing.T) {
	a := setupAttendance(t)
	a.lesson(t, time.Hour, 2*time.Hour)
	var teacherID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	var f files.File
	decode(t, expect(t, a.admin, "POST", "/files", files.Input{StudentID: a.firstID, Name: "Материал.txt", Data: []byte("Private learning material")}, 201), &f)
	if f.Size != 25 || f.SHA256 == "" || !f.CanManage {
		t.Fatalf("bad metadata: %+v", f)
	}
	path := fmt.Sprintf("/files/%d", f.ID)
	expect(t, a.own, "GET", path+"/content", nil, 404)
	var list files.Page
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/students/%d/files", a.firstID), nil, 200), &list)
	if list.Total != 0 || len(list.Items) != 0 {
		t.Fatal("private metadata/count leaked")
	}
	expect(t, a.admin, "PUT", path+"/access", files.Grant{UserID: teacherID, Allow: true}, 204)
	response, err := a.own.client.Get(a.f.server.URL + "/api/v1" + path + "/content")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "Private learning material" || response.Header.Get("Cache-Control") != "private, no-store" || response.Header.Get("Content-Disposition") == "" {
		t.Fatal("invalid download")
	}
	expect(t, a.own, "PUT", path+"/access", files.Grant{UserID: a.foreignUserID, Allow: true}, 403)
	expect(t, a.admin, "PUT", path+"/access", files.Grant{UserID: a.foreignUserID, Allow: true}, 400)
	expect(t, a.admin, "PUT", path+"/access", files.Grant{UserID: teacherID, Allow: false}, 204)
	expect(t, a.own, "GET", path+"/content", nil, 404)
	expect(t, a.admin, "PUT", path+"/access", files.Grant{UserID: teacherID, Allow: true}, 204)
	// Authoritative student access, not only the file grant, is checked on every read.
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", teacherID), map[string]any{"roles": []string{"curator"}}, 200)
	expect(t, a.own, "GET", path+"/content", nil, 401)
	a.own.login(t, "attendance-own@example.test", "TestOnly-New-Account-9372")
	expect(t, a.own, "GET", path+"/content", nil, 404)
	expect(t, a.admin, "POST", path+"/revoke", nil, 204)
	expect(t, a.admin, "GET", path+"/content", nil, 404)
	if _, err := a.f.pool.Exec(context.Background(), `UPDATE private_files SET data='changed' WHERE id=$1`, f.ID); err == nil {
		t.Fatal("immutable file changed")
	}
}

func TestPrivateFilesAuditFailureRollsBackAndPagination(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	for i := 0; i < 21; i++ {
		expect(t, a.admin, "POST", "/files", files.Input{StudentID: a.firstID, Name: fmt.Sprintf("%d.txt", i), Data: []byte("content")}, 201)
	}
	var p files.Page
	decode(t, expect(t, a.admin, "GET", fmt.Sprintf("/students/%d/files?page=2", a.firstID), nil, 200), &p)
	if p.Total != 21 || len(p.Items) != 1 {
		t.Fatalf("bad page: %+v", p)
	}
	_, err := a.f.pool.Exec(ctx, `CREATE FUNCTION fail_private_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action LIKE 'file.%' THEN RAISE EXCEPTION 'test'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_private_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_private_audit()`)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "POST", "/files", files.Input{StudentID: a.firstID, Name: "rollback.txt", Data: []byte("not committed")}, 500)
	expect(t, a.admin, "POST", fmt.Sprintf("/files/%d/revoke", p.Items[0].ID), nil, 500)
	expect(t, a.admin, "GET", fmt.Sprintf("/files/%d/content", p.Items[0].ID), nil, 200)
	var count int
	if err = a.f.pool.QueryRow(ctx, `SELECT count(*) FROM private_files`).Scan(&count); err != nil || count != 21 {
		t.Fatalf("partial upload: %d %v", count, err)
	}
}
