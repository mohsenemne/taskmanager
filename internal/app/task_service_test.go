package app

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/mohsenemne/taskmanager/internal/task"
	"github.com/mohsenemne/taskmanager/internal/task/mocks"
)

type TaskServiceSuite struct {
	suite.Suite
	repo *mocks.MockRepository
	svc  *TaskService
}

func TestTaskServiceSuite(t *testing.T) {
	suite.Run(t, new(TaskServiceSuite))
}

func (s *TaskServiceSuite) SetupTest() {
	s.repo = mocks.NewMockRepository(s.T())
	s.svc = NewTaskService(s.repo)
}

// The tests below script the repository with MatchedBy where the point is what
// the service sends to storage, and with mock.Anything where it is not.

func (s *TaskServiceSuite) TestCreateAppliesDefaults() {
	id := uuid.New()
	s.repo.EXPECT().
		Create(mock.Anything, mock.MatchedBy(func(in task.Task) bool {
			return in.Title == "write the service" &&
				in.Status == task.StatusTodo &&
				in.Assignee != nil && *in.Assignee == "mohsen"
		})).
		Return(task.Task{ID: id, Title: "write the service", Status: task.StatusTodo}, nil)

	created, err := s.svc.Create(context.Background(), task.Task{
		Title:    "  write the service  ",
		Assignee: ptr("  mohsen  "),
	})
	s.Require().NoError(err)
	s.Equal(id, created.ID)
}

func (s *TaskServiceSuite) TestCreateTreatsBlankAssigneeAsUnassigned() {
	s.repo.EXPECT().
		Create(mock.Anything, mock.MatchedBy(func(in task.Task) bool {
			return in.Assignee == nil
		})).
		Return(task.Task{Title: "review the harness"}, nil)

	_, err := s.svc.Create(context.Background(), task.Task{
		Title:    "review the harness",
		Assignee: ptr("   "),
	})
	s.Require().NoError(err)
}

func (s *TaskServiceSuite) TestCreateRejectsInvalidInput() {
	tests := []struct {
		name string
		in   task.Task
	}{
		{name: "empty title", in: task.Task{}},
		{name: "whitespace only title", in: task.Task{Title: "   "}},
		{name: "title too long", in: task.Task{Title: strings.Repeat("a", maxTitleLength+1)}},
		{name: "unknown status", in: task.Task{Title: "valid title", Status: "blocked"}},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// No EXPECT: an unexpected call fails the test instead of reaching storage.
			_, err := s.svc.Create(context.Background(), tt.in)
			s.ErrorIs(err, task.ErrInvalid)
		})
	}
}

func (s *TaskServiceSuite) TestCreateAcceptsTitleAtTheLimit() {
	title := strings.Repeat("a", maxTitleLength)
	s.repo.EXPECT().
		Create(mock.Anything, mock.MatchedBy(func(in task.Task) bool {
			return in.Title == title
		})).
		Return(task.Task{Title: title}, nil)

	created, err := s.svc.Create(context.Background(), task.Task{Title: title})
	s.Require().NoError(err)
	s.Equal(title, created.Title)
}

func (s *TaskServiceSuite) TestCreatePropagatesRepositoryErrors() {
	s.repo.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(task.Task{}, task.ErrConflict)

	_, err := s.svc.Create(context.Background(), task.Task{Title: "duplicate"})
	s.ErrorIs(err, task.ErrConflict)
}

func (s *TaskServiceSuite) TestGetReturnsStoredTask() {
	id := uuid.New()
	s.repo.EXPECT().
		Get(mock.Anything, id).
		Return(task.Task{ID: id, Title: "lookup"}, nil)

	found, err := s.svc.Get(context.Background(), id)
	s.Require().NoError(err)
	s.Equal(id, found.ID)
	s.Equal("lookup", found.Title)
}

func (s *TaskServiceSuite) TestGetRejectsZeroID() {
	// No EXPECT: the identifier is rejected before storage is consulted.
	_, err := s.svc.Get(context.Background(), uuid.Nil)
	s.ErrorIs(err, task.ErrInvalid)
}

func (s *TaskServiceSuite) TestGetPropagatesNotFound() {
	id := uuid.New()
	s.repo.EXPECT().
		Get(mock.Anything, id).
		Return(task.Task{}, task.ErrNotFound)

	_, err := s.svc.Get(context.Background(), id)
	s.ErrorIs(err, task.ErrNotFound)
}

