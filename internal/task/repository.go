package task

import (
	"context"

	"github.com/google/uuid"
)

// Pagination policy shared by the service and the HTTP layer, so both agree on
// what an absent or oversized page means.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// Filter narrows a task listing. Zero values mean "no filter".
type Filter struct {
	Status   Status
	Assignee *string
	Limit    int
	Offset   int
}

// Repository is the storage contract. Implementations own persistence details;
// the service layer only sees this interface.
type Repository interface {
	// Create stores t and returns it with identity and timestamps filled in.
	Create(ctx context.Context, t Task) (Task, error)
	// Get returns the task with the given id, or ErrNotFound.
	Get(ctx context.Context, id uuid.UUID) (Task, error)
	// List returns one page of tasks matching f and the total number of matches.
	List(ctx context.Context, f Filter) (tasks []Task, total int, err error)
	// Update replaces the mutable fields of t and returns the stored row.
	Update(ctx context.Context, t Task) (Task, error)
	// Delete removes the task with the given id, or returns ErrNotFound.
	Delete(ctx context.Context, id uuid.UUID) error
}
