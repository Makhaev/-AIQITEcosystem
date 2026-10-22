package programs

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

const scopeSQL = `($1 OR ($2 AND EXISTS(
 SELECT 1 FROM groups g LEFT JOIN teachers t ON t.id=g.teacher_id WHERE g.program_id=p.id AND
 (t.user_id=$4 OR EXISTS(SELECT 1 FROM lessons l JOIN teachers lt ON lt.id=l.teacher_id WHERE l.group_id=g.id AND lt.user_id=$4))
)) OR ($3 AND EXISTS(
 SELECT 1 FROM groups g JOIN group_enrollments e ON e.group_id=g.id JOIN students s ON s.id=e.student_id WHERE g.program_id=p.id AND s.curator_id=$4
)) OR EXISTS(SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id
 WHERE l.program_id=p.id AND ` + access.LessonReadScopeSQL + `))`

func flags(actor httpx.Actor) (bool, bool, bool) {
	scope := access.ScopeFor(actor, access.Programs, access.Read)
	return scope == access.All, scope&access.Own != 0, scope&access.Assigned != 0
}
func (r *Repository) begin(ctx context.Context, write bool) (pgx.Tx, httpx.Actor, error) {
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		options = pgx.TxOptions{}
	}
	tx, err := r.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, httpx.Actor{}, err
	}
	var actor httpx.Actor
	if write {
		err = access.LockManagement(ctx, tx)
		if err == nil {
			actor, err = access.Authorize(ctx, tx, access.Programs, access.Manage)
		}
	} else {
		actor, err = access.AuthorizeRead(ctx, tx, access.Programs)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}
