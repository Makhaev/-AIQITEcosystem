package events

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/notify"
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type Registration struct {
	ID             int64     `json:"id"`
	StudentID      int64     `json:"student_id"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	Revision       int64     `json:"revision"`
	CreatedAt      time.Time `json:"created_at"`
	AllowedActions []string  `json:"allowed_actions"`
}
type RegistrationInput struct {
	StudentID int64  `json:"student_id"`
	Action    string `json:"action"`
	Revision  int64  `json:"revision"`
}
type ParticipationPage struct {
	Page[Registration]
	CanManage              bool `json:"can_manage"`
	SelfRegistrationMinAge int  `json:"self_registration_min_age"`
	Occupied               int  `json:"occupied"`
}
type History struct {
	Status    string    `json:"status"`
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"created_at"`
}

const parentAccess = `EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id AND p.status='active' JOIN family_access fa ON fa.parent_id=p.id AND fa.confirmed JOIN user_roles ur ON ur.user_id=pa.user_id AND ur.role='parent' WHERE pa.user_id=$1 AND fa.student_id=s.id)`
const selfAccess = `EXISTS(SELECT 1 FROM student_accounts sa JOIN user_roles ur ON ur.user_id=sa.user_id AND ur.role='student' WHERE sa.user_id=$1 AND sa.student_id=s.id)`

