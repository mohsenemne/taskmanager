// Package app holds the use cases that coordinate the domain packages.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
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

	// hotListKey holds the unfiltered first page. It is the only listing
	// cached: one key means one DEL per write, with nothing to enumerate.
	hotListKey = "tasks:list:v1"

	// taskKeyBase prefixes the cache key of a single task.
	taskKeyBase = "tasks:item:v1:"
)

// taskKey is the cache key of a single task.
func taskKey(id uuid.UUID) string {
	return taskKeyBase + id.String()
}

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

	if errors.Is(err, task.ErrInvalid) || errors.Is(err, task.ErrNotFound) ||
		errors.Is(err, task.ErrConflict) {
		return
	}

	span.SetStatus(codes.Error, err.Error())
}

// TaskService applies the task rules on top of a task.Repository. It is the
// entry point the HTTP layer talks to.
type TaskService struct {
	repo  task.Repository
	cache Cache
	ttl   time.Duration
}

// NewTaskService returns a service backed by repo. A nil cache disables
// caching, which is how the service runs without Redis.
func NewTaskService(
	repo task.Repository,
	cache Cache,
	ttl time.Duration,
) *TaskService {
	return &TaskService{repo: repo, cache: cache, ttl: ttl}
}

// Create validates in, applies defaults and stores it.
func (s *TaskService) Create(
	ctx context.Context,
	in task.Task,
) (task.Task, error) {
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
	s.invalidate(ctx, hotListKey)

	return created, nil
}

// Get returns a single task.
func (s *TaskService) Get(
	ctx context.Context,
	id uuid.UUID,
) (task.Task, error) {
	ctx, span := startSpan(ctx, "task.get")
	defer span.End()

	if id == uuid.Nil {
		err := fmt.Errorf("%w: id is required", task.ErrInvalid)
		recordError(span, err)

		return task.Task{}, err
	}

	if cached, ok := s.cachedTask(ctx, taskKey(id)); ok {
		span.SetAttributes(
			attribute.String("task.id", cached.ID.String()),
			attribute.Bool("cache.hit", true),
		)

		return cached, nil
	}

	found, err := s.repo.Get(ctx, id)
	if err != nil {
		recordError(span, err)
		return task.Task{}, err
	}

	s.storeTask(ctx, taskKey(id), found)

	span.SetAttributes(attribute.String("task.id", found.ID.String()))

	return found, nil
}

// List returns one page of tasks and the total number of matches.
func (s *TaskService) List(
	ctx context.Context,
	f task.Filter,
) (task.Page, error) {
	ctx, span := startSpan(ctx, "task.list")
	defer span.End()

	if f.Status != "" && !f.Status.Valid() {
		err := fmt.Errorf(
			"%w: status %q must be one of todo, in_progress or done",
			task.ErrInvalid,
			f.Status,
		)
		recordError(span, err)

		return task.Page{}, err
	}

	limit := f.Limit
	switch {
	case limit <= 0:
		limit = task.DefaultPageSize
	case limit > task.MaxPageSize:
		limit = task.MaxPageSize
	}

	// Ask for one row more than the page: "has next" then costs no extra query.
	f.Limit = limit + 1

	span.SetAttributes(attribute.Int("page.limit", limit))
	annotateFilter(span, f)

	cacheable := s.cacheable(f, limit)
	if cacheable {
		if page, ok := s.cachedPage(ctx, hotListKey); ok {
			return page, nil
		}
	}

	items, err := s.repo.List(ctx, f)
	if err != nil {
		recordError(span, err)
		return task.Page{}, err
	}

	page := buildPage(items, limit)

	if cacheable {
		s.storePage(ctx, hotListKey, page)
	}

	span.SetAttributes(
		attribute.Int("result.count", len(page.Items)),
		attribute.Bool("result.has_next", page.HasNext),
	)

	return page, nil
}

