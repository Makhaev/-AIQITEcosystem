package teachers

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type Teacher struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Phone          string    `json:"phone"`
	Email          string    `json:"email"`
	Specialization string    `json:"specialization"`
	Status         string    `json:"status"`
	UserID         *int64    `json:"user_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

const columns = `t.id,t.name,t.phone,t.email,t.specialization,t.status,t.user_id,t.created_at,t.updated_at`

func scan(row pgx.Row) (Teacher, error) {
	var value Teacher
	err := row.Scan(&value.ID, &value.Name, &value.Phone, &value.Email, &value.Specialization, &value.Status, &value.UserID, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}

type CreateInput struct {
	Name           string `json:"name"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Specialization string `json:"specialization"`
	Status         string `json:"status"`
	UserID         *int64 `json:"user_id"`
}

// Optional distinguishes an omitted PATCH field from explicit null (unlink/clear).
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (field *Optional[T]) UnmarshalJSON(data []byte) error {
	field.Set = true
	return json.Unmarshal(data, &field.Value)
}

type UpdateInput struct {
	Name           Optional[string] `json:"name"`
	Phone          Optional[string] `json:"phone"`
	Email          Optional[string] `json:"email"`
	Specialization Optional[string] `json:"specialization"`
	Status         Optional[string] `json:"status"`
	UserID         Optional[int64]  `json:"user_id"`
}
type Filter struct {
	Page, Size     int
	Search, Status string
}
type Page struct {
	Items    []Teacher `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}
