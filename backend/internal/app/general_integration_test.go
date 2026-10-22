package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"

	"aiq.local/core/internal/platform/httpx"
)

func TestGeneralListContractAndStablePageBoundaries(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	var ids []int64
	for i := 0; i < 25; i++ {
		v := teacherCreate(t, admin, map[string]any{"name": "Вымышленный педагог"})
		ids = append(ids, v.ID)
	}
	paths := []string{"/users", "/teachers", "/students", "/parents", "/programs", "/groups", "/lessons", "/tasks", "/tasks/assignees", "/audit-logs"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			var page struct {
				Items []json.RawMessage `json:"items"`
				Total int               `json:"total"`
				Page  int               `json:"page"`
				Size  int               `json:"page_size"`
			}
			decode(t, expect(t, admin, "GET", path, nil, 200), &page)
			if page.Items == nil || page.Page != 1 || page.Size != 20 || page.Total < len(page.Items) || len(page.Items) > 20 {
				t.Fatalf("invalid default list contract: %+v", page)
			}
			decode(t, expect(t, admin, "GET", path+"?page=2&page_size=100", nil, 200), &page)
			if page.Page != 2 || page.Size != 100 || page.Items == nil {
				t.Fatalf("invalid explicit list contract: %+v", page)
			}
			for _, query := range []string{"?page=0", "?page=bad", "?page_size=0", "?page_size=101"} {
				var failure httpx.Error
				decode(t, expect(t, admin, "GET", path+query, nil, 400), &failure)
				if failure.Code == "" || failure.Message == "" || failure.Fields == nil || len(failure.RequestID) != 32 {
					t.Fatalf("invalid validation error: %+v", failure)
				}
			}
		})
	}
	for repeat := 0; repeat < 2; repeat++ {
		first := teacherPage(t, admin, "/teachers")
		second := teacherPage(t, admin, "/teachers?page=2")
		var got []int64
		for _, v := range first.Items {
			got = append(got, v.ID)
		}
		for _, v := range second.Items {
			got = append(got, v.ID)
		}
		if !reflect.DeepEqual(got, ids) || first.Total != 25 || second.Total != 25 {
			t.Fatalf("unstable boundaries for identical names: %v", got)
		}
	}
}

func TestGeneralHealthAndForeignKeys(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var cascadeCount, foreignKeyCount int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE confdeltype='c'),count(*) FROM pg_constraint WHERE contype='f' AND connamespace=current_schema()::regnamespace`).Scan(&cascadeCount, &foreignKeyCount); err != nil {
		t.Fatal(err)
	}
	if cascadeCount != 0 || foreignKeyCount < 20 {
		t.Fatalf("educational history constraints: cascades=%d foreign keys=%d", cascadeCount, foreignKeyCount)
	}
	check := func(path string, status int) []byte {
		t.Helper()
		res, err := http.Get(f.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status || res.Header.Get("X-Request-ID") == "" {
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
		return body
	}
	check("/health/live", 200)
	check("/health/ready", 200)
	f.pool.Close()
	check("/health/live", 200)
	var failure httpx.Error
	decode(t, check("/health/ready", 503), &failure)
	if failure.Code != "not_ready" || failure.Fields == nil || len(failure.RequestID) != 32 {
		t.Fatalf("invalid readiness failure: %+v", failure)
	}
}
