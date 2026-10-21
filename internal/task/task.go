// Package task holds the task domain model and the storage contract it is
// persisted through.
package task

import (
	"time"

	"github.com/google/uuid"
)

// Task is a single unit of work.
type Task struct {
	ID          uuid.UUID
	Title       string
	Description string
	Status      Status
	Assignee    *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
