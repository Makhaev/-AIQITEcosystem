package app_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"aiq.local/core/internal/platform/uploads"
)

func TestCuratorReportFilesPrivacyPersistenceAndPublication(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	user, curator := provision(t, f, admin, "curator-files@example.test", "curator")
	foreignUser, foreign := provision(t, f, admin, "foreign-files@example.test", "curator")
	student := studentCreate(t, admin, map[string]any{"name": "Ученик с вложениями"})
	other := studentCreate(t, admin, map[string]any{"name": "Чужой ученик"})
	curatedTestGroup(t, admin, student.ID, user.ID)
	curatedTestGroup(t, admin, other.ID, foreignUser.ID)
	parentCard := parentCreate(t, admin, map[string]any{"name": "Получатель", "email": "report-files-parent@example.test"})
	otherParent := parentCreate(t, admin, map[string]any{"name": "Не получатель", "email": "report-files-other-parent@example.test"})
	for _, id := range []int64{parentCard.ID, otherParent.ID} {
		expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", student.ID), map[string]any{"parent_id": id, "relationship": "Родитель", "is_primary": false}, 201)
	}
	_, parent := invite(t, f, admin, "parent", parentCard.ID, "report-files-parent@example.test")
	_, notRecipient := invite(t, f, admin, "parent", otherParent.ID, "report-files-other-parent@example.test")
	reportID := resultID(t, expect(t, curator, "POST", "/curator-reports", map[string]any{"student_id": student.ID, "summary": "Отчёт с материалами."}, 201))
	otherID := resultID(t, expect(t, foreign, "POST", "/curator-reports", map[string]any{"student_id": other.ID, "summary": "Другой отчёт."}, 201))
	path := fmt.Sprintf("/curator-reports/%d", reportID)
	base := path + "/files"
	content := []byte("Работа ученика\x00\xff")
	var file uploads.Info
	decode(t, uploadModuleFile(t, curator, base, "Работа ученика.pdf", content, true, 201), &file)
	url := fmt.Sprintf("%s/%d", base, file.ID)
	if !bytes.Equal(expect(t, curator, "GET", url, nil, 200), content) {
		t.Fatal("download changed original bytes")
	}
	var report struct {
		Files []uploads.Info `json:"files"`
	}
	decode(t, expect(t, curator, "GET", path, nil, 200), &report)
	if len(report.Files) != 1 || report.Files[0].Name != file.Name || report.Files[0].Size != len(content) {
		t.Fatalf("persisted metadata: %+v", report.Files)
	}
	expect(t, parent, "GET", url, nil, 404)
	expect(t, notRecipient, "GET", url, nil, 404)
	expect(t, foreign, "GET", url, nil, 404)
	uploadModuleFile(t, foreign, base, "denied.txt", []byte("x"), true, 404)
	uploadModuleFile(t, parent, base, "denied.txt", []byte("x"), true, 403)
	uploadModuleFile(t, curator, base, "empty.txt", nil, true, 400)
	uploadModuleFile(t, curator, base, "large.bin", bytes.Repeat([]byte("x"), uploads.MaxFileSize+1), true, 413)
	uploadModuleFile(t, curator, base, "csrf.txt", []byte("x"), false, 403)
	expect(t, admin, "DELETE", fmt.Sprintf("/curator-reports/%d/files/%d", otherID, file.ID), nil, 404)
	var removable uploads.Info
	decode(t, uploadModuleFile(t, curator, base, "Черновик.txt", []byte("delete me"), true, 201), &removable)
	expect(t, curator, "DELETE", fmt.Sprintf("%s/%d", base, removable.ID), nil, 204)
	expect(t, curator, "GET", fmt.Sprintf("%s/%d", base, removable.ID), nil, 404)
	expect(t, curator, "POST", path+"/publish", map[string]any{"parent_ids": []int64{parentCard.ID}}, 200)
	if !bytes.Equal(expect(t, parent, "GET", url, nil, 200), content) {
		t.Fatal("recipient download changed bytes")
	}
	decode(t, expect(t, parent, "GET", path, nil, 200), &report)
	if len(report.Files) != 1 || report.Files[0].ID != file.ID {
		t.Fatal("published report lost attachment")
	}
	expect(t, notRecipient, "GET", url, nil, 404)
	uploadModuleFile(t, curator, base, "after-publication.txt", []byte("x"), true, 409)
	expect(t, curator, "DELETE", url, nil, 409)
	expect(t, admin, "DELETE", url, nil, 409)
	_, err := f.pool.Exec(ctx, `INSERT INTO curator_report_files(report_id,name,content) VALUES($1,'late.txt',decode('78','hex'))`, reportID)
	dbConstraint(t, err, "23514", "curator_report_file_published")
	_, err = f.pool.Exec(ctx, `UPDATE curator_report_files SET content=decode('78','hex') WHERE id=$1`, file.ID)
	dbConstraint(t, err, "23514", "curator_report_file_published")
	_, err = f.pool.Exec(ctx, `DELETE FROM curator_report_files WHERE id=$1`, file.ID)
	dbConstraint(t, err, "23514", "curator_report_file_published")
	if _, err = f.pool.Exec(ctx, `DELETE FROM student_parents WHERE student_id=$1 AND parent_id=$2`, student.ID, parentCard.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, parent, "GET", url, nil, 404)
	capID := resultID(t, expect(t, curator, "POST", "/curator-reports", map[string]any{"student_id": student.ID, "summary": "Лимит файлов."}, 201))
	if _, err = f.pool.Exec(ctx, `INSERT INTO curator_report_files(report_id,name,content) SELECT $1,'fixture.txt',decode('78','hex') FROM generate_series(1,20)`, capID); err != nil {
		t.Fatal(err)
	}
	uploadModuleFile(t, curator, fmt.Sprintf("/curator-reports/%d/files", capID), "extra.txt", []byte("x"), true, 409)
}
