package events

import "time"

type Input struct {
	Title                string     `json:"title"`
	Type                 string     `json:"type"`
	Description          string     `json:"description"`
	Organizer            string     `json:"organizer"`
	ResponsibleIDs       []int64    `json:"responsible_ids"`
	StartsAt             *time.Time `json:"starts_at"`
	EndsAt               *time.Time `json:"ends_at"`
	TimeZone             string     `json:"time_zone"`
	Location             string     `json:"location"`
	Capacity             int        `json:"capacity"`
	RegistrationStartsAt *time.Time `json:"registration_starts_at"`
	RegistrationEndsAt   *time.Time `json:"registration_ends_at"`
	Conditions           string     `json:"conditions"`
	RequiredDocuments    string     `json:"required_documents"`
	Revision             int64      `json:"revision"`
}
type Person struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}
type Event struct {
	Input
	ID                   int64      `json:"id"`
	Status               string     `json:"status"`
	Responsibles         []Person   `json:"responsibles"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	PublishedAt          *time.Time `json:"published_at"`
	RegistrationClosedAt *time.Time `json:"registration_closed_at"`
	CompletedAt          *time.Time `json:"completed_at"`
	RegistrationOpen     bool       `json:"registration_open"`
	CanEdit              bool       `json:"can_edit"`
	AllowedActions       []string   `json:"allowed_actions"`
}
type Filter struct {
	Size           int
	Page           int
	Search, Status string
}
type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}
type ActionInput struct {
	Action   string `json:"action"`
	Revision int64  `json:"revision"`
}
