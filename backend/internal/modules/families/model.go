// Package families owns student/parent cards and their many-to-many relationship.
// Cards are independent of login accounts; equal names never identify a person.
package families

import (
	"encoding/json"
	"time"
)

type Optional[T any] struct {
	Set   bool
	Value *T
}

func (f *Optional[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	return json.Unmarshal(data, &f.Value)
}

type Student struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	BirthDate   *string   `json:"birth_date"`
	Status      string    `json:"status"`
	CuratorID   *int64    `json:"curator_id"`
	CuratorName *string   `json:"curator_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Parent struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type StudentInput struct {
	Name      string  `json:"name"`
	BirthDate *string `json:"birth_date"`
	Status    string  `json:"status"`
	CuratorID *int64  `json:"curator_id"`
}
type StudentUpdate struct {
	Name      Optional[string] `json:"name"`
	BirthDate Optional[string] `json:"birth_date"`
	Status    Optional[string] `json:"status"`
	CuratorID Optional[int64]  `json:"curator_id"`
}
type ParentInput struct {
	Name   string `json:"name"`
	Phone  string `json:"phone"`
	Email  string `json:"email"`
	Status string `json:"status"`
}
type ParentUpdate struct {
	Name   Optional[string] `json:"name"`
	Phone  Optional[string] `json:"phone"`
	Email  Optional[string] `json:"email"`
	Status Optional[string] `json:"status"`
}
type LinkInput struct {
	ParentID     int64  `json:"parent_id"`
	Relationship string `json:"relationship"`
	IsPrimary    bool   `json:"is_primary"`
}
type LinkUpdate struct {
	Relationship Optional[string] `json:"relationship"`
	IsPrimary    Optional[bool]   `json:"is_primary"`
}
type Link struct {
	StudentID    int64  `json:"student_id"`
	ParentID     int64  `json:"parent_id"`
	Relationship string `json:"relationship"`
	IsPrimary    bool   `json:"is_primary"`
}
type ParentLink struct {
	Parent
	Link
}
type StudentLink struct {
	Student
	Link
}
type StudentCard struct {
	Student
	Parents []ParentLink `json:"parents"`
}
type ParentCard struct {
	Parent
	Children []StudentLink `json:"children"`
}
type Filter struct {
	Page, Size     int
	Search, Status string
}
type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}
