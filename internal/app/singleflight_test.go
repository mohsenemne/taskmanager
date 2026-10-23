package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mohsenemne/taskmanager/internal/task"
	taskmocks "github.com/mohsenemne/taskmanager/internal/task/mocks"
)

// memCache is a concurrency-safe cache used by the flight tests, where the
// question is how many calls reach the repository rather than how the cache is
// invoked.
type memCache struct {
	mu    sync.Mutex
	items map[string][]byte
}

func newMemCache() *memCache {
	return &memCache{items: make(map[string][]byte)}
}

func (c *memCache) Get(
	_ context.Context,
	key string,
) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	value, found := c.items[key]

	return value, found, nil
}

func (c *memCache) Set(
	_ context.Context,
	key string,
	value []byte,
	_ time.Duration,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = value

	return nil
}

func (c *memCache) Delete(_ context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, key := range keys {
		delete(c.items, key)
	}

	return nil
}

// TestConcurrentGetsShareOneQuery is the reason singleflight exists here: ten
// requests arriving on a cold key must produce one query, not ten.
func TestConcurrentGetsShareOneQuery(t *testing.T) {
	id := uuid.New()
	found := task.Task{ID: id, Title: "shared"}

	repo := taskmocks.NewMockRepository(t)
	repo.EXPECT().
		Get(mock.Anything, id).
		Run(func(context.Context, uuid.UUID) { time.Sleep(50 * time.Millisecond) }).
		Return(found, nil).
		Once()

	svc := NewTaskService(repo, newMemCache(), time.Minute)

	const callers = 10

	var (
		wg   sync.WaitGroup
		errs = make([]error, callers)
	)

	for i := range callers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, errs[i] = svc.Get(context.Background(), id)
		}()
	}

	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
}

// TestConcurrentListsShareOneQuery does the same for the hot page, which is the
// key a write invalidates for everyone at once.
func TestConcurrentListsShareOneQuery(t *testing.T) {
	repo := taskmocks.NewMockRepository(t)
	repo.EXPECT().
		List(mock.Anything, mock.Anything).
		Run(func(context.Context, task.Filter) { time.Sleep(50 * time.Millisecond) }).
		Return([]task.Task{{Title: "shared"}}, nil).
		Once()

	svc := NewTaskService(repo, newMemCache(), time.Minute)

	const callers = 10

	var (
		wg   sync.WaitGroup
		errs = make([]error, callers)
	)

	for i := range callers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, errs[i] = svc.List(context.Background(), task.Filter{})
		}()
	}

	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
}
