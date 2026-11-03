package notifications

import (
	"context"
	"fmt"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/notify"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }
type Item struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}
type Page struct {
	Items    []Item `json:"items"`
	Total    int    `json:"total"`
	Unread   int    `json:"unread"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func title(kind string) string {
	switch kind {
	case "invitation":
		return "Приглашение в AIQ Core"
	case "password_reset":
		return "Запрошено восстановление пароля"
	case "lesson_rescheduled":
		return "Занятие перенесено"
	case "lesson_cancelled":
		return "Занятие отменено"
	case "assignment_assigned":
		return "Назначено учебное задание"
	case "attempt_reviewed":
		return "Работа проверена"
	case "report_published":
		return "Опубликован отчёт"
	case "request_answered":
		return "Получен ответ на обращение"
	case "participation_confirmed":
		return "Участие в событии подтверждено"
	case "participation_cancelled":
		return "Участие в событии отменено"
	case "event_rescheduled":
		return "Событие перенесено"
	case "event_cancelled":
		return "Событие отменено"
	}
	return "Новое уведомление"
}
func target(kind, audience string, id int64) string {
	if kind == "participation_confirmed" || kind == "participation_cancelled" || kind == "event_rescheduled" || kind == "event_cancelled" {
		return fmt.Sprintf("#/events/%d", id)
	}
	if audience == "account" {
		return "#/account"
	}
	if audience == "teacher" {
		return fmt.Sprintf("#/lessons/%d", id)
	}
	if kind == "request_answered" {
		return fmt.Sprintf("#/my-requests/%d", id)
	}
	return "#/family"
}

const inboxFrom = ` FROM notifications n JOIN users u ON u.id=n.user_id WHERE n.user_id=$1 AND ` + notify.VisibleSQL

// Contexts can overlap (two children in one lesson, or parent + teacher roles).
// The user sees one in-app delivery per event, just as the email outbox does.
const inboxGrouped = `SELECT min(n.id) AS id,n.kind,n.object_id,min(n.audience) AS audience,min(n.created_at) AS created_at,max(n.read_at) AS read_at` + inboxFrom + ` GROUP BY n.kind,n.object_id,n.event_version,n.user_id`

func (r *Repository) List(ctx context.Context, page int, unread bool, sizes ...int) (Page, error) {
	out := Page{Items: []Item{}, Page: page, PageSize: httpx.PageSize(sizes...)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Account, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE NOT $2 OR read_at IS NULL),count(*) FILTER(WHERE read_at IS NULL) FROM (`+inboxGrouped+`) visible`, a.ID, unread).Scan(&out.Total, &out.Unread); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id,kind,audience,object_id,created_at,read_at FROM (`+inboxGrouped+`) visible WHERE (NOT $2 OR read_at IS NULL) ORDER BY id DESC LIMIT $4 OFFSET $3`, a.ID, unread, (page-1)*out.PageSize, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v Item
		var audience string
		var id int64
		if err = rows.Scan(&v.ID, &v.Kind, &audience, &id, &v.CreatedAt, &v.ReadAt); err != nil {
			rows.Close()
			return out, err
		}
		v.Title = title(v.Kind)
		v.URL = target(v.Kind, audience, id)
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Read(ctx context.Context, id int64) error {
	tx, a, err := access.Begin(ctx, r.Pool, access.Account, access.Read, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE notifications SET read_at=COALESCE(read_at,clock_timestamp()) WHERE (kind,object_id,event_version,user_id) IN (SELECT n.kind,n.object_id,n.event_version,n.user_id`+inboxFrom+` AND n.id=$2)`, a.ID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.E(404, "not_found", "Уведомление недоступно.")
	}
	return tx.Commit(ctx)
}
