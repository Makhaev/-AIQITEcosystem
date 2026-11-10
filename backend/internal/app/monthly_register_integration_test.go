package app_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestMonthlyStudentRegisterLifecycleHistoryAndExcel(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	_, leader := provision(t, f, admin, "register-leader@example.test", "leader")
	_, curator := provision(t, f, admin, "register-curator@example.test", "curator")
	_, methodist := provision(t, f, admin, "register-methodist@example.test", "methodist")
	now := time.Now().In(time.FixedZone("MSK", 10800))
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	earlier := month.AddDate(0, -2, 0).AddDate(0, 0, 5)
	previous := month.AddDate(0, -1, 0).Format("2006-01")
	current := month.Format("2006-01")
	first := studentCreate(t, admin, map[string]any{"name": "Иванов Иван"})
	second := studentCreate(t, admin, map[string]any{"name": "=SUM(1,2)"})
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE students SET created_at=$2 WHERE id=$1`, first.ID, earlier); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE student_status_events SET changed_at=$2 WHERE student_id=$1`, first.ID, earlier); err != nil {
		t.Fatal(err)
	}
	path := "/students/monthly-register?month=" + current
	_, total := personalPage(t, admin, path)
	if total != 2 {
		t.Fatalf("new students not added: %d", total)
	}
	_, total = personalPage(t, leader, "/students/monthly-register?month="+previous)
	if total != 1 {
		t.Fatal("previous month must exclude newly added student")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", first.ID), map[string]any{"status": "archived"}, 200)
	rows, total := personalPage(t, admin, path)
	if total != 1 || rows[0]["id"] != float64(second.ID) {
		t.Fatal("withdrawn student retained in current roster")
	}
	_, total = personalPage(t, admin, "/students/monthly-register?month="+previous)
	if total != 1 {
		t.Fatal("withdrawal changed past month")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", first.ID), map[string]any{"status": "active"}, 200)
	_, total = personalPage(t, admin, path)
	if total != 2 {
		t.Fatal("restored student missing or duplicated")
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO students(name,name_search) SELECT 'Register pupil '||n,'register pupil '||n FROM generate_series(1,22) n`); err != nil {
		t.Fatal(err)
	}
	studentCreate(t, admin, map[string]any{"name": "Archived at creation", "status": "archived"})
	rows, total = personalPage(t, admin, path+"&page=2")
	if total != 24 || len(rows) != 4 {
		t.Fatalf("unexpected pagination: %d %d", total, len(rows))
	}
	data := expect(t, leader, "GET", path+"&page=2&format=xlsx", nil, 200)
	workbook, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal("Excel export is not an XLSX ZIP", err)
	}
	sheetFound := false
	for _, file := range workbook.File {
		if !strings.HasSuffix(file.Name, ".xml") && !strings.HasSuffix(file.Name, ".rels") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		decoder := xml.NewDecoder(bytes.NewReader(contents))
		for {
			_, err = decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid Excel XML %s: %v", file.Name, err)
			}
		}
		if file.Name == "xl/worksheets/sheet1.xml" {
			sheetFound = true
			if bytes.Count(contents, []byte("<row ")) != 25 || !bytes.Contains(contents, []byte("Иванов Иван")) || !bytes.Contains(contents, []byte("=SUM(1,2)")) || bytes.Contains(contents, []byte("<f>")) {
				t.Fatal("Excel must include every student as text, independent of page")
			}
		}
	}
	if !sheetFound {
		t.Fatal("missing Excel worksheet")
	}
	for _, viewer := range []*browser{curator, methodist} {
		expect(t, viewer, "GET", path, nil, 403)
		expect(t, viewer, "GET", path+"&format=xlsx", nil, 403)
	}
	expect(t, f.browser(t), "GET", path, nil, 401)
	for _, invalid := range []string{"month=bad", "month=2026-13", "month=" + month.AddDate(0, 1, 0).Format("2006-01"), "page=0", "format=csv"} {
		expect(t, admin, "GET", "/students/monthly-register?"+invalid, nil, 400)
	}
}
