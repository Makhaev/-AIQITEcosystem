package notifications

import (
	"aiq.local/core/internal/platform/notify"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

type Sender interface {
	Ready() error
	SendNotification(context.Context, string, string, string) error
}

// Implement only when the provider really supports a stable deduplication key.
// An SMTP Message-ID alone does not provide this guarantee.
type IdempotentSender interface {
	SendNotificationIdempotent(ctx context.Context, email, subject, link, key string) (Handoff, error)
}
type Handoff struct{ Provider, MessageID string }
type Worker struct {
	Repo   *Repository
	Sender Sender
	Origin string
}
type deliveryClaim struct {
	ID    int64
	Token string
}

// Claims commit before network I/O. A crash consumes an attempt and becomes
// recoverable after the lease, with a bounded number of recovery attempts.
func (r *Repository) claim(ctx context.Context) (deliveryClaim, bool, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return deliveryClaim{}, false, err
	}
	defer tx.Rollback(ctx)
	var c deliveryClaim
	var count, max int
	err = tx.QueryRow(ctx, `SELECT id,cycle_attempts,max_attempts FROM notification_deliveries
 WHERE channel='email' AND ((status='pending' AND next_attempt_at<=clock_timestamp()) OR (status='processing' AND lease_until<=clock_timestamp()))
 ORDER BY COALESCE(next_attempt_at,lease_until),id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&c.ID, &count, &max)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	if count >= max {
		_, err = tx.Exec(ctx, `UPDATE notification_deliveries SET status='failed',last_error='attempts_exhausted',next_attempt_at=NULL,claim_token=NULL,lease_until=NULL,revision=revision+1 WHERE id=$1`, c.ID)
		if err != nil {
			return c, true, err
		}
		return deliveryClaim{}, true, tx.Commit(ctx)
	}
	err = tx.QueryRow(ctx, `UPDATE notification_deliveries SET status='processing',attempts=attempts+1,cycle_attempts=cycle_attempts+1,
 next_attempt_at=NULL,lease_until=clock_timestamp()+interval '60 seconds',claim_token=gen_random_uuid(),revision=revision+1,
 last_error=CASE WHEN status='processing' THEN 'processing_expired' ELSE last_error END WHERE id=$1 RETURNING claim_token::text`, c.ID).Scan(&c.Token)
	if err != nil {
		return c, true, err
	}
	return c, true, tx.Commit(ctx)
}

// d is the outbox row. Any live context for this event and recipient suffices.
const deliveryAllowed = `EXISTS(SELECT 1 FROM notifications n JOIN users u ON u.id=n.user_id
 WHERE n.kind=d.kind AND n.object_id=d.object_id AND n.event_version=d.event_version AND n.user_id=d.user_id AND ` + notify.VisibleSQL + `)`

func (w *Worker) DeliverOne(ctx context.Context) (bool, error) {
	c, worked, err := w.Repo.claim(ctx)
	if err != nil || !worked || c.ID == 0 {
		return worked, err
	}
	// Persist results even during shutdown. The provider has a shorter deadline.
	// Only this outbox row stays locked during I/O: recovery skips active workers.
	dbctx, cancelDB := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancelDB()
	tx, err := w.Repo.Pool.Begin(dbctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(dbctx)
	var kind, email, key string
	var allowed bool
	var cycle, max int
	err = tx.QueryRow(dbctx, `SELECT d.kind,u.email,d.idempotency_key::text,`+deliveryAllowed+`,d.cycle_attempts,d.max_attempts
 FROM notification_deliveries d JOIN users u ON u.id=d.user_id WHERE d.id=$1 AND d.status='processing'
 AND d.claim_token=$2::uuid AND d.lease_until>clock_timestamp() FOR UPDATE OF d`, c.ID, c.Token).Scan(&kind, &email, &key, &allowed, &cycle, &max)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	code := ""
	receipt := Handoff{Provider: "smtp"}
	if !allowed {
		code = "recipient_unavailable"
	} else {
		origin, e := url.Parse(w.Origin)
		if e != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" || origin.Scheme != "https" && origin.Scheme != "http" || w.Sender == nil {
			code = "configuration_unavailable"
		} else if e = w.Sender.Ready(); e != nil {
			code = "configuration_unavailable"
		} else {
			sendctx, stop := context.WithTimeout(ctx, 10*time.Second)
			link := strings.TrimRight(w.Origin, "/") + "/#/notifications"
			if provider, ok := w.Sender.(IdempotentSender); ok {
				receipt, e = provider.SendNotificationIdempotent(sendctx, email, title(kind), link, key)
			} else {
				e = w.Sender.SendNotification(sendctx, email, title(kind), link)
			}
			stop()
			if e != nil {
				code = "provider_error"
			} else if receipt.Provider == "" || len(receipt.Provider) > 100 || len(receipt.MessageID) > 500 {
				code = "invalid_provider_receipt"
			}
		}
	}
	if code == "" {
		_, err = tx.Exec(dbctx, `UPDATE notification_deliveries SET status='sent',sent_at=clock_timestamp(),last_error='',provider=$3,provider_message_id=$4,
   claim_token=NULL,lease_until=NULL,revision=revision+1 WHERE id=$1 AND claim_token=$2::uuid`, c.ID, c.Token, receipt.Provider, receipt.MessageID)
	} else {
		terminal := cycle >= max || code == "recipient_unavailable"
		_, err = tx.Exec(dbctx, `UPDATE notification_deliveries SET status=CASE WHEN $4 THEN 'failed' ELSE 'pending' END,last_error=$3,
   next_attempt_at=CASE WHEN $4 THEN NULL ELSE clock_timestamp()+make_interval(secs=>LEAST(3600,30*power(2,LEAST(cycle_attempts-1,7)))::int) END,
   claim_token=NULL,lease_until=NULL,revision=revision+1 WHERE id=$1 AND claim_token=$2::uuid`, c.ID, c.Token, code, terminal)
		slog.Warn("notification delivery attempt failed", "delivery_id", c.ID, "code", code)
	}
	if err != nil {
		return true, err
	}
	return true, tx.Commit(dbctx)
}
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 20; i++ {
				done, err := w.DeliverOne(ctx)
				if err != nil {
					slog.Warn("notification delivery unavailable")
					break
				}
				if !done {
					break
				}
			}
		}
	}
}

// Trusted adapter boundary, NOT an HTTP webhook. Verify provider authentication
// and evidence before calling. SMTP and inbox read marks never call this method.
func (r *Repository) ConfirmDelivery(ctx context.Context, provider, messageID, evidence string, at time.Time) error {
	if provider == "" || messageID == "" || evidence == "" || len(provider) > 100 || len(messageID) > 500 || len(evidence) > 500 || at.IsZero() || at.After(time.Now().Add(time.Minute)) {
		return errors.New("invalid delivery confirmation")
	}
	tag, err := r.Pool.Exec(ctx, `UPDATE notification_deliveries SET delivered_at=$4,delivery_evidence=$3,revision=revision+1
 WHERE channel='email' AND status='sent' AND provider=$1 AND provider_message_id=$2 AND delivered_at IS NULL AND sent_at<=$4`, provider, messageID, evidence, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var duplicate bool
	err = r.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notification_deliveries WHERE channel='email' AND status='sent' AND provider=$1 AND provider_message_id=$2 AND delivery_evidence=$3 AND delivered_at=$4)`, provider, messageID, evidence, at).Scan(&duplicate)
	if err != nil {
		return err
	}
	if !duplicate {
		return errors.New("unmatched delivery confirmation")
	}
	return nil
}
