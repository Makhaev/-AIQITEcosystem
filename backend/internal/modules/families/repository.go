package families

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

const studentColumns = `s.id,s.name,s.birth_date::text,s.status,s.curator_id,u.name,s.created_at,s.updated_at`
const studentFrom = ` FROM students s LEFT JOIN users u ON u.id=s.curator_id`
const parentColumns = `p.id,p.name,p.phone,p.email,p.status,p.created_at,p.updated_at`

// Full family cards remain available only to All/Assigned scopes.
const studentScope = `($1 OR ($2 AND s.curator_id=$3))`
const parentScope = `($1 OR ($2 AND EXISTS(SELECT 1 FROM student_parents sp JOIN students s ON s.id=sp.student_id WHERE sp.parent_id=p.id AND s.curator_id=$3)))`

func scanStudent(row pgx.Row) (Student, error) {
	var v Student
	err := row.Scan(&v.ID, &v.Name, &v.BirthDate, &v.Status, &v.CuratorID, &v.CuratorName, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanParent(row pgx.Row) (Parent, error) {
	var v Parent
	err := row.Scan(&v.ID, &v.Name, &v.Phone, &v.Email, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scopes(actor httpx.Actor, resource access.Resource, action access.Action) (bool, bool) {
	s := access.ScopeFor(actor, resource, action)
	return s == access.All, s&access.Assigned != 0
}
func (r *Repository) begin(ctx context.Context, resource access.Resource, write bool) (pgx.Tx, httpx.Actor, error) {
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
			actor, err = access.Authorize(ctx, tx, resource, access.Manage)
		}
	} else {
		actor, err = access.AuthorizeRead(ctx, tx, resource)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}
func curator(ctx context.Context, tx pgx.Tx, id *int64) error {
	if id == nil {
		return nil
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN user_roles r ON r.user_id=u.id WHERE u.id=$1 AND NOT u.blocked AND r.role='curator')`, *id).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return httpx.Invalid("curator_id", "Выберите активного пользователя с ролью куратора.")
	}
	return nil
}
func (r *Repository) SaveStudent(ctx context.Context, id int64, in StudentInput, patch *StudentUpdate) (Student, error) {
	tx, actor, err := r.begin(ctx, access.Students, true)
	if err != nil {
		return Student{}, err
	}
	defer tx.Rollback(ctx)
	action := "student.created"
	var old Student
	if patch != nil {
		old, err = scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+studentFrom+` WHERE s.id=$1 FOR UPDATE OF s`, id))
		if err != nil {
			return Student{}, err
		}
		in = StudentInput{Name: old.Name, BirthDate: old.BirthDate, Status: old.Status, CuratorID: old.CuratorID}
		if !patch.Name.Set && !patch.BirthDate.Set && !patch.Status.Set && !patch.CuratorID.Set {
			return Student{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		if err = patchText(patch.Name, &in.Name, true, "name"); err != nil {
			return Student{}, err
		}
		if err = patchText(patch.Status, &in.Status, true, "status"); err != nil {
			return Student{}, err
		}
		if patch.BirthDate.Set {
			in.BirthDate = patch.BirthDate.Value
		}
		if patch.CuratorID.Set {
			in.CuratorID = patch.CuratorID.Value
		}
		action = "student.updated"
		if in.Status != old.Status {
			if in.Status == "archived" {
				action = "student.archived"
			} else {
				action = "student.restored"
			}
		}
	}
	if err = normalizeStudent(&in); err != nil {
		return Student{}, err
	}
	if patch == nil || patch.CuratorID.Set && (in.CuratorID != nil && (old.CuratorID == nil || *old.CuratorID != *in.CuratorID)) {
		if err = curator(ctx, tx, in.CuratorID); err != nil {
			return Student{}, err
		}
	}
	if patch == nil {
		err = tx.QueryRow(ctx, `INSERT INTO students(name,name_search,birth_date,status,curator_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, in.Name, strings.ToLower(in.Name), in.BirthDate, in.Status, in.CuratorID).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE students SET name=$2,name_search=$3,birth_date=$4,status=$5,curator_id=$6,updated_at=now() WHERE id=$1`, id, in.Name, strings.ToLower(in.Name), in.BirthDate, in.Status, in.CuratorID)
	}
	if err != nil {
		return Student{}, err
	}
	value, err := scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+studentFrom+` WHERE s.id=$1`, id))
	if err != nil {
		return Student{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "student", id); err != nil {
		return Student{}, err
	}
	return value, tx.Commit(ctx)
}
func (r *Repository) SaveParent(ctx context.Context, id int64, in ParentInput, patch *ParentUpdate) (Parent, error) {
	tx, actor, err := r.begin(ctx, access.Parents, true)
	if err != nil {
		return Parent{}, err
	}
	defer tx.Rollback(ctx)
	action := "parent.created"
	if patch != nil {
		old, err := scanParent(tx.QueryRow(ctx, `SELECT `+parentColumns+` FROM parents p WHERE p.id=$1 FOR UPDATE`, id))
		if err != nil {
			return Parent{}, err
		}
		in = ParentInput{Name: old.Name, Phone: old.Phone, Email: old.Email, Status: old.Status}
		if !patch.Name.Set && !patch.Phone.Set && !patch.Email.Set && !patch.Status.Set {
			return Parent{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		for _, f := range []struct {
			field    Optional[string]
			target   *string
			required bool
			key      string
		}{{patch.Name, &in.Name, true, "name"}, {patch.Phone, &in.Phone, false, "phone"}, {patch.Email, &in.Email, false, "email"}, {patch.Status, &in.Status, true, "status"}} {
			if err = patchText(f.field, f.target, f.required, f.key); err != nil {
				return Parent{}, err
			}
		}
		action = "parent.updated"
		if in.Status != old.Status {
			if in.Status == "archived" {
				action = "parent.archived"
			} else {
				action = "parent.restored"
			}
		}
	}
	if err = normalizeParent(&in); err != nil {
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
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "parent", value.ID); err != nil {
		return Parent{}, err
	}
	return value, tx.Commit(ctx)
}
func (r *Repository) Students(ctx context.Context, f Filter) (Page[Student], error) {
	result := Page[Student]{Items: []Student{}, Page: f.Page, PageSize: f.Size}
	tx, actor, err := r.begin(ctx, access.Students, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	scope := access.ScopeFor(actor, access.Students, access.Read)
	all, assigned, own := scope == access.All, scope&access.Assigned != 0, scope&access.Own != 0
	where := ` WHERE ` + studentVisible + ` AND ($5='' OR strpos(s.name_search,$5)>0) AND ($6='' OR s.status=$6)`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+studentFrom+where, all, assigned, actor.ID, own, f.Search, f.Status).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+studentColumns+studentFrom+where+` ORDER BY s.name_search,s.id LIMIT $7 OFFSET $8`, all, assigned, actor.ID, own, f.Search, f.Status, f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v, e := scanStudent(rows)
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
func (r *Repository) Student(ctx context.Context, id int64) (StudentCard, error) {
	result := StudentCard{Parents: []ParentLink{}}
	tx, actor, err := r.begin(ctx, access.Students, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all, assigned := scopes(actor, access.Students, access.ReadFull)
	result.Student, err = scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+studentFrom+` WHERE `+studentScope+` AND s.id=$4`, all, assigned, actor.ID, id))
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+parentColumns+`,sp.relationship,sp.is_primary FROM parents p JOIN student_parents sp ON sp.parent_id=p.id WHERE sp.student_id=$1 ORDER BY sp.is_primary DESC,p.name_search,p.id`, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v := ParentLink{Link: Link{StudentID: id}}
		err = rows.Scan(&v.ID, &v.Name, &v.Phone, &v.Email, &v.Status, &v.CreatedAt, &v.UpdatedAt, &v.Relationship, &v.IsPrimary)
		if err != nil {
			rows.Close()
			return result, err
		}
		v.ParentID = v.ID
		result.Parents = append(result.Parents, v)
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
func (r *Repository) SaveLink(ctx context.Context, studentID int64, in LinkInput, patch *LinkUpdate) (Link, error) {
	tx, actor, err := r.begin(ctx, access.Students, true)
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback(ctx)
	var studentStatus, parentStatus string
	if err = tx.QueryRow(ctx, `SELECT status FROM students WHERE id=$1 FOR UPDATE`, studentID).Scan(&studentStatus); err != nil {
		return Link{}, err
	}
	if err = tx.QueryRow(ctx, `SELECT status FROM parents WHERE id=$1 FOR UPDATE`, in.ParentID).Scan(&parentStatus); err != nil {
		return Link{}, err
	}
	action := "student.parent_linked"
	if patch == nil {
		if studentStatus != "active" || parentStatus != "active" {
			return Link{}, httpx.E(409, "card_archived", "Новую связь можно создать только между активными карточками.")
		}
	} else {
		if !patch.Relationship.Set && !patch.IsPrimary.Set {
			return Link{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		if err = tx.QueryRow(ctx, `SELECT relationship,is_primary FROM student_parents WHERE student_id=$1 AND parent_id=$2 FOR UPDATE`, studentID, in.ParentID).Scan(&in.Relationship, &in.IsPrimary); err != nil {
			return Link{}, err
		}
		if err = patchText(patch.Relationship, &in.Relationship, true, "relationship"); err != nil {
			return Link{}, err
		}
		if patch.IsPrimary.Set {
			if patch.IsPrimary.Value == nil {
				return Link{}, httpx.Invalid("is_primary", "Признак основного контакта обязателен.")
			}
			in.IsPrimary = *patch.IsPrimary.Value
		}
		action = "student.parent_link_updated"
	}
	if in.Relationship, err = relationship(in.Relationship); err != nil {
		return Link{}, err
	}
	if patch == nil {
		_, err = tx.Exec(ctx, `INSERT INTO student_parents(student_id,parent_id,relationship,is_primary) VALUES($1,$2,$3,$4)`, studentID, in.ParentID, in.Relationship, in.IsPrimary)
	} else {
		_, err = tx.Exec(ctx, `UPDATE student_parents SET relationship=$3,is_primary=$4,updated_at=now() WHERE student_id=$1 AND parent_id=$2`, studentID, in.ParentID, in.Relationship, in.IsPrimary)
	}
	if err != nil {
		return Link{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "student", studentID); err != nil {
		return Link{}, err
	}
	return Link{StudentID: studentID, ParentID: in.ParentID, Relationship: in.Relationship, IsPrimary: in.IsPrimary}, tx.Commit(ctx)
}
