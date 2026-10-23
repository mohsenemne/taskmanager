package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/mohsenemne/taskmanager/internal/app/mocks"
	"github.com/mohsenemne/taskmanager/internal/task"
	taskmocks "github.com/mohsenemne/taskmanager/internal/task/mocks"
)

type CacheSuite struct {
	suite.Suite
	repo  *taskmocks.MockRepository
	cache *mocks.MockCache
	svc   *TaskService
	ttl   time.Duration
}

func TestCacheSuite(t *testing.T) {
	suite.Run(t, new(CacheSuite))
}

func (s *CacheSuite) SetupTest() {
	s.repo = taskmocks.NewMockRepository(s.T())
	s.cache = mocks.NewMockCache(s.T())
	s.ttl = time.Minute
	s.svc = NewTaskService(s.repo, s.cache, s.ttl)
}

func marshal(s *CacheSuite, v any) []byte {
	s.T().Helper()

	raw, err := json.Marshal(v)
	s.Require().NoError(err)

	return raw
}

func (s *CacheSuite) TestGetServesFromCache() {
	id := uuid.New()
	cached := task.Task{ID: id, Title: "cached"}

	s.cache.EXPECT().
		Get(mock.Anything, taskKey(id)).
		Return(marshal(s, cached), true, nil)

	// No repository expectation: a hit must not reach the database.
	got, err := s.svc.Get(context.Background(), id)
	s.Require().NoError(err)
	s.Equal("cached", got.Title)
}

func (s *CacheSuite) TestGetOnMissReadsAndCaches() {
	id := uuid.New()
	found := task.Task{ID: id, Title: "from database"}

	s.cache.EXPECT().
		Get(mock.Anything, taskKey(id)).
		Return(nil, false, nil)
	s.repo.EXPECT().
		Get(mock.Anything, id).
		Return(found, nil)
	s.cache.EXPECT().
		Set(mock.Anything, taskKey(id), mock.Anything, s.ttl).
		Return(nil)

	got, err := s.svc.Get(context.Background(), id)
	s.Require().NoError(err)
	s.Equal("from database", got.Title)
}

func (s *CacheSuite) TestGetFallsBackWhenTheCacheFails() {
	id := uuid.New()
	found := task.Task{ID: id, Title: "from database"}

	s.cache.EXPECT().
		Get(mock.Anything, taskKey(id)).
		Return(nil, false, errors.New("connection refused"))
	s.repo.EXPECT().
		Get(mock.Anything, id).
		Return(found, nil)
	s.cache.EXPECT().
		Set(mock.Anything, taskKey(id), mock.Anything, s.ttl).
		Return(errors.New("still down"))

	// A broken cache must not fail the read.
	got, err := s.svc.Get(context.Background(), id)
	s.Require().NoError(err)
	s.Equal("from database", got.Title)
}

func (s *CacheSuite) TestGetDoesNotCacheNotFound() {
	id := uuid.New()

	s.cache.EXPECT().
		Get(mock.Anything, taskKey(id)).
		Return(nil, false, nil)
	s.repo.EXPECT().
		Get(mock.Anything, id).
		Return(task.Task{}, task.ErrNotFound)

	// No Set expectation: a negative result is never cached.
	_, err := s.svc.Get(context.Background(), id)
	s.ErrorIs(err, task.ErrNotFound)
}

func (s *CacheSuite) TestGetTreatsCorruptEntryAsMiss() {
	id := uuid.New()
	found := task.Task{ID: id, Title: "from database"}

	s.cache.EXPECT().
		Get(mock.Anything, taskKey(id)).
		Return([]byte("not json"), true, nil)
	s.repo.EXPECT().
		Get(mock.Anything, id).
		Return(found, nil)
	s.cache.EXPECT().
		Set(mock.Anything, taskKey(id), mock.Anything, s.ttl).
		Return(nil)

	got, err := s.svc.Get(context.Background(), id)
	s.Require().NoError(err)
	s.Equal("from database", got.Title)
}

