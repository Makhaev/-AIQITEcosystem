package invitations

import "time"

type Invitation struct {
	ID        int64      `json:"id"`
	Email     string     `json:"email"`
	Role      string     `json:"role"`
	CardID    int64      `json:"card_id"`
	CreatedBy int64      `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at"`
	RevokedAt *time.Time `json:"revoked_at"`
	Status    string     `json:"status"`
}
type Input struct {
	Email  string `json:"email"`
	Role   string `json:"role"`
	CardID int64  `json:"card_id"`
}
type TokenInput struct {
	Token string `json:"token"`
}
type AcceptInput struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}
type Preview struct {
	Email         string    `json:"email"`
	Role          string    `json:"role"`
	ExpiresAt     time.Time `json:"expires_at"`
	RequiresLogin bool      `json:"requires_login"`
}
type Result struct {
	Role           string `json:"role"`
	CardID         int64  `json:"card_id"`
	CreatedAccount bool   `json:"created_account"`
}
type Link struct {
	Role   string `json:"role"`
	CardID int64  `json:"card_id"`
	Name   string `json:"name"`
}
type Page struct {
	Items    []Invitation `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}
