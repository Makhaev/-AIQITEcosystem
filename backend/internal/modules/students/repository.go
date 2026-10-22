package students

import (
	"aiq.local/core/internal/domain/people"
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

func scanStudent(row pgx.Row) (Student, error) {
	var v Student
	err := row.Scan(&v.ID, &v.Name, &v.BirthDate, &v.Status, &v.CuratorID, &v.CuratorName, &v.CreatedAt, &v.UpdatedAt)
	return v, err
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
	var old Student
	if patch != nil {
		old, err = scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+studentFrom+` WHERE s.id=$1 FOR UPDATE OF s`, id))
		if err != nil {
			return Student{}, err
		}
	}
	in, action, err := prepareStudent(old, in, patch)
	if err != nil {
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
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "student", id, database.ChangedFields(old, value, "name", "birth_date", "status", "curator_id")...); err != nil {
		return Student{}, err
	}
	return value, tx.Commit(ctx)
}

func (r *Repository) Students(ctx context.Context, f Filter) (Page[StudentResponse], error) {
	result := Page[StudentResponse]{Items: []StudentResponse{}, Page: f.Page, PageSize: f.Size}
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
		full := access.StudentAllowed(actor, access.StudentLinks{CuratorID: v.CuratorID}, true)
		result.Items = append(result.Items, studentResponse(v, full))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (r *Repository) Student(ctx context.Context, id int64) (StudentCardResponse, error) {
	result := StudentCardResponse{}
	tx, actor, err := r.begin(ctx, access.Students, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	scope := access.ScopeFor(actor, access.Students, access.Read)
	student, err := scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+studentFrom+` WHERE `+studentVisible+` AND s.id=$5`, scope == access.All, scope&access.Assigned != 0, actor.ID, scope&access.Own != 0, id))
	if err != nil {
		return result, err
	}
	full := access.StudentAllowed(actor, access.StudentLinks{CuratorID: student.CuratorID}, true)
	result.StudentResponse = studentResponse(student, full)
	if full {
		parents, e := studentParents(ctx, tx, id)
		if e != nil {
			return result, e
		}
		result.StudentFamilyFields = &StudentFamilyFields{Parents: parents}
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
	var old Link
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
		old = Link{StudentID: studentID, ParentID: in.ParentID, Relationship: in.Relationship, IsPrimary: in.IsPrimary}
		if err = people.PatchText(patch.Relationship, &in.Relationship, true, "relationship"); err != nil {
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
	if in.Relationship, err = people.Relationship(in.Relationship); err != nil {
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
	value := Link{StudentID: studentID, ParentID: in.ParentID, Relationship: in.Relationship, IsPrimary: in.IsPrimary}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "student", studentID, database.ChangedFields(old, value, "parent_id", "relationship", "is_primary")...); err != nil {
		return Link{}, err
	}
	return Link{StudentID: studentID, ParentID: in.ParentID, Relationship: in.Relationship, IsPrimary: in.IsPrimary}, tx.Commit(ctx)
}
