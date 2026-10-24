package files

import "time"

const MaxSize = 10 << 20

type File struct {
	ID        int64     `json:"id"`
	StudentID int64     `json:"student_id"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"`
	Size      int       `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
	CanManage bool      `json:"can_manage"`
	Data      []byte    `json:"-"`
}
type Input struct {
	StudentID int64  `json:"student_id"`
	Name      string `json:"name"`
	Data      []byte `json:"data"` // JSON base64; decoded bytes are validated server-side.
}
type Grant struct {
	UserID int64 `json:"user_id"`
	Allow  bool  `json:"allow"`
}
type Page struct {
	Items    []File `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
