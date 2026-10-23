package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/mohsenemne/taskmanager/internal/store/postgres"
	"github.com/mohsenemne/taskmanager/internal/task"
)

// RepositorySuite is the contract every task.Repository implementation must
// satisfy. It runs against a real PostgreSQL instance because constraints,
// indexes and ordering are part of the contract.
type RepositorySuite struct {
	suite.Suite
	pool *pgxpool.Pool
	repo *postgres.Store
}

func TestRepositorySuite(t *testing.T) {
	suite.Run(t, new(RepositorySuite))
}

func (s *RepositorySuite) SetupTest() {
	s.pool = newPool(s.T())
	s.repo = postgres.New(s.pool)

	_, err := s.pool.Exec(context.Background(), "TRUNCATE TABLE tasks")
	s.Require().NoError(err)
}

func (s *RepositorySuite) createTask(t task.Task) task.Task {
	s.T().Helper()

	created, err := s.repo.Create(context.Background(), t)
	s.Require().NoError(err)

	return created
}

func (s *RepositorySuite) TestCreatePersistsTask() {
	created := s.createTask(task.Task{
		Title:       "write the repository",
		Description: "cover the contract with integration tests",
		Status:      task.StatusInProgress,
		Assignee:    ptr("mohsen"),
	})

	s.NotEqual(uuid.Nil, created.ID)
	s.Equal("write the repository", created.Title)
	s.Equal("cover the contract with integration tests", created.Description)
	s.Equal(task.StatusInProgress, created.Status)
	s.Require().NotNil(created.Assignee)
	s.Equal("mohsen", *created.Assignee)
	s.False(created.CreatedAt.IsZero())
	s.False(created.UpdatedAt.IsZero())

	stored, err := s.repo.Get(context.Background(), created.ID)
	s.Require().NoError(err)
	s.Equal(created.ID, stored.ID)
	s.Equal(created.Title, stored.Title)
	s.WithinDuration(created.CreatedAt, stored.CreatedAt, time.Millisecond)
}

func (s *RepositorySuite) TestCreateRejectsDuplicateID() {
	existing := s.createTask(task.Task{Title: "first", Status: task.StatusTodo})

	_, err := s.repo.Create(context.Background(), task.Task{
		ID:     existing.ID,
		Title:  "second",
		Status: task.StatusTodo,
	})

	s.ErrorIs(err, task.ErrConflict)
}

func (s *RepositorySuite) TestGetUnknownIDReturnsNotFound() {
	_, err := s.repo.Get(context.Background(), uuid.New())

	s.ErrorIs(err, task.ErrNotFound)
}

func (s *RepositorySuite) TestListFiltersByStatus() {
	s.createTask(task.Task{Title: "a", Status: task.StatusTodo})
	s.createTask(task.Task{Title: "b", Status: task.StatusDone})
	s.createTask(task.Task{Title: "c", Status: task.StatusDone})

	page, err := s.repo.List(context.Background(), task.Filter{Status: task.StatusDone})
	s.Require().NoError(err)

	s.Require().Len(page, 2)

	for _, item := range page {
		s.Equal(task.StatusDone, item.Status)
	}
}

func (s *RepositorySuite) TestListFiltersByAssignee() {
	s.createTask(task.Task{Title: "a", Status: task.StatusTodo, Assignee: ptr("mohsen")})
	s.createTask(task.Task{Title: "b", Status: task.StatusTodo, Assignee: ptr("sara")})
	s.createTask(task.Task{Title: "c", Status: task.StatusTodo})

	page, err := s.repo.List(context.Background(), task.Filter{Assignee: ptr("mohsen")})
	s.Require().NoError(err)

	s.Require().Len(page, 1)
	s.Equal("a", page[0].Title)
}

