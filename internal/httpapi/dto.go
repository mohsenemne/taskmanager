package httpapi

import (
	"time"

	"github.com/google/uuid"

	"github.com/mohsenemne/taskmanager/internal/task"
)

// CreateTaskRequest is the body of POST /api/v1/tasks.
type CreateTaskRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Assignee    *string `json:"assignee"`
}

func (r CreateTaskRequest) toTask() task.Task {
	return task.Task{
		Title:       r.Title,
		Description: r.Description,
		Status:      task.Status(r.Status),
		Assignee:    r.Assignee,
	}
}

// UpdateTaskRequest is the body of PUT /api/v1/tasks/{id}. PUT replaces the
// mutable fields, so every field is expected on the wire.
type UpdateTaskRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Assignee    *string `json:"assignee"`
}

func (r UpdateTaskRequest) toTask(id uuid.UUID) task.Task {
	return task.Task{
		ID:          id,
		Title:       r.Title,
		Description: r.Description,
		Status:      task.Status(r.Status),
		Assignee:    r.Assignee,
	}
}

// TaskResponse is the representation of a task on the wire.
type TaskResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Assignee    *string   `json:"assignee"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newTaskResponse(t task.Task) TaskResponse {
	return TaskResponse{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Assignee:    t.Assignee,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// ListResponse carries one page of tasks plus the paging metadata needed to
// walk the rest of the collection.
type ListResponse struct {
	Items []TaskResponse `json:"items"`
	Page  PageMeta       `json:"page"`
}

// PageMeta describes where a page sits within the full result set.
type PageMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// ErrorResponse is the single error shape every endpoint returns.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the machine-readable code and human message of an error.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