// buildPage trims the look-ahead row and derives the cursor for the page that
// follows.
func buildPage(items []task.Task, limit int) task.Page {
	page := task.Page{Items: items}
	if len(items) > limit {
		page.HasNext = true
		page.Items = items[:limit]
	}

	if page.HasNext && len(page.Items) > 0 {
		last := page.Items[len(page.Items)-1]
		page.Next = &task.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}

	return page
}

// annotateFilter records the filter on the span.
func annotateFilter(span trace.Span, f task.Filter) {
	if f.Status != "" {
		span.SetAttributes(attribute.String("filter.status", string(f.Status)))
	}

	if f.Assignee != nil {
		span.SetAttributes(attribute.String("filter.assignee", *f.Assignee))
	}

	if f.After != nil {
		span.SetAttributes(attribute.String("page.after", f.After.ID.String()))
	}
}

// cacheable reports whether a listing is the hot, unfiltered first page. Any
// filter, cursor or custom page size goes straight to the database: caching
// those would need a way to enumerate keys on write, and one key does not.
func (s *TaskService) cacheable(f task.Filter, limit int) bool {
	return s.cache != nil &&
		f.After == nil &&
		f.Status == "" &&
		f.Assignee == nil &&
		limit == task.DefaultPageSize
}

// cachedTask returns the cached task, if the cache is enabled and holds a
// readable entry. A corrupt entry is treated as a miss.
func (s *TaskService) cachedTask(
	ctx context.Context,
	key string,
) (task.Task, bool) {
	if s.cache == nil {
		return task.Task{}, false
	}

	raw, found, err := s.cache.Get(ctx, key)
	if err != nil || !found {
		return task.Task{}, false
	}

	var cached task.Task
	if err := json.Unmarshal(raw, &cached); err != nil {
		return task.Task{}, false
	}

	return cached, true
}

// storeTask caches a task. Failures are ignored: the TTL and the next write
// both bound staleness, and a read must not fail because the cache did.
func (s *TaskService) storeTask(ctx context.Context, key string, t task.Task) {
	if s.cache == nil {
		return
	}

	raw, err := json.Marshal(t)
	if err != nil {
		return
	}

	_ = s.cache.Set(ctx, key, raw, s.ttl)
}

// cachedPage returns the cached hot listing, if any.
func (s *TaskService) cachedPage(
	ctx context.Context,
	key string,
) (task.Page, bool) {
	if s.cache == nil {
		return task.Page{}, false
	}

	raw, found, err := s.cache.Get(ctx, key)
	if err != nil || !found {
		return task.Page{}, false
	}

	var page task.Page
	if err := json.Unmarshal(raw, &page); err != nil {
		return task.Page{}, false
	}

	return page, true
}

// storePage caches the hot listing.
func (s *TaskService) storePage(
	ctx context.Context,
	key string,
	page task.Page,
) {
	if s.cache == nil {
		return
	}

	raw, err := json.Marshal(page)
	if err != nil {
		return
	}

	_ = s.cache.Set(ctx, key, raw, s.ttl)
}

// invalidate drops the entries a write made stale. Failures are ignored: the
// TTL bounds the damage and a cache outage must not fail the write.
func (s *TaskService) invalidate(ctx context.Context, keys ...string) {
	if s.cache == nil {
		return
	}

	_ = s.cache.Delete(ctx, keys...)
}

// Update replaces the mutable fields of a task.
func (s *TaskService) Update(
	ctx context.Context,
	in task.Task,
) (task.Task, error) {
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

	s.invalidate(ctx, taskKey(in.ID), hotListKey)

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

	s.invalidate(ctx, taskKey(id), hotListKey)

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
		return fmt.Errorf(
			"%w: title must be at most %d characters, got %d",
			task.ErrInvalid,
			maxTitleLength,
			n,
		)
	}

	if !t.Status.Valid() {
		return fmt.Errorf(
			"%w: status %q must be one of todo, in_progress or done",
			task.ErrInvalid,
			t.Status,
		)
	}

	return nil
}
