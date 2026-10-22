package tasks

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
)

func transition(v Task, actor httpx.Actor, status string) error {
	if !validStatus(status) {
		return httpx.Invalid("status", "Недопустимый статус задачи.")
	}
	if v.Status == status {
		return nil
	}
	manage := access.ScopeFor(actor, access.Tasks, access.Manage) == access.All
	worker := manage || v.AssigneeID == actor.ID
	owner := manage || v.CreatorID == actor.ID
	valid, allowed := false, false
	switch v.Status {
	case "todo":
		valid = status == "in_progress" || status == "cancelled"
		allowed = status == "in_progress" && worker || status == "cancelled" && owner
	case "in_progress":
		valid = status == "done" || status == "cancelled"
		allowed = status == "done" && worker || status == "cancelled" && owner
	case "done":
		valid = status == "in_progress" || status == "cancelled"
		allowed = owner
	}
	if !valid {
		return httpx.E(409, "invalid_task_transition", "Недопустимый переход статуса задачи.")
	}
	if !allowed {
		return httpx.Forbidden()
	}
	return nil
}
