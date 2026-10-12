package groups

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type Group struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	ProgramID   int64     `json:"program_id"`
	ProgramName string    `json:"program_name"`
	TeacherID   *int64    `json:"teacher_id"`
	TeacherName *string   `json:"teacher_name"`
	Capacity    int       `json:"capacity"`
	Status      string    `json:"status"`
	ActiveCount int       `json:"active_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Enrollment struct {
	ID          int64     `json:"id"`
	GroupID     int64     `json:"group_id"`
	StudentID   int64     `json:"student_id"`
	StudentName string    `json:"student_name"`
	StartsOn    string    `json:"starts_on"`
	EndsOn      *string   `json:"ends_on"`
	CreatedAt   time.Time `json:"created_at"`
}
type Card struct {
	Group
	Enrollments []Enrollment `json:"enrollments"`
}
type CreateInput struct {
	Name      string `json:"name"`
	ProgramID int64  `json:"program_id"`
	TeacherID *int64 `json:"teacher_id"`
	Capacity  int    `json:"capacity"`
	Status    string `json:"status"`
}
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (f *Optional[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	return json.Unmarshal(data, &f.Value)
}

type UpdateInput struct {
	Name      Optional[string] `json:"name"`
	ProgramID Optional[int64]  `json:"program_id"`
	TeacherID Optional[int64]  `json:"teacher_id"`
	Capacity  Optional[int]    `json:"capacity"`
	Status    Optional[string] `json:"status"`
}
type EnrollInput struct {
	StudentID int64  `json:"student_id"`
	StartsOn  string `json:"starts_on"`
}
type CloseInput struct {
	EndsOn string `json:"ends_on"`
}
type TransferInput struct {
	TargetGroupID int64  `json:"target_group_id"`
	TransfersOn   string `json:"transfers_on"`
}
type TransferResult struct {
	Previous   Enrollment `json:"previous"`
	Enrollment Enrollment `json:"enrollment"`
}
type Filter struct {
	Page, Size     int
	Search, Status string
}
type Page struct {
	Items    []Group `json:"items"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

const columns = `g.id,g.name,g.program_id,p.name,g.teacher_id,t.name,g.capacity,g.status,(SELECT count(*) FROM group_enrollments e WHERE e.group_id=g.id AND e.ends_on IS NULL),g.created_at,g.updated_at`
const fromSQL = ` FROM groups g JOIN programs p ON p.id=g.program_id LEFT JOIN teachers t ON t.id=g.teacher_id`
const enrollmentColumns = `e.id,e.group_id,e.student_id,s.name,to_char(e.starts_on,'YYYY-MM-DD'),to_char(e.ends_on,'YYYY-MM-DD'),e.created_at`

func scan(row pgx.Row) (Group, error) {
	var v Group
	err := row.Scan(&v.ID, &v.Name, &v.ProgramID, &v.ProgramName, &v.TeacherID, &v.TeacherName, &v.Capacity, &v.Status, &v.ActiveCount, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func scanEnrollment(row pgx.Row) (Enrollment, error) {
	var v Enrollment
	err := row.Scan(&v.ID, &v.GroupID, &v.StudentID, &v.StudentName, &v.StartsOn, &v.EndsOn, &v.CreatedAt)
	return v, err
}