func manager(ctx context.Context, tx pgx.Tx, a httpx.Actor, eid int64) (bool, error) {
	if access.ScopeFor(a, access.Events, access.Manage) == access.All {
		return true, nil
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM free_event_responsibles er JOIN users u ON u.id=er.user_id WHERE er.event_id=$1 AND u.id=$2 AND `+staffSQL+`)`, eid, a.ID).Scan(&ok)
	return ok, err
}
func participant(ctx context.Context, tx pgx.Tx, a httpx.Actor, sid int64, manage bool) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students s WHERE s.id=$2 AND ($3 OR notification_family_access($1,s.id)))`, a.ID, sid, manage).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return pgx.ErrNoRows
	}
	return nil
}
func (r *Repository) eligible(ctx context.Context, tx pgx.Tx, a httpx.Actor, sid int64, manage bool, zone string) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students s WHERE s.id=$2 AND s.status='active' AND ($3 OR `+parentAccess+` OR (`+selfAccess+` AND $4::integer>0 AND s.birth_date IS NOT NULL AND s.birth_date<=((clock_timestamp() AT TIME ZONE $5)::date-make_interval(years=>$4))::date)))`, a.ID, sid, manage, r.SelfRegistrationMinAge, zone).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return httpx.E(403, "registration_ineligible", "Регистрация этого ученика недоступна: проверьте семейный доступ и возрастное правило.")
	}
	return nil
}
func occupied(ctx context.Context, tx pgx.Tx, eid int64) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM event_registrations WHERE event_id=$1 AND status IN ('requested','confirmed','attended','no_show')`, eid).Scan(&n)
	return n, err
}
func registration(ctx context.Context, tx pgx.Tx, eid, rid int64) (Registration, error) {
	var v Registration
	err := tx.QueryRow(ctx, `SELECT r.id,r.student_id,s.name,r.status,r.revision,r.created_at FROM event_registrations r JOIN students s ON s.id=r.student_id WHERE r.event_id=$1 AND r.id=$2`, eid, rid).Scan(&v.ID, &v.StudentID, &v.Name, &v.Status, &v.Revision, &v.CreatedAt)
	v.AllowedActions = []string{}
	return v, err
}
func registrationActions(v *Registration, e Event, manage bool, now time.Time) {
	v.AllowedActions = []string{}
	if e.Status == "cancelled" {
		return
	}
	if (e.Status == "published" || e.Status == "registration_closed") && e.StartsAt != nil && now.Before(*e.StartsAt) {
		if v.Status == "requested" || v.Status == "confirmed" || v.Status == "waitlisted" {
			v.AllowedActions = append(v.AllowedActions, "cancelled")
		}
		if manage && (v.Status == "requested" || v.Status == "waitlisted") {
			v.AllowedActions = append(v.AllowedActions, "confirmed")
		}
	}
	if manage && e.StartsAt != nil && !now.Before(*e.StartsAt) {
		switch v.Status {
		case "confirmed":
			v.AllowedActions = append(v.AllowedActions, "attended", "no_show")
		case "attended":
			v.AllowedActions = append(v.AllowedActions, "no_show")
		case "no_show":
			v.AllowedActions = append(v.AllowedActions, "attended")
		}
	}
}
func (r *Repository) Register(ctx context.Context, eid, rid int64, in RegistrationInput) (Registration, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Read, true)
	if err != nil {
		return Registration{}, err
	}
	defer tx.Rollback(ctx)
	e, err := load(ctx, tx, eid, true)
	if err != nil {
		return Registration{}, err
	}
	if e.Status == "draft" {
		return Registration{}, pgx.ErrNoRows
	}
	manage, err := manager(ctx, tx, a, eid)
	if err != nil {
		return Registration{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Registration{}, err
	}
	var v Registration
	if rid == 0 {
		if in.StudentID < 1 || in.Action != "" || in.Revision != 0 {
			return v, httpx.Invalid("student_id", "Укажите ученика без статуса и редакции.")
		}
		if e.Status != "published" || e.RegistrationStartsAt == nil || e.RegistrationEndsAt == nil || now.Before(*e.RegistrationStartsAt) || !now.Before(*e.RegistrationEndsAt) {
			return v, httpx.E(409, "registration_closed", "Период регистрации закрыт.")
		}
		if err = r.eligible(ctx, tx, a, in.StudentID, manage, e.TimeZone); err != nil {
			return v, err
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM event_registrations WHERE event_id=$1 AND student_id=$2 AND status<>'cancelled')`, eid, in.StudentID).Scan(&exists); err != nil {
			return v, err
		}
		if exists {
			return v, httpx.E(409, "duplicate_registration", "У ученика уже есть активная заявка.")
		}
		n, err := occupied(ctx, tx, eid)
		if err != nil {
			return v, err
		}
		status := "requested"
		if n >= e.Capacity {
			status = "waitlisted"
		}
		err = tx.QueryRow(ctx, `INSERT INTO event_registrations(event_id,student_id,status,created_by,updated_by) VALUES($1,$2,$3,$4,$4) RETURNING id`, eid, in.StudentID, status, a.ID).Scan(&rid)
		if err != nil {
			return v, err
		}
	} else {
		v, err = registration(ctx, tx, eid, rid)
		if err != nil {
			return v, err
		}
		if err = participant(ctx, tx, a, v.StudentID, manage); err != nil {
			return Registration{}, err
		}
		if in.Revision != v.Revision {
			return Registration{}, conflict()
		}
		registrationActions(&v, e, manage, now)
		allowed := false
		for _, action := range v.AllowedActions {
			if action == in.Action {
				allowed = true
			}
		}
		if !allowed {
			return Registration{}, httpx.E(409, "registration_transition", "Переход недоступен из текущего состояния или для вашей роли.")
		}
		if in.Action == "confirmed" {
			// Revalidate the participant and capacity under the same event lock.
			if err = r.eligible(ctx, tx, a, v.StudentID, true, e.TimeZone); err != nil {
				return Registration{}, err
			}
			n, err := occupied(ctx, tx, eid)
			if err != nil {
				return Registration{}, err
			}
			if v.Status == "waitlisted" && n >= e.Capacity {
				return Registration{}, httpx.E(409, "event_full", "Свободных мест нет.")
			}
		}
		_, err = tx.Exec(ctx, `UPDATE event_registrations SET status=$2,revision=revision+1,updated_by=$3,updated_at=clock_timestamp() WHERE id=$1`, rid, in.Action, a.ID)
		if err != nil {
			return Registration{}, err
		}
	}
	v, err = registration(ctx, tx, eid, rid)
	if err != nil {
		return v, err
	}
	if v.Status == "confirmed" || v.Status == "cancelled" {
		if err = notify.Participation(ctx, tx, eid, fmt.Sprintf("%d:%d", rid, v.Revision), v.StudentID, v.Status == "confirmed"); err != nil {
			return Registration{}, err
		}
	}
	if err = database.AuditObject(ctx, tx, &a.ID, "event.registration_"+v.Status, "event_registration", rid, "status", "revision"); err != nil {
		return Registration{}, err
	}
	registrationActions(&v, e, manage, now)
	return v, tx.Commit(ctx)
}
func (r *Repository) Registrations(ctx context.Context, eid int64, page int, sizes ...int) (ParticipationPage, error) {
	out := ParticipationPage{Page: Page[Registration]{Items: []Registration{}, Page: page, PageSize: httpx.PageSize(sizes...)}, SelfRegistrationMinAge: r.SelfRegistrationMinAge}
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	e, err := load(ctx, tx, eid, false)
	if err != nil {
		return out, err
	}
	if e.Status == "draft" {
		return out, pgx.ErrNoRows
	}
	out.CanManage, err = manager(ctx, tx, a, eid)
	if err != nil {
		return out, err
	}
	out.Occupied, err = occupied(ctx, tx, eid)
	if err != nil {
		return out, err
	}
	where := ` FROM event_registrations r JOIN students s ON s.id=r.student_id WHERE r.event_id=$1 AND ($2 OR notification_family_access($3,s.id))`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, eid, out.CanManage, a.ID).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT r.id,r.student_id,s.name,r.status,r.revision,r.created_at`+where+` ORDER BY r.id DESC LIMIT $5 OFFSET $4`, eid, out.CanManage, a.ID, (page-1)*out.PageSize, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v Registration
		if err = rows.Scan(&v.ID, &v.StudentID, &v.Name, &v.Status, &v.Revision, &v.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		registrationActions(&v, e, out.CanManage, time.Now())
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Candidates(ctx context.Context, eid int64, f Filter) (Page[Person], error) {
	out := Page[Person]{Items: []Person{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	e, err := load(ctx, tx, eid, false)
	if err != nil {
		return out, err
	}
	if e.Status == "draft" {
		return out, pgx.ErrNoRows
	}
	manage, err := manager(ctx, tx, a, eid)
	if err != nil {
		return out, err
	}
	where := ` FROM students s WHERE s.status='active' AND ($2 OR notification_family_access($1,s.id)) AND ($3='' OR strpos(s.name_search,$3)>0)`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, a.ID, manage, f.Search).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT s.id,s.name,($2 OR `+parentAccess+` OR (`+selfAccess+` AND $5::integer>0 AND s.birth_date IS NOT NULL AND s.birth_date<=((clock_timestamp() AT TIME ZONE $6)::date-make_interval(years=>$5))::date))`+where+` ORDER BY s.name,s.id LIMIT $7 OFFSET $4`, a.ID, manage, f.Search, (f.Page-1)*out.PageSize, r.SelfRegistrationMinAge, e.TimeZone, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v Person
		if err = rows.Scan(&v.ID, &v.Name, &v.Active); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func notifyEvent(ctx context.Context, tx pgx.Tx, eid int64, kind string, revision int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO notifications(user_id,student_id,kind,object_id,event_version,audience) SELECT u.id,r.student_id,$2,$1,$3,'family' FROM event_registrations r JOIN users u ON notification_family_access(u.id,r.student_id) WHERE r.event_id=$1 AND r.status IN ('requested','confirmed','waitlisted') ON CONFLICT DO NOTHING`, eid, kind, fmt.Sprint(revision))
	return err
}
