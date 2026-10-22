package lessons

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"testing"
)

func TestLessonStateTransitions(t *testing.T) {
	for _, from := range []string{"scheduled", "completed", "cancelled"} {
		for _, to := range []string{"scheduled", "completed", "cancelled"} {
			t.Run(from+"_to_"+to, func(t *testing.T) {
				err := transition(from, to)
				denied := from == "cancelled" && to == "completed"
				if (err != nil) != denied {
					t.Fatalf("transition error=%v denied=%v", err, denied)
				}
			})
		}
	}
	for _, pair := range [][2]string{{"scheduled", "unknown"}, {"unknown", "scheduled"}, {"scheduled", ""}} {
		if transition(pair[0], pair[1]) == nil {
			t.Fatalf("accepted invalid transition %v", pair)
		}
	}
	for _, to := range []string{"scheduled", "completed", "cancelled"} {
		if err := transition("", to); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompletionIdempotencyAndCancelledGuardBeforeJournal(t *testing.T) {
	// A nil transaction proves these paths cannot initialize/change the journal.
	for _, status := range []string{"completed", "cancelled"} {
		before := Lesson{ID: 17, Status: status}
		after, err := complete(context.Background(), nil, httpx.Actor{}, before)
		if after.ID != before.ID || after.Status != before.Status {
			t.Fatal("early guard changed lesson")
		}
		if (err != nil) != (status == "cancelled") {
			t.Fatal("unexpected completion result", err)
		}
	}
}
