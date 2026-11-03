package events

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Repository struct {
	Pool                   *pgxpool.Pool
	SelfRegistrationMinAge int
}

const staffSQL = `NOT u.blocked AND EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=u.id AND ur.role IN ('admin','leader','teacher','curator','methodologist','education_leader'))`
const columns = `e.id,e.title,e.type,e.description,e.organizer,e.starts_at,e.ends_at,e.time_zone,e.location,e.capacity,e.registration_starts_at,e.registration_ends_at,e.conditions,e.required_documents,e.status,e.revision,e.created_at,e.updated_at,e.published_at,e.registration_closed_at,e.completed_at,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',u.id,'name',u.name,'active',` + staffSQL + `) ORDER BY u.id) FROM free_event_responsibles er JOIN users u ON u.id=er.user_id WHERE er.event_id=e.id),'[]'::jsonb)`

func scan(row pgx.Row) (Event, error) {
	var e Event
	var raw []byte
	err := row.Scan(&e.ID, &e.Title, &e.Type, &e.Description, &e.Organizer, &e.StartsAt, &e.EndsAt, &e.TimeZone, &e.Location, &e.Capacity, &e.RegistrationStartsAt, &e.RegistrationEndsAt, &e.Conditions, &e.RequiredDocuments, &e.Status, &e.Revision, &e.CreatedAt, &e.UpdatedAt, &e.PublishedAt, &e.RegistrationClosedAt, &e.CompletedAt, &raw)
	if err != nil {
		return e, err
	}
	if err = json.Unmarshal(raw, &e.Responsibles); err != nil {
		return e, err
	}
	e.ResponsibleIDs = []int64{}
	for _, p := range e.Responsibles {
		e.ResponsibleIDs = append(e.ResponsibleIDs, p.ID)
	}
	return e, nil
}
func decorate(e *Event, a httpx.Actor) {
	now := time.Now()
	e.AllowedActions = []string{}
	e.RegistrationOpen = e.Status == "published" && e.RegistrationStartsAt != nil && e.RegistrationEndsAt != nil && !now.Before(*e.RegistrationStartsAt) && now.Before(*e.RegistrationEndsAt)
	admin := access.ScopeFor(a, access.Events, access.Manage) == access.All
	e.CanEdit = admin && e.Status != "completed" && e.Status != "cancelled"
	if !admin {
		return
	}
	switch e.Status {
	case "draft":
		e.AllowedActions = append(e.AllowedActions, "publish")
	case "published":
		e.AllowedActions = append(e.AllowedActions, "close_registration", "cancel")
	case "registration_closed":
		e.AllowedActions = append(e.AllowedActions, "cancel")
		if e.EndsAt != nil && !now.Before(*e.EndsAt) {
			e.AllowedActions = append(e.AllowedActions, "complete")
		}
	}
}
func load(ctx context.Context, tx pgx.Tx, id int64, lock bool) (Event, error) {
	q := `SELECT ` + columns + ` FROM free_events e WHERE e.id=$1`
	if lock {
		q += ` FOR UPDATE OF e`
	}
	return scan(tx.QueryRow(ctx, q, id))
}
func (r *Repository) List(ctx context.Context, f Filter) (Page[Event], error) {
	out := Page[Event]{Items: []Event{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	all := access.ScopeFor(a, access.Events, access.Read) == access.All
	where := ` WHERE ($1 OR e.status<>'draft') AND ($2='' OR strpos(e.title_search,$2)>0) AND ($3='' OR e.status=$3)`
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM free_events e`+where, all, f.Search, f.Status).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM free_events e`+where+` ORDER BY e.starts_at DESC NULLS FIRST,e.id DESC LIMIT $5 OFFSET $4`, all, f.Search, f.Status, (f.Page-1)*out.PageSize, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		decorate(&v, a)
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Get(ctx context.Context, id int64) (Event, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Read, false)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback(ctx)
	e, err := load(ctx, tx, id, false)
	if err != nil {
		return e, err
	}
	if e.Status == "draft" && access.ScopeFor(a, access.Events, access.Read) != access.All {
		return Event{}, pgx.ErrNoRows
	}
	decorate(&e, a)
	return e, tx.Commit(ctx)
}
func validateStaff(ctx context.Context, tx pgx.Tx, ids []int64) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users u WHERE u.id=ANY($1::bigint[]) AND `+staffSQL, ids).Scan(&count); err != nil {
		return err
	}
	if count != len(ids) {
		return httpx.Invalid("responsible_ids", "Выберите действующих сотрудников; заблокированные аккаунты и внешние роли не подходят.")
	}
	return nil
}
func (r *Repository) Save(ctx context.Context, id int64, in Input) (Event, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Manage, true)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback(ctx)
	var old Event
	action := "event.created"
	if id > 0 {
		old, err = load(ctx, tx, id, true)
		if err != nil {
			return Event{}, err
		}
		if old.Revision != in.Revision {
			return Event{}, conflict()
		}
		if old.Status == "completed" || old.Status == "cancelled" {
			return Event{}, httpx.E(409, "event_completed", "Завершённое событие не редактируется.")
		}
		if err = validate(&in, old.Status != "draft"); err != nil {
			return Event{}, err
		}
		action = "event.updated"
		n, countErr := occupied(ctx, tx, id)
		if countErr != nil {
			return Event{}, countErr
		}
		if in.Capacity < n {
			return Event{}, httpx.Invalid("capacity", "Вместимость не может быть меньше числа занятых мест.")
		}
	}
	if err = validateStaff(ctx, tx, in.ResponsibleIDs); err != nil {
		return Event{}, err
	}
	args := []any{in.Title, strings.ToLower(in.Title), in.Type, in.Description, in.Organizer, in.StartsAt, in.EndsAt, in.TimeZone, in.Location, in.Capacity, in.RegistrationStartsAt, in.RegistrationEndsAt, in.Conditions, in.RequiredDocuments}
	if id == 0 {
		args = append(args, a.ID)
		err = tx.QueryRow(ctx, `INSERT INTO free_events(title,title_search,type,description,organizer,starts_at,ends_at,time_zone,location,capacity,registration_starts_at,registration_ends_at,conditions,required_documents,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`, args...).Scan(&id)
	} else {
		args = append(args, id)
		_, err = tx.Exec(ctx, `UPDATE free_events SET title=$1,title_search=$2,type=$3,description=$4,organizer=$5,starts_at=$6,ends_at=$7,time_zone=$8,location=$9,capacity=$10,registration_starts_at=$11,registration_ends_at=$12,conditions=$13,required_documents=$14,revision=revision+1,updated_at=clock_timestamp() WHERE id=$15`, args...)
	}
	if err != nil {
		return Event{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM free_event_responsibles WHERE event_id=$1`, id); err != nil {
		return Event{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO free_event_responsibles(event_id,user_id) SELECT $1,unnest($2::bigint[])`, id, in.ResponsibleIDs); err != nil {
		return Event{}, err
	}
	v, err := load(ctx, tx, id, false)
	if err != nil {
		return v, err
	}
	if old.ID > 0 && old.Status != "draft" && (!sameTime(old.StartsAt, v.StartsAt) || !sameTime(old.EndsAt, v.EndsAt) || old.Location != v.Location || old.TimeZone != v.TimeZone) {
		if err = notifyEvent(ctx, tx, id, "event_rescheduled", v.Revision); err != nil {
			return Event{}, err
		}
	}
	if err = database.AuditObject(ctx, tx, &a.ID, action, "free_event", id, database.ChangedFields(old.Input, v.Input, "title", "type", "description", "organizer", "responsible_ids", "starts_at", "ends_at", "time_zone", "location", "capacity", "registration_starts_at", "registration_ends_at", "conditions", "required_documents", "revision")...); err != nil {
		return Event{}, err
	}
	decorate(&v, a)
	return v, tx.Commit(ctx)
}
func conflict() error {
	return httpx.E(409, "event_conflict", "Событие изменилось. Обновите карточку перед повтором.")
}
func (r *Repository) Action(ctx context.Context, id int64, in ActionInput) (Event, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Manage, true)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback(ctx)
	v, err := load(ctx, tx, id, true)
	if err != nil {
		return v, err
	}
	if v.Revision != in.Revision {
		return Event{}, conflict()
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Event{}, err
	}
	next := ""
	column := ""
	switch {
	case in.Action == "publish" && v.Status == "draft":
		if err = validate(&v.Input, true); err != nil {
			return Event{}, err
		}
		if err = validateStaff(ctx, tx, v.ResponsibleIDs); err != nil {
			return Event{}, err
		}
		if !v.StartsAt.After(now) || !v.RegistrationEndsAt.After(now) {
			return Event{}, httpx.Invalid("registration_ends_at", "Для публикации начало события и окончание регистрации должны быть в будущем.")
		}
		next = "published"
		column = "published_at"
	case in.Action == "close_registration" && v.Status == "published":
		next = "registration_closed"
		column = "registration_closed_at"
	case in.Action == "cancel" && (v.Status == "published" || v.Status == "registration_closed"):
		next = "cancelled"
		column = "cancelled_at"
	case in.Action == "complete" && v.Status == "registration_closed":
		if v.EndsAt == nil || v.EndsAt.After(now) {
			return Event{}, httpx.E(409, "event_not_ended", "Завершить событие можно после времени окончания.")
		}
		next = "completed"
		column = "completed_at"
	default:
		return Event{}, httpx.E(409, "event_transition", "Этот переход из текущего состояния недоступен.")
	}
	if _, err = tx.Exec(ctx, `UPDATE free_events SET status=$2,`+column+`=clock_timestamp(),revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id, next); err != nil {
		return Event{}, err
	}
	if next == "cancelled" {
		if err = notifyEvent(ctx, tx, id, "event_cancelled", v.Revision+1); err != nil {
			return Event{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE event_registrations SET status='cancelled',revision=revision+1,updated_by=$2,updated_at=clock_timestamp() WHERE event_id=$1 AND status IN ('requested','confirmed','waitlisted')`, id, a.ID); err != nil {
			return Event{}, err
		}
	}
	if err = database.AuditObject(ctx, tx, &a.ID, "event."+next, "free_event", id, "status", column, "revision"); err != nil {
		return Event{}, err
	}
	v, err = load(ctx, tx, id, false)
	if err != nil {
		return v, err
	}
	decorate(&v, a)
	return v, tx.Commit(ctx)
}
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
func (r *Repository) Staff(ctx context.Context, f Filter) (Page[Person], error) {
	out := Page[Person]{Items: []Person{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Manage, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if access.ScopeFor(a, access.Events, access.Manage) != access.All {
		return out, httpx.Forbidden()
	}
	// Go folds Unicode consistently, including Cyrillic on PostgreSQL clusters
	// created with the C locale. Stream names only; retain at most one page.
	rows, err := tx.Query(ctx, `SELECT u.id,u.name FROM users u WHERE `+staffSQL+` ORDER BY u.name,u.id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		v := Person{Active: true}
		if err = rows.Scan(&v.ID, &v.Name); err != nil {
			rows.Close()
			return out, err
		}
		if strings.Contains(strings.ToLower(v.Name), f.Search) {
			out.Total++
			if out.Total > (f.Page-1)*out.PageSize && len(out.Items) < out.PageSize {
				out.Items = append(out.Items, v)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
