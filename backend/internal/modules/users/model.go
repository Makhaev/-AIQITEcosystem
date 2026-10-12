package users

import (
	"github.com/jackc/pgx/v5"
	"time"
)

type User struct {
	ID                 int64     `json:"id"`
	Email              string    `json:"email"`
	Name               string    `json:"name"`
	Roles              []string  `json:"roles"`
	Blocked            bool      `json:"blocked"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

const Columns = `u.id,u.email,u.name,ARRAY(SELECT role FROM user_roles WHERE user_id=u.id ORDER BY role),u.blocked,u.must_change_password,u.created_at,u.updated_at`

func Scan(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Roles, &u.Blocked, &u.MustChangePassword, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

type CreateInput struct {
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Password string   `json:"password"`
	Roles    []string `json:"roles"`
}
type UpdateInput struct {
	Name     *string  `json:"name"`
	Roles    []string `json:"roles"`
	Blocked  *bool    `json:"blocked"`
	Password *string  `json:"password"`
}
type Page struct {
	Items    []User `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
