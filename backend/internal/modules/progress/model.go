package progress

import "time"

type ReportInput struct {
	Revision       int    `json:"revision"`
	State          string `json:"state"`
	Outcome        string `json:"outcome"`
	PlanResult     string `json:"plan_result"`
	Comment        string `json:"comment"`
	ConfirmElement bool   `json:"confirm_element"`
	Reason         string `json:"reason"`
}
type Report struct {
	ReportInput
	LessonID         int64     `json:"lesson_id"`
	PlanAssignmentID *int64    `json:"plan_assignment_id"`
	TemplateKey      string    `json:"template_key"`
	ActorID          int64     `json:"actor_id"`
	CreatedAt        time.Time `json:"created_at"`
}
type Reports struct {
	Current *Report  `json:"current"`
	History []Report `json:"history"`
	CanEdit bool     `json:"can_edit"`
}
type Element struct {
	Key       string   `json:"key"`
	Title     string   `json:"title"`
	Weight    int      `json:"weight"`
	Completed bool     `json:"completed"`
	Sources   []Report `json:"sources"`
}
type Overview struct {
	GroupID         int64      `json:"group_id"`
	AssignmentID    int64      `json:"assignment_id,omitempty"`
	VersionID       int64      `json:"version_id,omitempty"`
	VersionNumber   int        `json:"version_number,omitempty"`
	AssignedAt      *time.Time `json:"assigned_at,omitempty"`
	State           string     `json:"state"`
	Percent         *float64   `json:"percent"`
	TotalWeight     int        `json:"total_weight,omitempty"`
	CompletedWeight *int       `json:"completed_weight"`
	Elements        []Element  `json:"elements"`
}
