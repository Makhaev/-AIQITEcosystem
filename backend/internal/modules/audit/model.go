package audit

import "time"

type Entry struct {
	ID            int64     `json:"id"`
	OccurredAt    time.Time `json:"occurred_at"`
	ActorID       *int64    `json:"actor_id"`
	ActorName     *string   `json:"actor_name"`
	Action        string    `json:"action"`
	ObjectType    string    `json:"object_type"`
	ObjectID      *int64    `json:"object_id"`
	ChangedFields []string  `json:"changed_fields"`
	RequestID     string    `json:"request_id"`
}

type Page struct {
	Items    []Entry `json:"items"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

type Filter struct {
	Page, Size                    int
	From, To                      *time.Time
	ActorID, ObjectID             int64
	Action, ObjectType, RequestID string
}