func (s *TaskServiceSuite) TestListAppliesDefaults() {
	s.repo.EXPECT().
		List(mock.Anything, task.Filter{Limit: defaultPageSize, Offset: 0}).
		Return([]task.Task{}, 0, nil)

	_, _, err := s.svc.List(context.Background(), task.Filter{})
	s.Require().NoError(err)
}

func (s *TaskServiceSuite) TestListClampsNegativeOffset() {
	s.repo.EXPECT().
		List(mock.Anything, task.Filter{Limit: defaultPageSize, Offset: 0}).
		Return([]task.Task{}, 0, nil)

	_, _, err := s.svc.List(context.Background(), task.Filter{Offset: -5})
	s.Require().NoError(err)
}

func (s *TaskServiceSuite) TestListCapsPageSize() {
	s.repo.EXPECT().
		List(mock.Anything, task.Filter{Limit: maxPageSize, Offset: 0}).
		Return([]task.Task{}, 0, nil)

	_, _, err := s.svc.List(context.Background(), task.Filter{Limit: 500})
	s.Require().NoError(err)
}

func (s *TaskServiceSuite) TestListRejectsUnknownStatus() {
	// No EXPECT: the status is rejected before storage is consulted.
	_, _, err := s.svc.List(context.Background(), task.Filter{Status: "blocked"})
	s.ErrorIs(err, task.ErrInvalid)
}

func (s *TaskServiceSuite) TestListPassesFiltersThrough() {
	assignee := "mohsen"
	want := task.Filter{Status: task.StatusDone, Assignee: &assignee, Limit: 5, Offset: 2}

	s.repo.EXPECT().
		List(mock.Anything, want).
		Return([]task.Task{{Title: "shipped"}}, 1, nil)

	items, total, err := s.svc.List(context.Background(), want)
	s.Require().NoError(err)
	s.Equal(1, total)
	s.Require().Len(items, 1)
	s.Equal("shipped", items[0].Title)
}

func (s *TaskServiceSuite) TestUpdateAppliesNormalization() {
	id := uuid.New()
	s.repo.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(in task.Task) bool {
			return in.ID == id &&
				in.Title == "after" &&
				in.Description == "with a description" &&
				in.Status == task.StatusDone &&
				in.Assignee == nil
		})).
		Return(task.Task{ID: id, Title: "after", Status: task.StatusDone}, nil)

	updated, err := s.svc.Update(context.Background(), task.Task{
		ID:          id,
		Title:       "  after  ",
		Description: "  with a description  ",
		Status:      task.StatusDone,
	})
	s.Require().NoError(err)
	s.Equal("after", updated.Title)
}

func (s *TaskServiceSuite) TestUpdateRequiresID() {
	// No EXPECT: the missing identifier is rejected before storage is consulted.
	_, err := s.svc.Update(context.Background(), task.Task{
		Title:  "no id",
		Status: task.StatusTodo,
	})
	s.ErrorIs(err, task.ErrInvalid)
}

func (s *TaskServiceSuite) TestUpdateRejectsBlankTitle() {
	// No EXPECT: the title is rejected before storage is consulted.
	_, err := s.svc.Update(context.Background(), task.Task{
		ID:     uuid.New(),
		Title:  "   ",
		Status: task.StatusTodo,
	})
	s.ErrorIs(err, task.ErrInvalid)
}

func (s *TaskServiceSuite) TestUpdatePropagatesNotFound() {
	s.repo.EXPECT().
		Update(mock.Anything, mock.Anything).
		Return(task.Task{}, task.ErrNotFound)

	_, err := s.svc.Update(context.Background(), task.Task{
		ID:     uuid.New(),
		Title:  "ghost",
		Status: task.StatusTodo,
	})
	s.ErrorIs(err, task.ErrNotFound)
}

func (s *TaskServiceSuite) TestDeleteRemovesTask() {
	id := uuid.New()
	s.repo.EXPECT().
		Delete(mock.Anything, id).
		Return(nil)

	s.Require().NoError(s.svc.Delete(context.Background(), id))
}

func (s *TaskServiceSuite) TestDeleteRejectsZeroID() {
	// No EXPECT: the identifier is rejected before storage is consulted.
	s.ErrorIs(s.svc.Delete(context.Background(), uuid.Nil), task.ErrInvalid)
}

func (s *TaskServiceSuite) TestDeletePropagatesNotFound() {
	id := uuid.New()
	s.repo.EXPECT().
		Delete(mock.Anything, id).
		Return(task.ErrNotFound)

	s.ErrorIs(s.svc.Delete(context.Background(), id), task.ErrNotFound)
}

func ptr(s string) *string { return &s }
