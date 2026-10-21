package httpapi

import (
	"time"

	"github.com/google/uuid"

	"github.com/mohsenemne/taskmanager/internal/task"
)

// createTaskRequest is the body of POST /api/v1/tasks.
type createTaskRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Assignee    *string `json:"assignee"`
}

func (r createTaskRequest) toTask() task.Task {
	return task.Task{
		Title:       r.Title,
		Description: r.Description,
		Status:      task.Status(r.Status),
		Assignee:    r.Assignee,
	}
}

// updateTaskRequest is the body of PUT /api/v1/tasks/{id}. PUT replaces the
// mutable fields, so every field is expected on the wire.
type updateTaskRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Assignee    *string `json:"assignee"`
}

func (r updateTaskRequest) toTask(id uuid.UUID) task.Task {
	return task.Task{
		ID:          id,
		Title:       r.Title,
		Description: r.Description,
		Status:      task.Status(r.Status),
		Assignee:    r.Assignee,
	}
}

// taskResponse is the representation of a task on the wire.
type taskResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Assignee    *string   `json:"assignee"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newTaskResponse(t task.Task) taskResponse {
	return taskResponse{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		Assignee:    t.Assignee,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// listResponse carries one page of tasks plus the paging metadata needed to
// walk the rest of the collection.
type listResponse struct {
	Items []taskResponse `json:"items"`
	Page  pageMeta       `json:"page"`
}

type pageMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// errorResponse is the single error shape every endpoint returns.
type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
