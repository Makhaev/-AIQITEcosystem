package family

import "time"

type Student struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Own  bool   `json:"own"`
}
type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}
type File struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Size      int    `json:"size"`
	URL       string `json:"url"`
	Data      []byte `json:"-"`
}
type Record struct {
	CanSubmit       bool           `json:"can_submit"`
	SubmissionIssue string         `json:"submission_issue,omitempty"`
	Feedback        string         `json:"feedback,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	ID              int64          `json:"id"`
	StudentID       int64          `json:"student_id"`
	Kind            string         `json:"kind"`
	State           string         `json:"state"`
	Revision        int            `json:"revision"`
	Content         map[string]any `json:"content"`
	Files           []File         `json:"files"`
	Sources         []Source       `json:"sources"`
	Parent          *Source        `json:"parent,omitempty"`
}
type Source struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}
type Lesson struct {
	ID         int64     `json:"id"`
	Topic      string    `json:"topic"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Status     string    `json:"status"`
	Attendance string    `json:"attendance"`
}
type Report struct {
	Version          int              `json:"version"`
	State            string           `json:"state"`
	PublishedVersion *int             `json:"published_version,omitempty"`
	ApprovedBy       *int64           `json:"approved_by,omitempty"`
	ApprovedAt       *time.Time       `json:"approved_at,omitempty"`
	Actions          []string         `json:"actions,omitempty"`
	Decisions        []ReportDecision `json:"decisions,omitempty"`
	Legacy           bool             `json:"legacy,omitempty"`
	Snapshot         *ReportSnapshot  `json:"snapshot,omitempty"`
	NextFocus        string           `json:"next_focus"`
	ReviewerID       *int64           `json:"reviewer_id"`
	ReviewerName     string           `json:"reviewer_name"`
	AuthorID         int64            `json:"author_id"`
	AuthorName       string           `json:"author_name"`
	UpdatedBy        int64            `json:"updated_by"`
	CreatedAt        *time.Time       `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	PublishedAt      *time.Time       `json:"published_at"`
	WithdrawnAt      *time.Time       `json:"withdrawn_at"`
	ID               int64            `json:"id"`
	StudentID        int64            `json:"student_id"`
	Title            string           `json:"title"`
	Body             string           `json:"body"`
	Revision         int              `json:"revision"`
	Published        bool             `json:"published"`
	Files            []File           `json:"files"`
}
type ReportInput struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	NextFocus  string  `json:"next_focus"`
	ReviewerID *int64  `json:"reviewer_id"`
	Generate   bool    `json:"generate"`
	Title      string  `json:"title"`
	Body       string  `json:"body"`
	Revision   int     `json:"revision"`
	FileIDs    []int64 `json:"file_ids"`
}
type Publication struct {
	Version   int    `json:"version"`
	Reason    string `json:"reason"`
	Revision  int    `json:"revision"`
	Published *bool  `json:"published"`
}
type ReportAction struct {
	Action   string `json:"action"`
	Version  int    `json:"version"`
	Revision int    `json:"revision"`
	Reason   string `json:"reason"`
}
type ReportDecision struct {
	Version   int       `json:"version"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	ActorID   int64     `json:"actor_id"`
	ActorName string    `json:"actor_name"`
	CreatedAt time.Time `json:"created_at"`
}
type ReportNotification struct {
	ReportID  int64     `json:"report_id"`
	Version   int       `json:"version"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
}
type Audience struct {
	Students bool `json:"students"`
	Parents  bool `json:"parents"`
	Revision int  `json:"revision"`
}
type LinkAccess struct {
	ParentID  int64  `json:"parent_id"`
	Name      string `json:"name"`
	Confirmed bool   `json:"confirmed"`
}
type Request struct {
	ID           int64            `json:"id"`
	StudentID    *int64           `json:"student_id"`
	StudentName  string           `json:"student_name"`
	AuthorID     int64            `json:"author_id"`
	AuthorName   string           `json:"author_name"`
	AssigneeID   *int64           `json:"assignee_id"`
	AssigneeName string           `json:"assignee_name"`
	Status       string           `json:"status"`
	CreatedAt    time.Time        `json:"created_at"`
	AnsweredAt   *time.Time       `json:"answered_at"`
	ClosedAt     *time.Time       `json:"closed_at"`
	Actions      []string         `json:"actions"`
	Messages     []RequestMessage `json:"messages,omitempty"`
	Events       []RequestEvent   `json:"events,omitempty"`
	Subject      string           `json:"subject"`
	Body         string           `json:"body"`
	Reply        string           `json:"reply"`
	Revision     int              `json:"revision"`
}
type RequestInput struct {
	StudentID *int64          `json:"student_id"`
	Subject   string          `json:"subject"`
	Body      string          `json:"body"`
	Files     []RequestUpload `json:"files"`
}
type RequestUpload struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
}
type RequestMessage struct {
	ID         int64      `json:"id"`
	AuthorID   *int64     `json:"author_id"`
	AuthorName string     `json:"author_name"`
	Body       string     `json:"body"`
	Kind       string     `json:"kind"`
	CreatedAt  *time.Time `json:"created_at"`
	Files      []File     `json:"files"`
}
type RequestEvent struct {
	Action     string     `json:"action"`
	From       string     `json:"from_status"`
	To         string     `json:"to_status"`
	ActorID    *int64     `json:"actor_id"`
	ActorName  string     `json:"actor_name"`
	AssigneeID *int64     `json:"assignee_id"`
	Reason     string     `json:"reason"`
	CreatedAt  *time.Time `json:"created_at"`
}
type RequestChange struct {
	Action     string          `json:"action"`
	Revision   int             `json:"revision"`
	AssigneeID *int64          `json:"assignee_id"`
	Body       string          `json:"body"`
	Reason     string          `json:"reason"`
	Files      []RequestUpload `json:"files"`
}
type ReplyInput struct {
	Reply    string `json:"reply"`
	Revision int    `json:"revision"`
}
type Filter struct {
	Size         int
	Status       string
	From         string
	To           string
	AssignmentID int64
	Page         int
	Search       string
	Kind         string
}
