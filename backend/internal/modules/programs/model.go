package programs

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type Program struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Module struct {
	ID          int64     `json:"id"`
	ProgramID   int64     `json:"program_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Card struct {
	Program
	VersionsEnabled bool     `json:"versions_enabled"`
	Modules         []Module `json:"modules"`
}
type CreateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (f *Optional[T]) UnmarshalJSON(data []byte) error {
	f.Set = true
	return json.Unmarshal(data, &f.Value)
}

type UpdateInput struct {
	Name        Optional[string] `json:"name"`
	Description Optional[string] `json:"description"`
	Status      Optional[string] `json:"status"`
}
type ModuleInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    int    `json:"position"`
}
type ModuleUpdate struct {
	Name        Optional[string] `json:"name"`
	Description Optional[string] `json:"description"`
	Position    Optional[int]    `json:"position"`
}
type Filter struct {
	Page, Size     int
	Search, Status string
}
type Page struct {
	Items    []Program `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

const columns = `p.id,p.name,p.description,p.status,p.created_at,p.updated_at`
const moduleColumns = `id,program_id,name,description,position,created_at,updated_at`

func scan(row pgx.Row) (Program, error) {
	var v Program
	e := row.Scan(&v.ID, &v.Name, &v.Description, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
func scanModule(row pgx.Row) (Module, error) {
	var v Module
	e := row.Scan(&v.ID, &v.ProgramID, &v.Name, &v.Description, &v.Position, &v.CreatedAt, &v.UpdatedAt)
	return v, e
}
