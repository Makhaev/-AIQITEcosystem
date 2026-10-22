package attendance

import (
	"time"
)

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
	HasMarks      bool          `json:"has_marks"`
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
