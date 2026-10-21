package task

import "errors"

// Errors returned by repository implementations and the service layer.
var (
	// ErrNotFound is returned when a task does not exist.
	ErrNotFound = errors.New("task not found")
	// ErrConflict is returned when a write collides with an existing task.
	ErrConflict = errors.New("task already exists")
	// ErrInvalid is returned when a value breaks a domain rule.
	ErrInvalid = errors.New("invalid task")
)
