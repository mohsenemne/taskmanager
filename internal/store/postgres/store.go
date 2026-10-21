// Package postgres implements the task repository on top of PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohsenemne/taskmanager/internal/task"
)

// Store is the PostgreSQL implementation of task.Repository.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a store that uses pool for every query.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

var _ task.Repository = (*Store)(nil)

// taskColumns is the projection every read shares, so scanTask stays in sync
// with the SELECT and RETURNING clauses.
const taskColumns = `id, title, description, status, assignee, ` +
	`created_at, updated_at`

// defaultLimit bounds an unbounded list query.
const defaultLimit = 100

// Create stores a task and returns it with identity and timestamps filled in.
//
// Timestamps come from the database clock, not the application clock, so that
// created_at and updated_at stay comparable even when the app and PostgreSQL
// run on different hosts. A caller may still supply created_at explicitly,
// which the tests use to produce deterministic ordering.
func (s *Store) Create(ctx context.Context, t task.Task) (task.Task, error) {
	if t.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return task.Task{}, fmt.Errorf("generate task id: %w", err)
		}

		t.ID = id
	}

	var createdAt, updatedAt *time.Time

	if !t.CreatedAt.IsZero() {
		createdAt = &t.CreatedAt
		updatedAt = &t.CreatedAt
	}

	if !t.UpdatedAt.IsZero() {
		updatedAt = &t.UpdatedAt
	}

	const query = `
		INSERT INTO tasks (
			id, title, description, status, assignee, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			COALESCE($6::timestamptz, now()),
			COALESCE($7::timestamptz, now())
		)
		RETURNING ` + taskColumns

	row := s.pool.QueryRow(
		ctx,
		query,
		t.ID,
		t.Title,
		t.Description,
		string(t.Status),
		t.Assignee,
		createdAt,
		updatedAt,
	)

	created, err := scanTask(row)
	if err != nil {
		if isUniqueViolation(err) {
			return task.Task{}, fmt.Errorf(
				"%w: task %s",
				task.ErrConflict,
				t.ID,
			)
		}

		return task.Task{}, fmt.Errorf("create task: %w", err)
	}

	return created, nil
}

// Get returns a single task by id.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	const query = `SELECT ` + taskColumns + ` FROM tasks WHERE id = $1`

	found, err := scanTask(s.pool.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, fmt.Errorf("%w: task %s", task.ErrNotFound, id)
	}

	if err != nil {
		return task.Task{}, fmt.Errorf("get task: %w", err)
	}

	return found, nil
}

// List returns one page of tasks and the total number of matches.
func (s *Store) List(
	ctx context.Context,
	f task.Filter,
) ([]task.Task, int, error) {
	where, args := buildListFilter(f)

	var total int
	if err := s.pool.QueryRow(
		ctx,
		"SELECT count(*) FROM tasks"+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	offset := max(f.Offset, 0)

	query := fmt.Sprintf(
		`SELECT %s FROM tasks%s `+
			`ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		taskColumns,
		where,
		len(args)+1,
		len(args)+2,
	)

	rows, err := s.pool.Query(ctx, query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]task.Task, 0, limit)

	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan task: %w", err)
		}

		tasks = append(tasks, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}

	return tasks, total, nil
}

// buildListFilter turns a filter into a WHERE clause and its arguments.
func buildListFilter(f task.Filter) (string, []any) {
	var (
		conditions []string
		args       []any
	)

	if f.Status != "" {
		args = append(args, string(f.Status))
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}

	if f.Assignee != nil {
		args = append(args, *f.Assignee)
		conditions = append(
			conditions,
			fmt.Sprintf("assignee = $%d", len(args)),
		)
	}

	if len(conditions) == 0 {
		return "", args
	}

	return " WHERE " + strings.Join(conditions, " AND "), args
}

// Update replaces the mutable fields of a task and returns the stored row.
func (s *Store) Update(ctx context.Context, t task.Task) (task.Task, error) {
	const query = `
		UPDATE tasks
		SET
			title = $2,
			description = $3,
			status = $4,
			assignee = $5,
			updated_at = now()
		WHERE id = $1
		RETURNING ` + taskColumns

	updated, err := scanTask(s.pool.QueryRow(
		ctx,
		query,
		t.ID,
		t.Title,
		t.Description,
		string(t.Status),
		t.Assignee,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, fmt.Errorf("%w: task %s", task.ErrNotFound, t.ID)
	}

	if err != nil {
		return task.Task{}, fmt.Errorf("update task: %w", err)
	}

	return updated, nil
}

// Delete removes a task by id.
func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: task %s", task.ErrNotFound, id)
	}

	return nil
}

// CountTasks returns the number of stored tasks. It backs the tasks_count
// gauge and is not part of the repository contract the service depends on.
func (s *Store) CountTasks(ctx context.Context) (int64, error) {
	var total int64

	row := s.pool.QueryRow(ctx, `SELECT count(*) FROM tasks`)
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("count tasks: %w", err)
	}

	return total, nil
}

// scanTask maps one row of the shared projection into a domain task.
func scanTask(row pgx.Row) (task.Task, error) {
	var (
		t        task.Task
		status   string
		assignee pgtype.Text
	)

	err := row.Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&status,
		&assignee,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		return task.Task{}, err
	}

	t.Status = task.Status(status)

	if assignee.Valid {
		value := assignee.String
		t.Assignee = &value
	}

	return t, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique constraint
// error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
