package tasks

import (
	"encoding/json"
	"time"
)

type Task struct {
	ID           int64      `json:"id"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	CreatorID    int64      `json:"creator_id"`
	CreatorName  string     `json:"creator_name"`
	AssigneeID   int64      `json:"assignee_id"`
	AssigneeName string     `json:"assignee_name"`
	DueAt        time.Time  `json:"due_at"`
	Status       string     `json:"status"`
	Overdue      bool       `json:"overdue"`
	ObjectType   *string    `json:"object_type"`
	ObjectID     *int64     `json:"object_id"`
	ObjectName   *string    `json:"object_name"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ClosedAt     *time.Time `json:"closed_at"`
}
type Input struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	AssigneeID  int64     `json:"assignee_id"`
	DueAt       time.Time `json:"due_at"`
	Status      string    `json:"status"`
	ObjectType  *string   `json:"object_type"`
	ObjectID    *int64    `json:"object_id"`
}
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

type Update struct {
	Title       Optional[string]    `json:"title"`
	Description Optional[string]    `json:"description"`
	AssigneeID  Optional[int64]     `json:"assignee_id"`
	DueAt       Optional[time.Time] `json:"due_at"`
	Status      Optional[string]    `json:"status"`
	ObjectType  Optional[string]    `json:"object_type"`
	ObjectID    Optional[int64]     `json:"object_id"`
}
type Filter struct {
	Page, Size     int
	Search, Status string
	AssigneeID     int64
	Overdue        *bool
}
type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}
type Assignee struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Blocked bool   `json:"blocked"`
}
