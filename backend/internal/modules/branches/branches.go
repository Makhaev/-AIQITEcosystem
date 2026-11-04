package branches

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Repository struct{ Pool *pgxpool.Pool }
type Service struct{ Repo *Repository }
type Handler struct{ Service *Service }
type Input struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (s *Service) Save(ctx context.Context, id int64, in Input) (map[string]any, error) {
	in.Name = strings.TrimSpace(in.Name)
	if utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 200 || strings.ContainsRune(in.Name, 0) {
		return nil, httpx.Invalid("name", "Название: от 1 до 200 символов.")
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "archived" {
		return nil, httpx.Invalid("status", "Выберите действующий или архивный филиал.")
	}
	value, err := s.Repo.Save(ctx, id, in)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.E(404, "not_found", "Филиал не найден.")
	}
	return value, err
}
func (r *Repository) Save(ctx context.Context, id int64, in Input) (map[string]any, error) {
	tx, a, e := access.Begin(ctx, r.Pool, access.Groups, access.Manage, true)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if id == 0 {
		e = tx.QueryRow(ctx, `INSERT INTO branches(name,status) VALUES($1,$2) RETURNING id`, in.Name, in.Status).Scan(&id)
	} else {
		var found int64
		e = tx.QueryRow(ctx, `UPDATE branches SET name=$2,status=$3 WHERE id=$1 RETURNING id`, id, in.Name, in.Status).Scan(&found)
	}
	if e != nil {
		return nil, e
	}
	if e = database.AuditObject(ctx, tx, &a.ID, "branch.saved", "branch", id, "name", "status"); e != nil {
		return nil, e
	}
	return map[string]any{"id": id, "name": in.Name, "status": in.Status}, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, page, size int) (map[string]any, error) {
	tx, a, e := access.Begin(ctx, r.Pool, access.Groups, access.Read, false)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	scope := access.ScopeFor(a, access.Groups, access.Read)
	where := ` WHERE ($1 OR EXISTS(SELECT 1 FROM groups g LEFT JOIN teachers t ON t.id=g.teacher_id WHERE g.branch_id=b.id AND ` + access.GroupReadScopeSQL + `))`
	args := []any{scope == access.All, scope&access.Own != 0, scope&access.Assigned != 0, a.ID}
	var total int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM branches b`+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT jsonb_build_object('id',b.id,'name',b.name,'status',b.status) FROM branches b`+where+` ORDER BY b.name,b.id LIMIT $5 OFFSET $6`, append(args, size, (page-1)*size)...)
	if e != nil {
		return nil, e
	}
	items := []map[string]any{}
	for rows.Next() {
		var item map[string]any
		if e = rows.Scan(&item); e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": items, "total": total, "page": page, "page_size": size}, tx.Commit(ctx)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, size := 1, 100
	for _, v := range []struct {
		k   string
		p   *int
		max int
	}{{"page", &page, 1000000}, {"page_size", &size, 100}} {
		if raw := r.URL.Query().Get(v.k); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 || n > v.max {
				httpx.Fail(w, r, httpx.Invalid(v.k, "Некорректная страница."))
				return
			}
			*v.p = n
		}
	}
	data, e := h.Service.Repo.List(r.Context(), page, size)
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	httpx.JSON(w, 200, data)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	id := int64(0)
	if raw := r.PathValue("id"); raw != "" {
		var e error
		id, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || id <= 0 {
			httpx.Fail(w, r, httpx.Invalid("id", "Некорректный филиал."))
			return
		}
	}
	var in Input
	if e := httpx.Decode(w, r, &in); e != nil {
		httpx.Fail(w, r, e)
		return
	}
	data, e := h.Service.Save(r.Context(), id, in)
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	httpx.JSON(w, status, data)
}
