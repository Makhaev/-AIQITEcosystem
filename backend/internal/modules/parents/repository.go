package parents

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

type Repository struct{ Pool *pgxpool.Pool }

func (r *Repository) begin(ctx context.Context, resource access.Resource, write bool) (pgx.Tx, httpx.Actor, error) {
	return access.Begin(ctx, r.Pool, resource, access.Manage, write)
}

const studentColumns = `s.id,s.name,s.birth_date::text,s.status,s.curator_id,u.name,s.created_at,s.updated_at`

const studentFrom = ` FROM students s LEFT JOIN users u ON u.id=s.curator_id`

const parentColumns = `p.id,p.name,p.phone,p.email,p.status,p.created_at,p.updated_at`

// Full family cards remain available only to All/Assigned scopes.

func scanParent(row pgx.Row) (Parent, error) {
	var v Parent
	err := row.Scan(&v.ID, &v.Name, &v.Phone, &v.Email, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func scopes(actor httpx.Actor, resource access.Resource, action access.Action) (bool, bool) {
	s := access.ScopeFor(actor, resource, action)
	return s == access.All, s&access.Assigned != 0
}

func (r *Repository) SaveParent(ctx context.Context, id int64, in ParentInput, patch *ParentUpdate) (Parent, error) {
	tx, actor, err := r.begin(ctx, access.Parents, true)
	if err != nil {
		return Parent{}, err
	}
	defer tx.Rollback(ctx)
	var old Parent
	if patch != nil {
		old, err = scanParent(tx.QueryRow(ctx, `SELECT `+parentColumns+` FROM parents p WHERE p.id=$1 FOR UPDATE`, id))
		if err != nil {
			return Parent{}, err
		}
	}
	in, action, err := prepareParent(old, in, patch)
	if err != nil {
		return Parent{}, err
	}
	var value Parent
	if patch == nil {
		value, err = scanParent(tx.QueryRow(ctx, `INSERT INTO parents(name,name_search,phone,email,status) VALUES($1,$2,$3,$4,$5) RETURNING id,name,phone,email,status,created_at,updated_at`, in.Name, strings.ToLower(in.Name), in.Phone, in.Email, in.Status))
	} else {
		value, err = scanParent(tx.QueryRow(ctx, `UPDATE parents SET name=$2,name_search=$3,phone=$4,email=$5,status=$6,updated_at=now() WHERE id=$1 RETURNING id,name,phone,email,status,created_at,updated_at`, id, in.Name, strings.ToLower(in.Name), in.Phone, in.Email, in.Status))
	}
	if err != nil {
		return Parent{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "parent", value.ID, database.ChangedFields(old, value, "name", "phone", "email", "status")...); err != nil {
		return Parent{}, err
	}
	return value, tx.Commit(ctx)
}

func (r *Repository) Parents(ctx context.Context, f Filter) (Page[Parent], error) {
	result := Page[Parent]{Items: []Parent{}, Page: f.Page, PageSize: f.Size}
	tx, actor, err := r.begin(ctx, access.Parents, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all, assigned := scopes(actor, access.Parents, access.Read)
	where := ` WHERE ` + parentScope + ` AND ($4='' OR strpos(p.name_search,$4)>0 OR strpos(p.phone,$4)>0 OR strpos(p.email,$4)>0) AND ($5='' OR p.status=$5)`
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM parents p`+where, all, assigned, actor.ID, f.Search, f.Status).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+parentColumns+` FROM parents p`+where+` ORDER BY p.name_search,p.id LIMIT $6 OFFSET $7`, all, assigned, actor.ID, f.Search, f.Status, f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v, e := scanParent(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *Repository) Parent(ctx context.Context, id int64) (ParentCard, error) {
	result := ParentCard{Children: []StudentLink{}}
	tx, actor, err := r.begin(ctx, access.Parents, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all, assigned := scopes(actor, access.Parents, access.Read)
	result.Parent, err = scanParent(tx.QueryRow(ctx, `SELECT `+parentColumns+` FROM parents p WHERE `+parentScope+` AND p.id=$4`, all, assigned, actor.ID, id))
	if err != nil {
		return result, err
	}
	all, assigned = scopes(actor, access.Students, access.ReadFull)
	rows, err := tx.Query(ctx, `SELECT `+studentColumns+`,sp.relationship,sp.is_primary`+studentFrom+` JOIN student_parents sp ON sp.student_id=s.id WHERE `+studentScope+` AND sp.parent_id=$4 ORDER BY s.name_search,s.id`, all, assigned, actor.ID, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v := StudentLink{Link: Link{ParentID: id}}
		err = rows.Scan(&v.ID, &v.Name, &v.BirthDate, &v.Status, &v.CuratorID, &v.CuratorName, &v.CreatedAt, &v.UpdatedAt, &v.Relationship, &v.IsPrimary)
		if err != nil {
			rows.Close()
			return result, err
		}
		v.StudentID = v.ID
		result.Children = append(result.Children, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

const parentScope = access.ParentReadScopeSQL
const studentScope = access.StudentFullReadScopeSQL
