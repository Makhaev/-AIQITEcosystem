package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestStudent360EducationEmptyAndIndependentPeriod(t *testing.T) {
	a := setupAttendance(t)
	a.lesson(t, time.Hour, 2*time.Hour)
	for _, actor := range []*browser{a.admin, a.own} {
		for _, route := range []string{"overview", "360"} {
			path := fmt.Sprintf("/students/%d/%s", a.firstID, route)
			var result map[string]any
			decode(t, expect(t, actor, "GET", path+"?from=2026-10-01&to=2026-10-31&education_from=2024-02-29&education_to=2024-02-29", nil, 200), &result)
			education := result["education"].(map[string]any)
			if education["from"] != "2024-02-29" || education["to"] != "2024-02-29" || result["attendance"].(map[string]any)["from"] != "2026-10-01" {
				t.Fatal(result)
			}
			for _, key := range []string{"goals", "observations", "skills", "assignments", "attempts", "projects", "achievements"} {
				block := education[key].(map[string]any)
				if block["state"] != "ready" || block["total"] != float64(0) || len(block["items"].([]any)) != 0 {
					t.Fatalf("unexpected block: %v", block)
				}
			}
			if actor == a.own {
				for _, key := range []string{"age", "birth_date", "parents", "curator_id"} {
					if _, exists := result[key]; exists {
						t.Fatalf("teacher received %s", key)
					}
				}
			}
			expect(t, actor, "GET", path+"?education_from=2026-10-01", nil, 400)
		}
	}
	// A failed source must remain an error, without fabricated empty results.
	_, err := a.f.pool.Exec(context.Background(), `WITH r AS (INSERT INTO learning_records(student_id,kind,scope,created_by,state) VALUES($1,'projects','explicit',$2,'active') RETURNING id) INSERT INTO learning_revisions(record_id,revision,content,author_id) SELECT id,1,'{"title":42,"starts_on":"2000-01-01"}'::jsonb,$2 FROM r`, a.firstID, a.f.admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	var overview map[string]any
	decode(t, expect(t, a.admin, "GET", fmt.Sprintf("/students/%d/overview", a.firstID), nil, 200), &overview)
	education := overview["education"].(map[string]any)
	failed := education["projects"].(map[string]any)
	if failed["state"] != "error" || len(failed) != 1 || education["goals"].(map[string]any)["state"] != "ready" {
		t.Fatal("source failure fabricated results or hid independent blocks", education)
	}
}
