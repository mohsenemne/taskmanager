// Package app holds the use cases that coordinate the domain packages.
package app

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mohsenemne/taskmanager/internal/task"
)

const (
	// maxTitleLength mirrors the CHECK constraint on tasks.title.
	maxTitleLength = 200
)

// TaskService applies the task rules on top of a task.Repository. It is the
// entry point the HTTP layer talks to.
type TaskService struct {
	repo task.Repository
}

// NewTaskService returns a service backed by repo.
func NewTaskService(repo task.Repository) *TaskService {
	return &TaskService{repo: repo}
}

// Create validates in, applies defaults and stores it.
func (s *TaskService) Create(ctx context.Context, in task.Task) (task.Task, error) {
	in = normalize(in)
	if in.Status == "" {
		in.Status = task.StatusTodo
	}
	if err := validate(in); err != nil {
		return task.Task{}, err
	}
	return s.repo.Create(ctx, in)
}

// Get returns a single task.
func (s *TaskService) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	if id == uuid.Nil {
		return task.Task{}, fmt.Errorf("%w: id is required", task.ErrInvalid)
	}
	return s.repo.Get(ctx, id)
}

// List returns one page of tasks and the total number of matches.
func (s *TaskService) List(ctx context.Context, f task.Filter) ([]task.Task, int, error) {
	if f.Status != "" && !f.Status.Valid() {
		return nil, 0, fmt.Errorf("%w: status %q must be one of todo, in_progress or done", task.ErrInvalid, f.Status)
	}

	switch {
	case f.Limit <= 0:
		f.Limit = task.DefaultPageSize
	case f.Limit > task.MaxPageSize:
		f.Limit = task.MaxPageSize
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	return s.repo.List(ctx, f)
}

// Update replaces the mutable fields of a task.
func (s *TaskService) Update(ctx context.Context, in task.Task) (task.Task, error) {
	if in.ID == uuid.Nil {
		return task.Task{}, fmt.Errorf("%w: id is required", task.ErrInvalid)
	}
	in = normalize(in)
	if err := validate(in); err != nil {
		return task.Task{}, err
	}
	return s.repo.Update(ctx, in)
}

// Delete removes a task.
func (s *TaskService) Delete(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: id is required", task.ErrInvalid)
	}
	return s.repo.Delete(ctx, id)
}

// normalize trims user input so the same values are stored regardless of how
// the client formatted them. An assignee that is only whitespace becomes
// unassigned rather than storing an empty string.
func normalize(t task.Task) task.Task {
	t.Title = strings.TrimSpace(t.Title)
	t.Description = strings.TrimSpace(t.Description)

	if t.Assignee != nil {
		assignee := strings.TrimSpace(*t.Assignee)
		if assignee == "" {
			t.Assignee = nil
		} else {
			t.Assignee = &assignee
		}
	}

	return t
}

// validate enforces the rules the database also enforces, so callers get
// ErrInvalid instead of a driver or constraint error.
func validate(t task.Task) error {
	if t.Title == "" {
		return fmt.Errorf("%w: title is required", task.ErrInvalid)
	}
	if n := utf8.RuneCountInString(t.Title); n > maxTitleLength {
		return fmt.Errorf("%w: title must be at most %d characters, got %d", task.ErrInvalid, maxTitleLength, n)
	}
	if !t.Status.Valid() {
		return fmt.Errorf("%w: status %q must be one of todo, in_progress or done", task.ErrInvalid, t.Status)
	}
	return nil
}
