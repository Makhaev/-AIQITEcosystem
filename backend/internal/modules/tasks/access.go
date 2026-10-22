package tasks

import (
	"context"
	"errors"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

func assignmentDenied() error {
	err := httpx.E(400, "assignee_object_access_denied", "Ответственный не имеет доступа к выбранной записи.")
	err.Fields["assignee_id"] = "Выберите сотрудника с доступом к связанному объекту."
	return err
}

func validateAssignment(ctx context.Context, tx pgx.Tx, actor httpx.Actor, in Input) error {
	assignee := httpx.Actor{ID: in.AssigneeID}
	var blocked bool
	err := tx.QueryRow(ctx, `SELECT blocked,ARRAY(SELECT role FROM user_roles WHERE user_id=u.id ORDER BY role)
 FROM users u WHERE id=$1`, in.AssigneeID).Scan(&blocked, &assignee.Roles)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (blocked || access.ScopeFor(assignee, access.Tasks, access.Read) == access.None) {
		if in.ObjectType != nil {
			return assignmentDenied()
		}
		return httpx.Invalid("assignee_id", "Выберите активного сотрудника.")
	}
	if err != nil || in.ObjectType == nil {
		return err
	}
	allowed, err := access.ObjectReadable(ctx, tx, assignee, *in.ObjectType, *in.ObjectID)
	if err != nil {
		return err
	}
	if !allowed {
		return assignmentDenied()
	}
	// Task management was authorized separately in begin. Check object access
	// independently as well; an assignee's permission never authorizes the author.
	// The check above also rejects missing objects without disclosing their data.
	allowed, err = access.ObjectReadable(ctx, tx, actor, *in.ObjectType, *in.ObjectID)
	if err != nil {
		return err
	}
	if !allowed {
		return httpx.Forbidden()
	}
	return nil
}

// Existing tasks remain readable after relationship changes, but they never
// grant object access. Evaluate within the same read snapshot as the task.
func projectTask(ctx context.Context, tx pgx.Tx, actor httpx.Actor, v *Task) error {
	if v.ObjectType == nil || v.ObjectID == nil {
		return nil
	}
	allowed, err := access.ObjectReadable(ctx, tx, actor, *v.ObjectType, *v.ObjectID)
	if err != nil {
		return err
	}
	if !allowed {
		v.ObjectType, v.ObjectID, v.ObjectName = nil, nil, nil
	}
	return nil
}
