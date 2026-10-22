package parents

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
)

type ContactMatchInput struct {
	Phone     string `json:"phone"`
	Email     string `json:"email"`
	ExcludeID *int64 `json:"exclude_id"`
}

type ContactMatch struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Status        string   `json:"status"`
	MatchedFields []string `json:"matched_fields"`
}

func (r *Repository) ContactMatches(ctx context.Context, in ContactMatchInput, f Filter) (Page[ContactMatch], error) {
	result := Page[ContactMatch]{Items: []ContactMatch{}, Page: f.Page, PageSize: f.Size}
	if in.ExcludeID != nil && *in.ExcludeID <= 0 {
		return result, httpx.Invalid("exclude_id", "Некорректный идентификатор родителя.")
	}
	contact := ParentInput{Name: "Проверка контактов", Phone: in.Phone, Email: in.Email}
	if err := normalizeParent(&contact); err != nil {
		return result, err
	}
	tx, actor, err := r.begin(ctx, access.Parents, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if access.ScopeFor(actor, access.Parents, access.Manage) != access.All {
		return result, httpx.Forbidden()
	}
	const where = ` FROM parents p WHERE (($1<>'' AND p.phone=$1) OR ($2<>'' AND p.email=$2)) AND ($3::bigint IS NULL OR p.id<>$3)`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, contact.Phone, contact.Email, in.ExcludeID).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.name,p.status,array_remove(ARRAY[CASE WHEN $1<>'' AND p.phone=$1 THEN 'phone' END,CASE WHEN $2<>'' AND p.email=$2 THEN 'email' END],NULL)`+where+` ORDER BY p.name_search,p.id LIMIT $4 OFFSET $5`, contact.Phone, contact.Email, in.ExcludeID, f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var v ContactMatch
		if err = rows.Scan(&v.ID, &v.Name, &v.Status, &v.MatchedFields); err != nil {
			return result, err
		}
		result.Items = append(result.Items, v)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	return result, tx.Commit(ctx)
}
