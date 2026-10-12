package lessons

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type Lesson struct {
	ID                  int64      `json:"id"`
	GroupID             int64      `json:"group_id"`
	GroupName           string     `json:"group_name"`
	ProgramID           int64      `json:"program_id"`
	ModuleID            *int64     `json:"module_id"`
	ModuleName          *string    `json:"module_name"`
	Topic               string     `json:"topic"`
	TeacherID           int64      `json:"teacher_id"`
	TeacherName         string     `json:"teacher_name"`
	TeacherUserID       *int64     `json:"teacher_user_id"`
	StartsAt            time.Time  `json:"starts_at"`
	EndsAt              time.Time  `json:"ends_at"`
	Location            string     `json:"location"`
	Status              string     `json:"status"`
	RosterInitializedAt *time.Time `json:"roster_initialized_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}
type Input struct {
	GroupID   int64     `json:"group_id"`
	ModuleID  *int64    `json:"module_id"`
	Topic     string    `json:"topic"`
	TeacherID int64     `json:"teacher_id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	Location  string    `json:"location"`
	Status    string    `json:"status"`
}
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (f *Optional[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	return json.Unmarshal(data, &f.Value)
}

type Update struct {
	GroupID   Optional[int64]     `json:"group_id"`
	ModuleID  Optional[int64]     `json:"module_id"`
	Topic     Optional[string]    `json:"topic"`
	TeacherID Optional[int64]     `json:"teacher_id"`
	StartsAt  Optional[time.Time] `json:"starts_at"`
	EndsAt    Optional[time.Time] `json:"ends_at"`
	Location  Optional[string]    `json:"location"`
	Status    Optional[string]    `json:"status"`
}
type Filter struct {
	Page, Size                       int
	Search, Status, DateFrom, DateTo string
	GroupID, TeacherID               int64
}
type Page struct {
	Items    []Lesson `json:"items"`
	Total    int      `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}
type Participant struct {
	ID           int64  `json:"id"`
	EnrollmentID int64  `json:"enrollment_id"`
	StudentID    int64  `json:"student_id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Comment      string `json:"comment"`
}
type Journal struct {
	LessonID      int64         `json:"lesson_id"`
	LessonDate    string        `json:"lesson_date"`
	InitializedAt *time.Time    `json:"initialized_at"`
	Items         []Participant `json:"items"`
}
type RosterInput struct {
	EnrollmentIDs []int64 `json:"enrollment_ids"`
	Reason        string  `json:"reason"`
}
type Mark struct {
	StudentID int64  `json:"student_id"`
	Status    string `json:"status"`
	Comment   string `json:"comment"`
}
type MarkInput struct {
	Items []Mark `json:"items"`
}

const columns = `l.id,l.group_id,g.name,l.program_id,l.module_id,m.name,l.topic,l.teacher_id,t.name,t.user_id,l.starts_at,l.ends_at,l.location,l.status,l.roster_initialized_at,l.created_at,l.updated_at`
const fromSQL = ` FROM lessons l JOIN groups g ON g.id=l.group_id JOIN teachers t ON t.id=l.teacher_id LEFT JOIN program_modules m ON m.id=l.module_id`

func scan(row pgx.Row) (Lesson, error) {
	var v Lesson
	err := row.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.ProgramID, &v.ModuleID, &v.ModuleName, &v.Topic, &v.TeacherID, &v.TeacherName, &v.TeacherUserID, &v.StartsAt, &v.EndsAt, &v.Location, &v.Status, &v.RosterInitializedAt, &v.CreatedAt, &v.UpdatedAt)
	v.StartsAt = v.StartsAt.UTC()
	v.EndsAt = v.EndsAt.UTC()
	return v, err
}
