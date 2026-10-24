package learning

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"strings"
	"time"
)

func (repo *Repository) Action(ctx context.Context, id int64, in ActionInput) (Card, error) {
	tx, a, err := access.Begin(ctx, repo.Pool, access.Students, access.Read, true)
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback(ctx)
	r, err := load(ctx, tx, a, id)
	if err != nil {
		return Card{}, err
	}
	if in.Revision != r.Revision {
		return Card{}, httpx.E(409, "revision_conflict", "Запись изменилась. Обновите карточку.")
	}
	allowed, err := actions(ctx, tx, a, r)
	if err != nil {
		return Card{}, err
	}
	if !contains(allowed, in.Action) || in.Action == "correct" {
		return Card{}, httpx.Forbidden()
	}
	if contains([]string{"return", "withdraw", "cancel", "unpublish", "achieve"}, in.Action) && strings.TrimSpace(in.Reason) == "" {
		return Card{}, httpx.Invalid("reason", "Укажите причину решения.")
	}
	if in.Action == "activate" && r.Content.DueOn < time.Now().In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02") {
		return Card{}, httpx.Invalid("due_on", "Срок цели истёк. Исправьте срок перед активацией.")
	}
	state, published := r.State, r.Published
	switch in.Action {
	case "activate":
		state = "active"
	case "achieve":
		state = "achieved"
	case "cancel":
		state = "cancelled"
	case "withdraw":
		state = "withdrawn"
		published = false
	case "submit":
		state = "submitted"
	case "return":
		state = "revision"
		if r.Kind == "observations" {
			state = "draft"
		}
		if r.Kind == "projects" {
			state = "active"
		}
	case "approve":
		switch r.Kind {
		case "observations":
			state = "reviewed"
		case "attempts":
			state = "accepted"
		case "projects":
			state = "completed"
		case "achievements":
			state = "confirmed"
		}
	case "publish":
		published = true
	case "unpublish":
		published = false
	}
	next := r.Revision + 1
	if _, err = tx.Exec(ctx, `INSERT INTO learning_revisions(record_id,revision,content,author_id) SELECT record_id,$3,content,$4 FROM learning_revisions WHERE record_id=$1 AND revision=$2`, id, r.Revision, next, a.ID); err != nil {
		return Card{}, err
	}
	if in.Action == "achieve" {
		if len(in.Sources) == 0 || len(in.Sources) > 20 {
			return Card{}, httpx.Invalid("sources", "Достижение цели требует от 1 до 20 подтверждённых оснований.")
		}
		if err = saveEvidence(ctx, tx, a, r.StudentID, id, next, in.Sources, nil); err != nil {
			return Card{}, err
		}
	} else {
		if in.Action == "approve" && r.Kind == "achievements" {
			view, e := projected(ctx, tx, a, r, time.Time{}, time.Time{})
			if e != nil {
				return Card{}, e
			}
			if view.BasisUnavailable || view.NeedsReview || len(view.Sources) == 0 {
				return Card{}, httpx.Invalid("sources", "Основания недоступны или изменились. Требуется повторная проверка.")
			}
		}
		if _, err = tx.Exec(ctx, `INSERT INTO learning_sources SELECT record_id,$3,source_id,source_revision FROM learning_sources WHERE record_id=$1 AND revision=$2`, id, r.Revision, next); err != nil {
			return Card{}, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO learning_files SELECT record_id,$3,file_id FROM learning_files WHERE record_id=$1 AND revision=$2`, id, r.Revision, next); err != nil {
		return Card{}, err
	}
	closed := contains([]string{"achieved", "cancelled", "withdrawn", "accepted", "completed", "confirmed"}, state)
	if _, err = tx.Exec(ctx, `UPDATE learning_records SET current_revision=$2,state=$3,published=$4,closed_at=CASE WHEN $5 THEN COALESCE(closed_at,clock_timestamp()) ELSE NULL END WHERE id=$1`, id, next, state, published, closed); err != nil {
		return Card{}, err
	}
	if err = event(ctx, tx, a, id, next, in.Action, in.Reason); err != nil {
		return Card{}, err
	}
	if r.Kind == "attempts" && r.ParentID != nil {
		parent, e := load(ctx, tx, a, *r.ParentID)
		if e != nil {
			return Card{}, e
		}
		pnext := parent.Revision + 1
		var accepted bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM learning_records WHERE parent_id=$1 AND kind='attempts' AND state='accepted')`, parent.ID).Scan(&accepted); e != nil {
			return Card{}, e
		}
		pstate := "assigned"
		if accepted {
			pstate = "completed"
		}
		if _, e = tx.Exec(ctx, `INSERT INTO learning_revisions(record_id,revision,content,author_id) SELECT record_id,$3,content,$4 FROM learning_revisions WHERE record_id=$1 AND revision=$2`, parent.ID, parent.Revision, pnext, a.ID); e != nil {
			return Card{}, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO learning_sources SELECT record_id,$3,source_id,source_revision FROM learning_sources WHERE record_id=$1 AND revision=$2`, parent.ID, parent.Revision, pnext); e != nil {
			return Card{}, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO learning_files SELECT record_id,$3,file_id FROM learning_files WHERE record_id=$1 AND revision=$2`, parent.ID, parent.Revision, pnext); e != nil {
			return Card{}, e
		}
		if _, e = tx.Exec(ctx, `UPDATE learning_records SET current_revision=$2,state=$3,closed_at=CASE WHEN $4 THEN COALESCE(closed_at,clock_timestamp()) ELSE NULL END WHERE id=$1`, parent.ID, pnext, pstate, accepted); e != nil {
			return Card{}, e
		}
		if e = event(ctx, tx, a, parent.ID, pnext, "attempt_review", ""); e != nil {
			return Card{}, e
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Card{}, err
	}
	return repo.Get(ctx, id, 0)
}
