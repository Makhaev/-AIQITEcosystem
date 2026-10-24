package learning

import (
	"aiq.local/core/internal/modules/files"
	"encoding/json"
	"time"
)

type Content struct {
	Title            string `json:"title"`
	Fact             string `json:"fact,omitempty"`
	Interpretation   string `json:"interpretation,omitempty"`
	Context          string `json:"context,omitempty"`
	OccurredOn       string `json:"occurred_on,omitempty"`
	StartsOn         string `json:"starts_on,omitempty"`
	EndsOn           string `json:"ends_on,omitempty"`
	DueOn            string `json:"due_on,omitempty"`
	ModelVersion     string `json:"model_version,omitempty"`
	SkillKey         string `json:"skill_key,omitempty"`
	SkillName        string `json:"skill_name,omitempty"`
	Instruction      string `json:"instruction,omitempty"`
	Criteria         string `json:"criteria,omitempty"`
	Format           string `json:"format,omitempty"`
	Answer           string `json:"answer,omitempty"`
	Channel          string `json:"channel,omitempty"`
	Sender           string `json:"sender,omitempty"`
	Contribution     string `json:"contribution,omitempty"`
	ProgramVersionID *int64 `json:"program_version_id,omitempty"`
	TemplateKey      string `json:"template_key,omitempty"`
	AllowLate        *bool  `json:"allow_late,omitempty"`
}
type Reference struct {
	ID       int64 `json:"id"`
	Revision int   `json:"revision"`
}
type Input struct {
	Kind       string      `json:"kind"`
	LessonID   *int64      `json:"lesson_id"`
	ParentID   *int64      `json:"parent_id"`
	ReviewerID *int64      `json:"reviewer_id"`
	Content    Content     `json:"content"`
	Sources    []Reference `json:"sources"`
	FileIDs    []int64     `json:"file_ids"`
	Revision   int         `json:"revision"`
	Reason     string      `json:"reason"`
}
type Record struct {
	ID               int64        `json:"id"`
	StudentID        int64        `json:"student_id"`
	Kind             string       `json:"kind"`
	Revision         int          `json:"revision"`
	State            string       `json:"state"`
	Published        bool         `json:"published"`
	CreatedAt        time.Time    `json:"created_at"`
	Content          Content      `json:"content"`
	Source           Source       `json:"source"`
	Sources          []Source     `json:"sources"`
	Files            []files.File `json:"files"`
	BasisUnavailable bool         `json:"basis_unavailable"`
	NeedsReview      bool         `json:"needs_review"`
	Actions          []string     `json:"actions"`
	ReviewerID       *int64       `json:"reviewer_id,omitempty"`
	AuthorID         int64        `json:"author_id"`
	ParentID         *int64       `json:"-"`
	ParentRevision   *int         `json:"-"`
	Parent           *Source      `json:"parent,omitempty"`
	Successor        *Source      `json:"successor,omitempty"`
	Scope            string       `json:"-"`
	LessonID         *int64       `json:"-"`
}
type Source struct {
	ID            int64  `json:"id"`
	Kind          string `json:"kind"`
	Revision      int    `json:"revision"`
	Title         string `json:"title"`
	Date          string `json:"date"`
	URL           string `json:"url"`
	OutsidePeriod bool   `json:"outside_period"`
}
type Decision struct {
	ID        int64     `json:"id"`
	Revision  int       `json:"revision"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	ActorID   int64     `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}
type Card struct {
	Record
	History []Decision `json:"history"`
}
type ActionInput struct {
	Revision int         `json:"revision"`
	Action   string      `json:"action"`
	Reason   string      `json:"reason"`
	Sources  []Reference `json:"sources"`
}
type Block struct {
	State     string   `json:"state"`
	CanCreate bool     `json:"can_create"`
	Items     []Record `json:"items"`
	Total     int      `json:"total"`
	Page      int      `json:"page"`
	PageSize  int      `json:"page_size"`
	From      string   `json:"from"`
	To        string   `json:"to"`
}
type Filter struct {
	Kind                           string
	Start, End                     time.Time
	Page                           int
	Current                        bool
	Work                           bool
	Status, ModelVersion, SkillKey string
}

func (b Block) MarshalJSON() ([]byte, error) {
	if b.State == "error" {
		return json.Marshal(struct {
			State string `json:"state"`
		}{State: b.State})
	}
	type plain Block
	return json.Marshal(plain(b))
}