func (s *RepositorySuite) TestListReturnsNewestFirst() {
	s.seedOrderedTasks()

	page, err := s.repo.List(context.Background(), task.Filter{Limit: 2})
	s.Require().NoError(err)

	s.Require().Len(page, 2)
	s.Equal("task 2", page[0].Title)
	s.Equal("task 1", page[1].Title)
}

func (s *RepositorySuite) TestListContinuesAfterCursor() {
	s.seedOrderedTasks()

	first, err := s.repo.List(context.Background(), task.Filter{Limit: 2})
	s.Require().NoError(err)
	s.Require().Len(first, 2)

	last := first[len(first)-1]
	second, err := s.repo.List(context.Background(), task.Filter{
		Limit: 2,
		After: &task.Cursor{CreatedAt: last.CreatedAt, ID: last.ID},
	})
	s.Require().NoError(err)

	s.Require().Len(second, 1)
	s.Equal("task 0", second[0].Title)
}

// TestListIsStableWhenARowIsInserted is the reason for keyset pagination: a
// write between two pages must not make the client repeat or skip a row.
func (s *RepositorySuite) TestListIsStableWhenARowIsInserted() {
	s.seedOrderedTasks()

	first, err := s.repo.List(context.Background(), task.Filter{Limit: 2})
	s.Require().NoError(err)
	s.Require().Len(first, 2)

	s.createTask(task.Task{
		Title:     "task 3",
		Status:    task.StatusTodo,
		CreatedAt: time.Date(2026, 1, 1, 12, 3, 0, 0, time.UTC),
	})

	last := first[len(first)-1]
	second, err := s.repo.List(context.Background(), task.Filter{
		Limit: 2,
		After: &task.Cursor{CreatedAt: last.CreatedAt, ID: last.ID},
	})
	s.Require().NoError(err)

	s.Require().Len(second, 1)
	s.Equal("task 0", second[0].Title, "the new row must not shift the continuation")
}

// seedOrderedTasks creates task 0, 1 and 2 one minute apart, so the newest is
// task 2.
func (s *RepositorySuite) seedOrderedTasks() {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		s.createTask(task.Task{
			Title:     fmt.Sprintf("task %d", i),
			Status:    task.StatusTodo,
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
}

func (s *RepositorySuite) TestUpdateReplacesMutableFields() {
	created := s.createTask(task.Task{
		Title:    "before",
		Status:   task.StatusTodo,
		Assignee: ptr("mohsen"),
	})

	updated, err := s.repo.Update(context.Background(), task.Task{
		ID:          created.ID,
		Title:       "after",
		Description: "now with a description",
		Status:      task.StatusDone,
		Assignee:    nil,
	})
	s.Require().NoError(err)

	s.Equal(created.ID, updated.ID)
	s.Equal("after", updated.Title)
	s.Equal("now with a description", updated.Description)
	s.Equal(task.StatusDone, updated.Status)
	s.Nil(updated.Assignee)
	s.WithinDuration(created.CreatedAt, updated.CreatedAt, time.Millisecond)
	s.False(updated.UpdatedAt.Before(created.UpdatedAt))
}

func (s *RepositorySuite) TestUpdateUnknownIDReturnsNotFound() {
	_, err := s.repo.Update(context.Background(), task.Task{
		ID:     uuid.New(),
		Title:  "ghost",
		Status: task.StatusTodo,
	})

	s.ErrorIs(err, task.ErrNotFound)
}

func (s *RepositorySuite) TestDeleteRemovesTask() {
	created := s.createTask(task.Task{Title: "temporary", Status: task.StatusTodo})

	s.Require().NoError(s.repo.Delete(context.Background(), created.ID))

	_, err := s.repo.Get(context.Background(), created.ID)
	s.ErrorIs(err, task.ErrNotFound)
}

func (s *RepositorySuite) TestDeleteUnknownIDReturnsNotFound() {
	s.ErrorIs(s.repo.Delete(context.Background(), uuid.New()), task.ErrNotFound)
}

func ptr(s string) *string { return &s }
