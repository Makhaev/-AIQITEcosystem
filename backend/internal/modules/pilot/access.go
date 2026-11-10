package pilot

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	Pool       *pgxpool.Pool
	Origin     string
	TrustProxy bool
}

func fail(w http.ResponseWriter, r *http.Request, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) {
		err = httpx.E(404, "not_found", "Запись не найдена или недоступна.")
	} else if errors.As(err, &pg) && pg.Code == "23505" {
		err = httpx.E(409, "conflict", "Учётная запись или связь уже существует.")
	} else if errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "23514") {
		err = httpx.Invalid("record", "Проверьте связанные записи и допустимость операции.")
	}
	httpx.Fail(w, r, err)
}
func textValid(value string, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(value))
	return n > 0 && n <= max
}
func audit(ctx context.Context, tx pgx.Tx, actor int64, action, kind string, id int64) error {
	return database.AuditObject(ctx, tx, &actor, action, kind, id)
}
func (h *Handler) Invitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email     string `json:"email"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		ProfileID int64  `json:"profile_id"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	email, err := users.NormalizeEmail(in.Email)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !textValid(in.Name, 200) || in.ProfileID <= 0 {
		fail(w, r, httpx.Invalid("profile_id", "Выберите существующую карточку и укажите имя."))
		return
	}
	table := ""
	switch in.Role {
	case "parent":
		table = "parents"
	case "student":
		table = "students"
	case "teacher":
		table = "teachers"
	default:
		fail(w, r, httpx.Invalid("role", "Приглашение доступно ученику, родителю или педагогу."))
		return
	}
	secret, err := auth.Token()
	if err != nil {
		fail(w, r, err)
		return
	}
	hash, err := users.HashPassword(secret)
	if err != nil {
		fail(w, r, err)
		return
	}
	token, err := auth.Token()
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = access.LockManagement(r.Context(), tx); err != nil {
		fail(w, r, err)
		return
	}
	actor, err := access.Authorize(r.Context(), tx, access.Users, access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	var available bool
	err = tx.QueryRow(r.Context(), `SELECT user_id IS NULL AND status='active' FROM `+table+` WHERE id=$1 FOR UPDATE`, in.ProfileID).Scan(&available)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !available {
		fail(w, r, httpx.E(409, "profile_linked", "Карточка уже связана с пользователем или находится в архиве."))
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(), `INSERT INTO users(email,name,password_hash) VALUES($1,$2,$3) RETURNING id`, email, strings.TrimSpace(in.Name), hash).Scan(&id)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO user_roles(user_id,role) VALUES($1,$2)`, id, in.Role)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE `+table+` SET user_id=$1 WHERE id=$2`, id, in.ProfileID)
	}
	expires := time.Now().UTC().Add(48 * time.Hour)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO access_links(token_hash,user_id,purpose,expires_at) VALUES($1,$2,'invitation',$3)`, auth.Hash(token), id, expires)
	}
	if err == nil {
		err = audit(r.Context(), tx, actor.ID, "access.invited", "user", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"user_id": id, "url": h.Origin + "/#/access/" + token, "expires_at": expires})
}

// Anonymous requests never reveal whether an email exists. Manual delivery is intentional.
func (h *Handler) RequestRecovery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	ip := httpx.ClientIP(r, h.TrustProxy)
	allowed, err := (&auth.Repository{Pool: h.Pool}).AllowIP(r.Context(), auth.Hash("recovery:"+ip))
	if err != nil {
		fail(w, r, err)
		return
	}
	if !allowed {
		fail(w, r, httpx.E(429, "rate_limited", "Слишком много запросов. Повторите позже."))
		return
	}
	email, _ := users.NormalizeEmail(in.Email)
	_, err = h.Pool.Exec(r.Context(), `INSERT INTO access_requests(user_id) SELECT id FROM users WHERE email=$1 AND NOT blocked ON CONFLICT(user_id) DO UPDATE SET requested_at=now(),resolved_at=NULL`, email)
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 202, map[string]string{"message": "Если учётная запись существует, администратор сможет передать вам ссылку после проверки личности. Свяжитесь с администратором AIQ."})
}
func (h *Handler) Requests(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `SELECT u.id,u.email,u.name,a.requested_at FROM access_requests a JOIN users u ON u.id=a.user_id WHERE a.resolved_at IS NULL AND NOT u.blocked ORDER BY a.requested_at LIMIT 200`)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var email, name string
		var at time.Time
		if err = rows.Scan(&id, &email, &name, &at); err != nil {
			fail(w, r, err)
			return
		}
		items = append(items, map[string]any{"user_id": id, "email": email, "name": name, "requested_at": at})
	}
	if err = rows.Err(); err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) RecoveryLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		UserID int64 `json:"user_id"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	token, err := auth.Token()
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if err = access.LockManagement(r.Context(), tx); err != nil {
		fail(w, r, err)
		return
	}
	actor, err := access.Authorize(r.Context(), tx, access.Users, access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	var blocked bool
	err = tx.QueryRow(r.Context(), `SELECT blocked FROM users WHERE id=$1 FOR UPDATE`, in.UserID).Scan(&blocked)
	if err != nil {
		fail(w, r, err)
		return
	}
	if blocked {
		fail(w, r, httpx.Forbidden())
		return
	}
	expires := time.Now().UTC().Add(30 * time.Minute)
	_, err = tx.Exec(r.Context(), `UPDATE access_links SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, in.UserID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO access_links(token_hash,user_id,purpose,expires_at) VALUES($1,$2,'recovery',$3)`, auth.Hash(token), in.UserID, expires)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE access_requests SET resolved_at=now() WHERE user_id=$1`, in.UserID)
	}
	if err == nil {
		err = audit(r.Context(), tx, actor.ID, "access.recovery_issued", "user", in.UserID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"url": h.Origin + "/#/access/" + token, "expires_at": expires})
}
func (h *Handler) AcceptLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !auth.ValidToken(in.Token) {
		fail(w, r, httpx.E(400, "invalid_link", "Ссылка недействительна или истекла."))
		return
	}
	ip := httpx.ClientIP(r, h.TrustProxy)
	allowed, err := (&auth.Repository{Pool: h.Pool}).AllowIP(r.Context(), auth.Hash("accept-link:"+ip))
	if err != nil {
		fail(w, r, err)
		return
	}
	if !allowed {
		fail(w, r, httpx.E(429, "rate_limited", "Слишком много запросов. Повторите позже."))
		return
	}
	// Reject unknown, expired and consumed tokens before expensive bcrypt work
	// or the management lock. The transaction below still rechecks single use.
	var valid bool
	err = h.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM access_links l JOIN users u ON u.id=l.user_id WHERE l.token_hash=$1 AND l.used_at IS NULL AND l.expires_at>clock_timestamp() AND NOT u.blocked)`, auth.Hash(in.Token)).Scan(&valid)
	if err != nil {
		fail(w, r, err)
		return
	}
	if !valid {
		fail(w, r, httpx.E(400, "invalid_link", "Ссылка недействительна или истекла."))
		return
	}
	hash, err := users.HashPassword(in.Password)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Same management -> user -> link lock order as link issuance and account changes.
	if err = access.LockManagement(r.Context(), tx); err != nil {
		fail(w, r, err)
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(), `SELECT user_id FROM access_links WHERE token_hash=$1`, auth.Hash(in.Token)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, httpx.E(400, "invalid_link", "Ссылка недействительна или истекла."))
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	var blocked bool
	err = tx.QueryRow(r.Context(), `SELECT blocked FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&blocked)
	if err != nil {
		fail(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE access_links SET used_at=clock_timestamp() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>clock_timestamp()`, auth.Hash(in.Token))
	if err != nil {
		fail(w, r, err)
		return
	}
	if blocked || tag.RowsAffected() != 1 {
		fail(w, r, httpx.E(400, "invalid_link", "Ссылка недействительна или истекла."))
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2,must_change_password=false,failed_attempts=0,failure_window_start=NULL,locked_until=NULL,updated_at=now() WHERE id=$1`, id, hash)
	if err == nil {
		_, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, id)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE access_links SET used_at=now() WHERE user_id=$1 AND used_at IS NULL`, id)
	}
	if err == nil {
		err = audit(r.Context(), tx, id, "access.accepted", "user", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(204)
}