func (r *Repository) Save(ctx context.Context, id int64, in CreateInput, patch *UpdateInput) (Program, error) {
	tx, actor, err := r.begin(ctx, true)
	if err != nil {
		return Program{}, err
	}
	defer tx.Rollback(ctx)
	action := "program.created"
	var old Program
	if patch != nil {
		old, err = scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM programs p WHERE p.id=$1 FOR UPDATE`, id))
		if err != nil {
			return Program{}, err
		}
		if !patch.Name.Set && !patch.Description.Set && !patch.Status.Set {
			return Program{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		in = CreateInput{Name: old.Name, Description: old.Description, Status: old.Status}
		for _, field := range []struct {
			input    Optional[string]
			target   *string
			required bool
			key      string
		}{{patch.Name, &in.Name, true, "name"}, {patch.Description, &in.Description, false, "description"}, {patch.Status, &in.Status, true, "status"}} {
			if err = patchText(field.input, field.target, field.required, field.key); err != nil {
				return Program{}, err
			}
		}
		action = "program.updated"
		if in.Status != old.Status {
			if in.Status == "archived" {
				action = "program.archived"
			} else if old.Status == "archived" {
				action = "program.restored"
			} else {
				action = "program.status_changed"
			}
		}
	}
	if err = normalize(&in); err != nil {
		return Program{}, err
	}
	var v Program
	if patch == nil {
		v, err = scan(tx.QueryRow(ctx, `INSERT INTO programs(name,name_search,description,status) VALUES($1,$2,$3,$4) RETURNING id,name,description,status,created_at,updated_at`, in.Name, strings.ToLower(in.Name), in.Description, in.Status))
	} else {
		v, err = scan(tx.QueryRow(ctx, `UPDATE programs SET name=$2,name_search=$3,description=$4,status=$5,updated_at=now() WHERE id=$1 RETURNING id,name,description,status,created_at,updated_at`, id, in.Name, strings.ToLower(in.Name), in.Description, in.Status))
	}
	if err != nil {
		return Program{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "program", v.ID, database.ChangedFields(old, v, "name", "description", "status")...); err != nil {
		return Program{}, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, f Filter) (Page, error) {
	result := Page{Items: []Program{}, Page: f.Page, PageSize: f.Size}
	tx, actor, err := r.begin(ctx, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all, own, assigned := flags(actor)
	where := ` WHERE ` + scopeSQL + ` AND ($5='' OR strpos(p.name_search,$5)>0) AND ($6='' OR p.status=$6)`
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM programs p`+where, all, own, assigned, actor.ID, f.Search, f.Status).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM programs p`+where+` ORDER BY p.name_search,p.id LIMIT $7 OFFSET $8`, all, own, assigned, actor.ID, f.Search, f.Status, f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v, e := scan(rows)
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
func scopedProgram(ctx context.Context, tx pgx.Tx, actor httpx.Actor, id int64) (Program, error) {
	all, own, assigned := flags(actor)
	return scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM programs p WHERE `+scopeSQL+` AND p.id=$5`, all, own, assigned, actor.ID, id))
}
func (r *Repository) Get(ctx context.Context, id int64) (Card, error) {
	result := Card{Modules: []Module{}, VersionsEnabled: true}
	tx, actor, err := r.begin(ctx, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	result.Program, err = scopedProgram(ctx, tx, actor, id)
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+moduleColumns+` FROM program_modules WHERE program_id=$1 ORDER BY position,id`, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v, e := scanModule(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Modules = append(result.Modules, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (r *Repository) GetModule(ctx context.Context, id, moduleID int64) (Module, error) {
	tx, actor, err := r.begin(ctx, false)
	if err != nil {
		return Module{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = scopedProgram(ctx, tx, actor, id); err != nil {
		return Module{}, err
	}
	v, err := scanModule(tx.QueryRow(ctx, `SELECT `+moduleColumns+` FROM program_modules WHERE program_id=$1 AND id=$2`, id, moduleID))
	if err != nil {
		return Module{}, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) SaveModule(ctx context.Context, id, moduleID int64, in ModuleInput, patch *ModuleUpdate) (Module, error) {
	tx, actor, err := r.begin(ctx, true)
	if err != nil {
		return Module{}, err
	}
	defer tx.Rollback(ctx)
	parent, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM programs p WHERE p.id=$1 FOR UPDATE`, id))
	if err != nil {
		return Module{}, err
	}
	if parent.Status == "archived" {
		return Module{}, httpx.E(409, "program_archived", "Для изменения модулей сначала восстановите программу из архива.")
	}
	action := "module.created"
	var old Module
	if patch != nil {
		old, err = scanModule(tx.QueryRow(ctx, `SELECT `+moduleColumns+` FROM program_modules WHERE program_id=$1 AND id=$2 FOR UPDATE`, id, moduleID))
		if err != nil {
			return Module{}, err
		}
		if !patch.Name.Set && !patch.Description.Set && !patch.Position.Set {
			return Module{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		in = ModuleInput{Name: old.Name, Description: old.Description, Position: old.Position}
		if err = patchText(patch.Name, &in.Name, true, "name"); err != nil {
			return Module{}, err
		}
		if err = patchText(patch.Description, &in.Description, false, "description"); err != nil {
			return Module{}, err
		}
		if patch.Position.Set {
			if patch.Position.Value == nil {
				return Module{}, httpx.Invalid("position", "Порядковый номер обязателен.")
			}
			in.Position = *patch.Position.Value
		}
		action = "module.updated"
	}
	if err = normalizeModule(&in); err != nil {
		return Module{}, err
	}
	var v Module
	if patch == nil {
		v, err = scanModule(tx.QueryRow(ctx, `INSERT INTO program_modules(program_id,name,description,position) VALUES($1,$2,$3,$4) RETURNING `+moduleColumns, id, in.Name, in.Description, in.Position))
	} else {
		v, err = scanModule(tx.QueryRow(ctx, `UPDATE program_modules SET name=$3,description=$4,position=$5,updated_at=now() WHERE program_id=$1 AND id=$2 RETURNING `+moduleColumns, id, moduleID, in.Name, in.Description, in.Position))
	}
	if err != nil {
		return Module{}, err
	}
	// Mark the containing card as changed as well as its module.
	if _, err = tx.Exec(ctx, `UPDATE programs SET updated_at=now() WHERE id=$1`, id); err != nil {
		return Module{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "module", v.ID, database.ChangedFields(old, v, "program_id", "name", "description", "position")...); err != nil {
		return Module{}, err
	}
	return v, tx.Commit(ctx)
}

// RequireActive is used inside a future group assignment transaction. The database
// trigger additionally enforces the same invariant for every SQL writer.
func RequireActive(ctx context.Context, tx pgx.Tx, id int64) (Program, error) {
	v, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM programs p WHERE p.id=$1 FOR SHARE`, id))
	if err != nil {
		return Program{}, translate(err)
	}
	if v.Status != "active" {
		return Program{}, httpx.E(409, "program_not_active", "Группу можно связать только с активной программой.")
	}
	return v, nil
}

// RequireGroupModule validates the group relationship loaded by the server; a
// client-supplied program ID is never trusted. Composite foreign keys also enforce it.
func RequireGroupModule(ctx context.Context, tx pgx.Tx, groupID int64, moduleID *int64) error {
	var programID int64
	if err := tx.QueryRow(ctx, `SELECT program_id FROM groups WHERE id=$1 FOR SHARE`, groupID).Scan(&programID); err != nil {
		return translate(err)
	}
	if moduleID == nil {
		return nil
	}
	var valid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM program_modules WHERE id=$1 AND program_id=$2)`, *moduleID, programID).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return httpx.Invalid("module_id", "Выберите модуль программы этой группы.")
	}
	return nil
}