func (s *CacheSuite) TestListServesCachedPage() {
	cached := task.Page{Items: []task.Task{{Title: "cached"}}}

	s.cache.EXPECT().
		Get(mock.Anything, hotListKey).
		Return(marshal(s, cached), true, nil)

	// No repository expectation: a hit must not reach the database.
	page, err := s.svc.List(context.Background(), task.Filter{})
	s.Require().NoError(err)
	s.Require().Len(page.Items, 1)
	s.Equal("cached", page.Items[0].Title)
}

func (s *CacheSuite) TestListCachesTheHotPage() {
	s.cache.EXPECT().
		Get(mock.Anything, hotListKey).
		Return(nil, false, nil)
	s.repo.EXPECT().
		List(mock.Anything, mock.MatchedBy(func(f task.Filter) bool {
			return f.Limit == task.DefaultPageSize+1
		})).
		Return([]task.Task{{Title: "from database"}}, nil)
	s.cache.EXPECT().
		Set(mock.Anything, hotListKey, mock.Anything, s.ttl).
		Return(nil)

	page, err := s.svc.List(context.Background(), task.Filter{})
	s.Require().NoError(err)
	s.Require().Len(page.Items, 1)
	s.Equal("from database", page.Items[0].Title)
}

func (s *CacheSuite) TestListSkipsCacheForFilteredList() {
	s.repo.EXPECT().
		List(mock.Anything, mock.Anything).
		Return([]task.Task{}, nil)

	// No cache expectations: anything but the hot page bypasses the cache.
	_, err := s.svc.List(context.Background(), task.Filter{Status: task.StatusDone})
	s.Require().NoError(err)
}

func (s *CacheSuite) TestListSkipsCacheForCustomLimit() {
	s.repo.EXPECT().
		List(mock.Anything, mock.Anything).
		Return([]task.Task{}, nil)

	_, err := s.svc.List(context.Background(), task.Filter{Limit: 5})
	s.Require().NoError(err)
}

func (s *CacheSuite) TestListSkipsCacheForCursor() {
	s.repo.EXPECT().
		List(mock.Anything, mock.Anything).
		Return([]task.Task{}, nil)

	after := &task.Cursor{CreatedAt: time.Now().UTC(), ID: uuid.New()}
	_, err := s.svc.List(context.Background(), task.Filter{After: after})
	s.Require().NoError(err)
}

func (s *CacheSuite) TestCreateInvalidatesHotPage() {
	id := uuid.New()

	s.repo.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(task.Task{ID: id, Title: "new"}, nil)
	s.cache.EXPECT().
		Delete(mock.Anything, hotListKey).
		Return(nil)

	_, err := s.svc.Create(context.Background(), task.Task{Title: "new"})
	s.Require().NoError(err)
}

func (s *CacheSuite) TestUpdateInvalidatesTaskAndHotPage() {
	id := uuid.New()

	s.repo.EXPECT().
		Update(mock.Anything, mock.Anything).
		Return(task.Task{ID: id, Title: "after"}, nil)
	s.cache.EXPECT().
		Delete(mock.Anything, taskKey(id), hotListKey).
		Return(nil)

	_, err := s.svc.Update(context.Background(), task.Task{
		ID:     id,
		Title:  "after",
		Status: task.StatusTodo,
	})
	s.Require().NoError(err)
}

func (s *CacheSuite) TestDeleteInvalidatesTaskAndHotPage() {
	id := uuid.New()

	s.repo.EXPECT().
		Delete(mock.Anything, id).
		Return(nil)
	s.cache.EXPECT().
		Delete(mock.Anything, taskKey(id), hotListKey).
		Return(nil)

	s.Require().NoError(s.svc.Delete(context.Background(), id))
}

func (s *CacheSuite) TestWriteSucceedsWhenInvalidationFails() {
	id := uuid.New()

	s.repo.EXPECT().
		Delete(mock.Anything, id).
		Return(nil)
	s.cache.EXPECT().
		Delete(mock.Anything, taskKey(id), hotListKey).
		Return(errors.New("redis down"))

	// The TTL bounds the staleness; the write itself must still succeed.
	s.Require().NoError(s.svc.Delete(context.Background(), id))
}
