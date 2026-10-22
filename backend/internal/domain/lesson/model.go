package lesson

import (
	"encoding/json"
	"time"
)

type Lesson struct {
	ID                  int64           `json:"id"`
	GroupID             int64           `json:"group_id"`
	GroupName           string          `json:"group_name"`
	ProgramID           int64           `json:"program_id"`
	ProgramVersionID    int64           `json:"program_version_id"`
	VersionModuleKey    string          `json:"version_module_key"`
	TemplateKey         string          `json:"template_key"`
	PlanSnapshot        json.RawMessage `json:"plan_snapshot"`
	ProgramName         string          `json:"program_name"`
	ModuleID            *int64          `json:"module_id"`
	ModuleName          *string         `json:"module_name"`
	Topic               string          `json:"topic"`
	TeacherID           int64           `json:"teacher_id"`
	TeacherName         string          `json:"teacher_name"`
	TeacherUserID       *int64          `json:"teacher_user_id"`
	StartsAt            time.Time       `json:"starts_at"`
	EndsAt              time.Time       `json:"ends_at"`
	Location            string          `json:"location"`
	Status              string          `json:"status"`
	RosterInitializedAt *time.Time      `json:"roster_initialized_at"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	SnapshotState       string          `json:"snapshot_state"`
	SnapshotCapturedAt  *time.Time      `json:"snapshot_captured_at"`
}

type Input struct {
	VersionModuleKey string    `json:"version_module_key"`
	TemplateKey      string    `json:"template_key"`
	GroupID          int64     `json:"group_id"`
	ModuleID         *int64    `json:"module_id"`
	Topic            string    `json:"topic"`
	TeacherID        int64     `json:"teacher_id"`
	StartsAt         time.Time `json:"starts_at"`
	EndsAt           time.Time `json:"ends_at"`
	Location         string    `json:"location"`
	Status           string    `json:"status"`
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
	VersionModuleKey Optional[string]    `json:"version_module_key"`
	TemplateKey      Optional[string]    `json:"template_key"`
	GroupID          Optional[int64]     `json:"group_id"`
	ModuleID         Optional[int64]     `json:"module_id"`
	Topic            Optional[string]    `json:"topic"`
	TeacherID        Optional[int64]     `json:"teacher_id"`
	StartsAt         Optional[time.Time] `json:"starts_at"`
	EndsAt           Optional[time.Time] `json:"ends_at"`
	Location         Optional[string]    `json:"location"`
	Status           Optional[string]    `json:"status"`
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
