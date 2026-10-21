package task

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
