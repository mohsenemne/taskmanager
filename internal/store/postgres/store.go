// Package postgres implements the task repository on top of PostgreSQL.
package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mohsenemne/taskmanager/internal/task"
)

// errNotImplemented marks the store methods that the next commit fills in.
// It exists so the contract tests can run against a compiling package first.
var errNotImplemented = errors.New("postgres: not implemented")

// Store is the PostgreSQL implementation of task.Repository.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a store that uses pool for every query.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

var _ task.Repository = (*Store)(nil)

// Create stores a task and returns it with identity and timestamps filled in.
func (s *Store) Create(ctx context.Context, t task.Task) (task.Task, error) {
	return task.Task{}, errNotImplemented
}

// Get returns a single task by id.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	return task.Task{}, errNotImplemented
}

// List returns one page of tasks and the total number of matches.
func (s *Store) List(ctx context.Context, f task.Filter) ([]task.Task, int, error) {
	return nil, 0, errNotImplemented
}

// Update replaces the mutable fields of a task.
func (s *Store) Update(ctx context.Context, t task.Task) (task.Task, error) {
	return task.Task{}, errNotImplemented
}

// Delete removes a task by id.
func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}
