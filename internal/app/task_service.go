// Package app holds the use cases that coordinate the domain packages.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/mohsenemne/taskmanager/internal/task"
)

const (
	// maxTitleLength mirrors the CHECK constraint on tasks.title.
	maxTitleLength = 200
	// tracerName identifies this package's spans.
	tracerName = "github.com/mohsenemne/taskmanager/internal/app"
)

// startSpan resolves the globally configured tracer at call time, so tests can
// swap the provider without this package holding a stale reference.
func startSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer(tracerName).Start(ctx, name)
}

// recordError attaches err to the span. Expected outcomes — a rejected input, a
// missing task, a conflict — are recorded as events but do not mark the span as
// failed, because they are not system faults.
func recordError(span trace.Span, err error) {
	if err == nil {
		return
	}

	span.RecordError(err)
	if errors.Is(err, task.ErrInvalid) || errors.Is(err, task.ErrNotFound) || errors.Is(err, task.ErrConflict) {
		return
	}
	span.SetStatus(codes.Error, err.Error())
}

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
	ctx, span := startSpan(ctx, "task.create")
	defer span.End()

	in = normalize(in)
	if in.Status == "" {
		in.Status = task.StatusTodo
	}
	if err := validate(in); err != nil {
		recordError(span, err)
		return task.Task{}, err
	}

	created, err := s.repo.Create(ctx, in)
	if err != nil {
		recordError(span, err)
		return task.Task{}, err
	}

	span.SetAttributes(attribute.String("task.id", created.ID.String()))
	return created, nil
}

// Get returns a single task.
func (s *TaskService) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	ctx, span := startSpan(ctx, "task.get")
	defer span.End()

	if id == uuid.Nil {
		err := fmt.Errorf("%w: id is required", task.ErrInvalid)
		recordError(span, err)
		return task.Task{}, err
	}

	found, err := s.repo.Get(ctx, id)
	if err != nil {
		recordError(span, err)
		return task.Task{}, err
	}

	span.SetAttributes(attribute.String("task.id", found.ID.String()))
	return found, nil
}

// List returns one page of tasks and the total number of matches.
func (s *TaskService) List(ctx context.Context, f task.Filter) ([]task.Task, int, error) {
	ctx, span := startSpan(ctx, "task.list")
	defer span.End()

	if f.Status != "" && !f.Status.Valid() {
		err := fmt.Errorf("%w: status %q must be one of todo, in_progress or done", task.ErrInvalid, f.Status)
		recordError(span, err)
		return nil, 0, err
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

	span.SetAttributes(
		attribute.Int("page.limit", f.Limit),
		attribute.Int("page.offset", f.Offset),
	)
	if f.Status != "" {
		span.SetAttributes(attribute.String("filter.status", string(f.Status)))
	}
	if f.Assignee != nil {
		span.SetAttributes(attribute.String("filter.assignee", *f.Assignee))
	}

	items, total, err := s.repo.List(ctx, f)
	if err != nil {
		recordError(span, err)
		return nil, 0, err
	}

	span.SetAttributes(
		attribute.Int("result.count", len(items)),
		attribute.Int("result.total", total),
	)
	return items, total, nil
}

// Update replaces the mutable fields of a task.
func (s *TaskService) Update(ctx context.Context, in task.Task) (task.Task, error) {
	ctx, span := startSpan(ctx, "task.update")
	defer span.End()

	if in.ID == uuid.Nil {
		err := fmt.Errorf("%w: id is required", task.ErrInvalid)
		recordError(span, err)
		return task.Task{}, err
	}
	span.SetAttributes(attribute.String("task.id", in.ID.String()))

	in = normalize(in)
	if err := validate(in); err != nil {
		recordError(span, err)
		return task.Task{}, err
	}

	updated, err := s.repo.Update(ctx, in)
	if err != nil {
		recordError(span, err)
		return task.Task{}, err
	}
	return updated, nil
}

// Delete removes a task.
func (s *TaskService) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := startSpan(ctx, "task.delete")
	defer span.End()

	if id == uuid.Nil {
		err := fmt.Errorf("%w: id is required", task.ErrInvalid)
		recordError(span, err)
		return err
	}
	span.SetAttributes(attribute.String("task.id", id.String()))

	if err := s.repo.Delete(ctx, id); err != nil {
		recordError(span, err)
		return err
	}
	return nil
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
