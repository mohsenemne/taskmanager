package task

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Pagination policy shared by the service and the HTTP layer, so both agree on
// what an absent or oversized page means.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// Cursor is a position in the newest-first ordering of tasks. It is the
// ordering key, not an offset: two concurrent writes cannot make a client skip
// or repeat a row.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// Filter narrows a task listing. Zero values mean "no filter".
type Filter struct {
	Status   Status
	Assignee *string
	Limit    int
	// After continues a listing after the given position. Nil starts at the
	// newest task.
	After *Cursor
}

// Page is one slice of a listing, plus enough information to ask for the next.
type Page struct {
	Items []Task
	// HasNext reports whether at least one more task follows this page.
	HasNext bool
	// Next is the position to pass as Filter.After for the following page.
	// It is nil when HasNext is false.
	Next *Cursor
}

// Repository is the storage contract. Implementations own persistence details;
// the service layer only sees this interface.
type Repository interface {
	// Create stores t and returns it with identity and timestamps filled in.
	Create(ctx context.Context, t Task) (Task, error)
	// Get returns the task with the given id, or ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (Task, error)
	// List returns up to Filter.Limit tasks matching the filter, newest first.
	List(ctx context.Context, f Filter) ([]Task, error)
	// Update replaces the mutable fields of t and returns the stored row.
	Update(ctx context.Context, t Task) (Task, error)
	// Delete removes the task with the given id, or returns ErrNotFound.
	Delete(ctx context.Context, id uuid.UUID) error
}
