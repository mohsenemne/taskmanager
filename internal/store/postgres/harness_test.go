package postgres_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mohsenemne/taskmanager/internal/store/postgres"
)

// testDSN is set by TestMain once a database is ready for the suites.
var testDSN string

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if testing.Short() {
		fmt.Println("store: skipping integration tests in short mode")
		return 0
	}

	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	stop := func() {}

	if dsn == "" {
		var err error

		dsn, stop, err = startPostgres(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "store: start postgres (set TEST_DATABASE_URL to reuse a local instance): %v\n", err)
			return 1
		}
	}

	defer stop()

	if err := postgres.Migrate(ctx, dsn); err != nil {
		fmt.Fprintf(os.Stderr, "store: apply migrations: %v\n", err)
		return 1
	}

	testDSN = dsn

	return m.Run()
}

// startPostgres runs a throwaway PostgreSQL in Docker and returns its DSN.
func startPostgres(ctx context.Context) (dsn string, stop func(), err error) {
	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("tasks"),
		tcpostgres.WithUsername("tasks"),
		tcpostgres.WithPassword("tasks"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		return "", nil, fmt.Errorf("run container: %w", err)
	}

	dsn, err = container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(context.Background())
		return "", nil, fmt.Errorf("connection string: %w", err)
	}

	return dsn, func() { _ = container.Terminate(context.Background()) }, nil
}

// newPool opens a pool against the test database and closes it when the test ends.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), testDSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, pool.Ping(context.Background()))

	return pool
}

// HarnessSuite proves the test infrastructure itself works: if it fails, the
// problem is the container or the migrations, not the repository.
type HarnessSuite struct {
	suite.Suite
	pool *pgxpool.Pool
}

func TestHarnessSuite(t *testing.T) {
	suite.Run(t, new(HarnessSuite))
}

func (s *HarnessSuite) SetupTest() {
	s.pool = newPool(s.T())
}

func (s *HarnessSuite) TestMigrationsAreApplied() {
	var exists bool

	err := s.pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'tasks'
		)`).Scan(&exists)

	s.Require().NoError(err)
	s.True(exists)
}

func (s *HarnessSuite) TestMigrationsAreIdempotent() {
	s.Require().NoError(postgres.Migrate(context.Background(), testDSN))
}

func (s *HarnessSuite) TestFilterIndexesExist() {
	rows, err := s.pool.Query(context.Background(), `
		SELECT indexname FROM pg_indexes
		WHERE schemaname = 'public' AND tablename = 'tasks'
		ORDER BY indexname`)
	s.Require().NoError(err)

	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string
		s.Require().NoError(rows.Scan(&name))
		names = append(names, name)
	}

	s.Require().NoError(rows.Err())

	for _, want := range []string{
		"tasks_assignee_idx",
		"tasks_created_at_idx",
		"tasks_status_idx",
	} {
		s.Contains(names, want)
	}
}
