package notifications

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type RetryInput struct {
	Revision                 int64  `json:"revision"`
	Reason                   string `json:"reason"`
	AcknowledgeDuplicateRisk bool   `json:"acknowledge_duplicate_risk"`
}
type RetryHistory struct {
	ActorID   int64     `json:"actor_id"`
	Reason    string    `json:"reason"`
	Attempts  int       `json:"previous_attempts"`
	CreatedAt time.Time `json:"created_at"`
}
type Delivery struct {
	ID            int64          `json:"id"`
	UserID        int64          `json:"user_id"`
	Kind          string         `json:"kind"`
	Title         string         `json:"title"`
	Channel       string         `json:"channel"`
	Status        string         `json:"status"`
	Attempts      int            `json:"attempts"`
	CycleAttempts int            `json:"cycle_attempts"`
	MaxAttempts   int            `json:"max_attempts"`
	Revision      int64          `json:"revision"`
	NextAttemptAt *time.Time     `json:"next_attempt_at"`
	LeaseUntil    *time.Time     `json:"lease_until"`
	CreatedAt     time.Time      `json:"created_at"`
	SentAt        *time.Time     `json:"sent_at"`
	DeliveredAt   *time.Time     `json:"delivered_at"`
	LastError     string         `json:"last_error"`
	CanRetry      bool           `json:"can_retry"`
	Retries       []RetryHistory `json:"retries"`
}
type DeliveryPage struct {
	Items    []Delivery `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

func (r *Repository) Deliveries(ctx context.Context, page int, status string, sizes ...int) (DeliveryPage, error) {
	out := DeliveryPage{Items: []Delivery{}, Page: page, PageSize: httpx.PageSize(sizes...)}
	tx, _, err := access.Begin(ctx, r.Pool, access.NotificationDeliveries, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM notification_deliveries WHERE channel='email' AND ($1='' OR status=$1)`, status).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT d.id,d.user_id,d.kind,d.channel,d.status,d.attempts,d.cycle_attempts,d.max_attempts,d.revision,d.next_attempt_at,d.lease_until,d.created_at,d.sent_at,d.delivered_at,d.last_error,(d.status='failed' AND `+deliveryAllowed+`)
 FROM notification_deliveries d WHERE d.channel='email' AND ($1='' OR d.status=$1) ORDER BY d.id DESC LIMIT $3 OFFSET $2`, status, (page-1)*out.PageSize, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v Delivery
		v.Retries = []RetryHistory{}
		if err = rows.Scan(&v.ID, &v.UserID, &v.Kind, &v.Channel, &v.Status, &v.Attempts, &v.CycleAttempts, &v.MaxAttempts, &v.Revision, &v.NextAttemptAt, &v.LeaseUntil, &v.CreatedAt, &v.SentAt, &v.DeliveredAt, &v.LastError, &v.CanRetry); err != nil {
			rows.Close()
			return out, err
		}
		v.Title = title(v.Kind)
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for i := range out.Items {
		rows, err = tx.Query(ctx, `SELECT actor_id,reason,previous_attempts,created_at FROM notification_delivery_retries WHERE delivery_id=$1 ORDER BY id`, out.Items[i].ID)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var h RetryHistory
			if err = rows.Scan(&h.ActorID, &h.Reason, &h.Attempts, &h.CreatedAt); err != nil {
				rows.Close()
				return out, err
			}
			out.Items[i].Retries = append(out.Items[i].Retries, h)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Retry(ctx context.Context, id int64, in RetryInput) error {
	tx, a, err := access.Begin(ctx, r.Pool, access.NotificationDeliveries, access.Manage, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var revision int64
	var status, channel string
	var attempts int
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT d.revision,d.status,d.channel,d.attempts,`+deliveryAllowed+` FROM notification_deliveries d WHERE d.id=$1 FOR UPDATE`, id).Scan(&revision, &status, &channel, &attempts, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Отправка не найдена.")
	}
	if err != nil {
		return err
	}
	if revision != in.Revision || status != "failed" || channel != "email" {
		return httpx.E(409, "delivery_conflict", "Состояние отправки изменилось. Обновите очередь.")
	}
	if !allowed {
		return httpx.E(409, "recipient_unavailable", "Получатель или доступ больше не актуальны. Повтор недоступен.")
	}
	_, err = tx.Exec(ctx, `INSERT INTO notification_delivery_retries(delivery_id,actor_id,reason,previous_revision,previous_attempts) VALUES($1,$2,$3,$4,$5)`, id, a.ID, in.Reason, revision, attempts)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE notification_deliveries SET status='pending',cycle_attempts=0,next_attempt_at=clock_timestamp(),last_error='',revision=revision+1 WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if err = database.AuditObject(ctx, tx, &a.ID, "notification.delivery_retry", "notification_delivery", id, "status", "cycle_attempts", "next_attempt_at", "revision"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
