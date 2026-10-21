package task

import "fmt"

// Status is the lifecycle state of a task.
type Status string

// Supported task statuses.
const (
	StatusTodo       Status = "todo"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
)

// Valid reports whether s is one of the supported statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusTodo, StatusInProgress, StatusDone:
		return true
	default:
		return false
	}
}

// ParseStatus converts a raw string, typically a query parameter or a request
// body field, into a valid Status.
func ParseStatus(raw string) (Status, error) {
	s := Status(raw)
	if !s.Valid() {
		return "", fmt.Errorf("%w: status %q must be one of todo, in_progress or done", ErrInvalid, raw)
	}
	return s, nil
}
