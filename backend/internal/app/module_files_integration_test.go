package app_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"aiq.local/core/internal/modules/programs"
)

func uploadModuleFile(t *testing.T, b *browser, path, name string, content []byte, csrf bool, want int) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", b.fixture.server.URL+"/api/v1"+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", origin)
	if csrf {
		req.Header.Set("X-CSRF-Token", b.csrf)
	}
	response, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("upload: want %d, got %d: %s", want, response.StatusCode, result)
	}
	return result
}

func TestModuleFilesLifecycleAndAccess(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Материалы программы", "status": "active"})
	m := moduleCreate(t, admin, p.ID, 1, "Учебный модуль")
	other := programCreate(t, admin, map[string]any{"name": "Другая программа"})
	base := fmt.Sprintf("/programs/%d/modules/%d/files", p.ID, m.ID)
	var empty struct {
		Items []programs.ModuleFile `json:"items"`
	}
	decode(t, expect(t, admin, "GET", base, nil, 200), &empty)
	if empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("initial files: %+v", empty)
	}
	content := []byte("Учебные материалы\x00\xff")
	var file programs.ModuleFile
	decode(t, uploadModuleFile(t, admin, base, "План занятия.pdf", content, true, 201), &file)
	if file.Name != "План занятия.pdf" || file.Size != len(content) {
		t.Fatalf("metadata: %+v", file)
	}
	decode(t, expect(t, admin, "GET", base, nil, 200), &empty)
	if len(empty.Items) != 1 || empty.Items[0].ID != file.ID {
		t.Fatalf("persisted files: %+v", empty)
	}
	url := fmt.Sprintf("%s/%d", base, file.ID)
	response, err := admin.client.Get(f.server.URL + "/api/v1" + url)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Equal(content, downloaded) {
		t.Fatalf("download: status=%d err=%v", response.StatusCode, err)
	}
	media, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	if err != nil || media != "attachment" || params["filename"] != file.Name || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe download headers: %v", response.Header)
	}
	wrong := fmt.Sprintf("/programs/%d/modules/%d/files", other.ID, m.ID)
	expect(t, admin, "GET", wrong, nil, 404)
	expect(t, admin, "GET", fmt.Sprintf("%s/%d", wrong, file.ID), nil, 404)
	uploadModuleFile(t, admin, wrong, "wrong.txt", []byte("x"), true, 404)
	uploadModuleFile(t, admin, base, "empty.txt", nil, true, 400)
	uploadModuleFile(t, admin, base, "large.bin", bytes.Repeat([]byte("x"), programs.MaxModuleFileSize+1), true, 413)
	uploadModuleFile(t, admin, base, "csrf.txt", []byte("x"), false, 403)
	for _, role := range []string{"teacher", "parent"} {
		u := create(t, admin, role+"-files@example.test", role)
		if _, err := f.pool.Exec(context.Background(), `UPDATE users SET must_change_password=false WHERE id=$1`, u.ID); err != nil {
			t.Fatal(err)
		}
		b := f.browser(t)
		b.login(t, u.Email, userPassword)
		uploadModuleFile(t, b, base, "denied.txt", []byte("x"), true, 403)
		expect(t, b, "GET", url, nil, map[string]int{"teacher": 404, "parent": 403}[role])
		expect(t, b, "DELETE", url, nil, 403)
	}
	leaderUser := create(t, admin, "leader-files@example.test", "leader")
	if _, err := f.pool.Exec(context.Background(), `UPDATE users SET must_change_password=false WHERE id=$1`, leaderUser.ID); err != nil {
		t.Fatal(err)
	}
	leader := f.browser(t)
	leader.login(t, leaderUser.Email, userPassword)
	if result := expect(t, leader, "GET", url, nil, 200); !bytes.Equal(result, content) {
		t.Fatal("leader download changed bytes")
	}
	uploadModuleFile(t, leader, base, "denied.txt", []byte("x"), true, 403)
	methodistUser := create(t, admin, "methodist-files@example.test", "methodist")
	if _, err := f.pool.Exec(context.Background(), `UPDATE users SET must_change_password=false WHERE id=$1`, methodistUser.ID); err != nil {
		t.Fatal(err)
	}
	methodist := f.browser(t)
	methodist.login(t, methodistUser.Email, userPassword)
	uploadModuleFile(t, methodist, base, "Заметки.txt", []byte("Заметки"), true, 201)
	expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", p.ID), map[string]string{"status": "archived"}, 200)
	uploadModuleFile(t, admin, base, "archived.txt", []byte("x"), true, 409)
	expect(t, methodist, "DELETE", url, nil, 409)
	expect(t, methodist, "GET", url, nil, 200)
	expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", p.ID), map[string]string{"status": "active"}, 200)
	expect(t, methodist, "DELETE", url, nil, 204)
	expect(t, admin, "GET", url, nil, 404)
	decode(t, expect(t, admin, "GET", base, nil, 200), &empty)
	if len(empty.Items) != 1 || !strings.HasSuffix(empty.Items[0].Name, ".txt") {
		t.Fatalf("delete removed wrong file: %+v", empty.Items)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO module_files(module_id,name,content) SELECT $1,'fixture.txt',decode('78','hex') FROM generate_series(1,19)`, m.ID); err != nil {
		t.Fatal(err)
	}
	uploadModuleFile(t, admin, base, "over-limit.txt", []byte("x"), true, 409)
}
