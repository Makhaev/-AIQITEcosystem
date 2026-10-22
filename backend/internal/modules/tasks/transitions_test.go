package tasks

import (
	"errors"
	"testing"

	"aiq.local/core/internal/platform/httpx"
)

func TestTaskTransitionMatrix(t *testing.T) {
	edges := map[string]string{
		"todo/in_progress": "worker", "in_progress/done": "worker",
		"todo/cancelled": "owner", "in_progress/cancelled": "owner",
		"done/in_progress": "owner", "done/cancelled": "owner",
	}
	for _, actor := range []struct {
		name          string
		actor         httpx.Actor
		worker, owner bool
	}{
		{"admin", httpx.Actor{ID: 1, Roles: []string{"admin"}}, true, true},
		{"leader", httpx.Actor{ID: 1, Roles: []string{"leader"}}, true, true},
		{"assignee", httpx.Actor{ID: 2, Roles: []string{"teacher"}}, true, false},
		{"creator", httpx.Actor{ID: 3, Roles: []string{"curator"}}, false, true},
		{"creator_and_assignee", httpx.Actor{ID: 2, Roles: []string{"teacher"}}, true, true},
		{"unrelated", httpx.Actor{ID: 4, Roles: []string{"curator"}}, false, false},
	} {
		for _, from := range []string{"todo", "in_progress", "done", "cancelled"} {
			for _, to := range []string{"todo", "in_progress", "done", "cancelled", "invalid"} {
				t.Run(actor.name+"/"+from+"/"+to, func(t *testing.T) {
					creator := int64(3)
					if actor.name == "creator_and_assignee" {
						creator = 2
					}
					want := 0
					if to == "invalid" {
						want = 400
					} else if from != to {
						switch edges[from+"/"+to] {
						case "worker":
							if !actor.worker {
								want = 403
							}
						case "owner":
							if !actor.owner {
								want = 403
							}
						default:
							want = 409
						}
					}
					err := transition(Task{Status: from, AssigneeID: 2, CreatorID: creator}, actor.actor, to)
					if want == 0 {
						if err != nil {
							t.Fatal(err)
						}
						return
					}
					var api *httpx.Error
					if !errors.As(err, &api) || api.Status != want {
						t.Fatalf("got %v, want HTTP %d", err, want)
					}
				})
			}
		}
	}
}
