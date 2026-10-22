package curriculum

import "time"

// Document is one atomic revision. Keys survive cloning; array order is the
// teaching order, so there are no ambiguous duplicate position numbers.
type Document struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Sections    []Section `json:"sections"`
}
type Section struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Modules []Module `json:"modules"`
}
type Module struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Lessons     []Template `json:"lessons"`
}
type Template struct {
	Key             string `json:"key"`
	Topic           string `json:"topic"`
	Goals           string `json:"goals"`
	DurationMinutes int    `json:"duration_minutes"`
	Outline         string `json:"outline"`
	Exercises       string `json:"exercises"`
	Equipment       string `json:"equipment"`
	Materials       string `json:"materials"`
	Criteria        string `json:"criteria"`
}
type Version struct {
	ID              int64      `json:"id"`
	ProgramID       int64      `json:"program_id"`
	Number          int        `json:"number"`
	Status          string     `json:"status"`
	Document        Document   `json:"document"`
	Revision        int        `json:"revision"`
	CreatedBy       *int64     `json:"created_by"`
	SubmittedBy     *int64     `json:"submitted_by"`
	ApprovedBy      *int64     `json:"approved_by"`
	SourceVersionID *int64     `json:"source_version_id"`
	LegacyImport    bool       `json:"legacy_import"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	PublishedAt     *time.Time `json:"published_at"`
}
type Event struct {
	ID        int64     `json:"id"`
	ActorID   *int64    `json:"actor_id"`
	Action    string    `json:"action"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}
type Card struct {
	Version
	Events []Event `json:"events"`
}
type CreateInput struct {
	SourceVersionID *int64 `json:"source_version_id"`
}
type SaveInput struct {
	Revision int      `json:"revision"`
	Document Document `json:"document"`
}
type ActionInput struct {
	Revision int    `json:"revision"`
	Action   string `json:"action"`
	Comment  string `json:"comment"`
}
type VersionPage struct {
	Items []Version `json:"items"`
	Total int       `json:"total"`
}
