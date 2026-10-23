# Task Manager

A small REST service for managing tasks, built with Go, Gin and PostgreSQL. It
is meant to be read as much as run: the interesting parts are the layering, the
tests, and the observability wiring.

## Contents

- [Quickstart](#quickstart)
- [API](#api)
- [Configuration](#configuration)
- [Testing](#testing)
- [Observability](#observability)
- [Design](#design)
- [Trade-offs](#trade-offs)
- [Assumptions and scope](#assumptions-and-scope)
- [Development](#development)

## Quickstart

Requires Docker with Compose. Everything else runs in containers.

```bash
docker compose up --build -d
curl -s localhost:8080/healthz          # ok
```

Then:

| URL | What |
| --- | --- |
| <http://localhost:8080/swagger/index.html> | Swagger UI for the API |
| <http://localhost:8080/metrics> | Prometheus metrics |
| <http://localhost:9090> | Prometheus |
| <http://localhost:16686> | Jaeger, with traces from the service |

Create a task:

```bash
curl -s -X POST localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title":"Write the README","assignee":"mohsen"}'
```

```json
{
  "id": "01a16038-2a91-73d6-9bb2-bdd0d9230f0d",
  "title": "Write the README",
  "description": "",
  "status": "todo",
  "assignee": "mohsen",
  "created_at": "2026-10-21T20:47:36.09627Z",
  "updated_at": "2026-10-21T20:47:36.09627Z"
}
```

Stop the stack. `down` keeps the database volume; `reset` deletes it.

```bash
make down    # docker compose down
make reset   # docker compose down -v  (destroys data)
```

If `go mod download` fails during the image build because `proxy.golang.org` is
unreachable from your network, point the build at a mirror:

```bash
GOPROXY=https://your-mirror.example/repository/go/ docker compose build
```

The same variable can live in `.env`, which Compose reads for build arguments.

## API

Base path `/api/v1`. All bodies are JSON; timestamps are RFC 3339.

| Method | Path | Description | Success |
| --- | --- | --- | --- |
| `POST` | `/tasks` | Create a task | `201` + `Location` |
| `GET` | `/tasks` | List tasks | `200` |
| `GET` | `/tasks/{id}` | Fetch one task | `200` |
| `PUT` | `/tasks/{id}` | Replace a task | `200` |
| `DELETE` | `/tasks/{id}` | Delete a task | `204` |

`status` is one of `todo`, `in_progress`, `done`. `assignee` is nullable.

### Create

```bash
curl -s -X POST localhost:8080/api/v1/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ship it","description":"Cut a release","status":"in_progress","assignee":"sara"}'
```

`title` is required and limited to 200 characters after trimming. A missing
`status` defaults to `todo`.

### List

```bash
curl -s 'localhost:8080/api/v1/tasks?status=in_progress&limit=2'
```

```json
{
  "items": [ { "id": "…", "title": "Ship it", "status": "in_progress" } ],
  "page": { "has_next": true, "next_cursor": "eyJ2IjoxLCJ0cyI6…" }
}
```

| Parameter | Type | Default | Notes |
| --- | --- | --- | --- |
| `status` | string | — | `todo`, `in_progress` or `done` |
| `assignee` | string | — | exact match |
| `limit` | int | 20 | 1–100 |
| `cursor` | string | — | opaque; pass `page.next_cursor` back |

Newest first, ordered by `created_at DESC, id DESC`. Pagination is keyset: send
`page.next_cursor` as `cursor` to continue, and stop when `has_next` is false.
Each page costs one index seek regardless of depth, and a row inserted or
deleted between requests cannot make the client skip or repeat a task.

There is deliberately no total. Counting matching rows on every request is
proportional to the table, not to the page, and infinite scroll does not need
it — `has_next` answers the only question the client asks.

A malformed or unknown-version cursor is rejected with `400`, as is a `limit`
outside 1–100, rather than being silently clamped.

### Update

`PUT` replaces the mutable fields; every field is expected.

```bash
curl -s -X PUT localhost:8080/api/v1/tasks/$ID \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ship it","description":"","status":"done","assignee":null}'
```

### Delete

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE localhost:8080/api/v1/tasks/$ID
```

### Errors

Every failure uses one envelope:

```json
{"error": {"code": "invalid_request", "message": "title is required"}}
```

| Code | Status | When |
| --- | --- | --- |
| `invalid_request` | 400 | malformed body, bad UUID, invalid field or query value |
| `not_found` | 404 | no task with that id |
| `conflict` | 409 | a uniqueness collision |
| `internal` | 500 | anything unexpected; the message is generic and the detail is logged |

Unknown JSON fields are rejected, and bodies are capped at 1 MiB.

### Probes

`GET /healthz` returns `200` while the process is alive. `GET /readyz` pings the
database and returns `503` when it is unreachable. Neither is part of the
versioned API or the OpenAPI document.

## Configuration

Environment variables, all optional except `DATABASE_URL`.

| Variable | Default | Notes |
| --- | --- | --- |
| `DATABASE_URL` | — | required, e.g. `postgres://user:pass@host:5432/tasks?sslmode=disable` |
| `APP_ENV` | `development` | `development` or `production` |
| `HTTP_ADDR` | `:8080` | `host:port` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | empty writes spans to stdout |
| `READ_HEADER_TIMEOUT` | `5s` | |
| `READ_TIMEOUT` | `10s` | |
| `WRITE_TIMEOUT` | `15s` | |
| `IDLE_TIMEOUT` | `60s` | |
| `SHUTDOWN_TIMEOUT` | `10s` | budget for in-flight requests |

Invalid values are reported together at startup, not one at a time. `.env.example`
documents the same set; `make env` copies it to `.env` for `make run`.

## Testing

```bash
make test              # unit tests, no Docker, ~2s
make test-integration  # store contract against PostgreSQL 16
make test-all          # both
make cover             # both, with a coverage total
```

Current state: `make test` green, `make test-integration` green, coverage ~91%.

The split matters. Unit tests cover the application rules against a mock
repository and the HTTP layer against a mocked service; neither touches a
database, so they run anywhere. The store tests need a real PostgreSQL because
the contract includes constraints, `NULL` handling, ordering and unique
violations — things a mock cannot prove. They start a container through
testcontainers, or use `TEST_DATABASE_URL` when that is set, and skip under
`-short`.

Mocks are generated by [mockery](https://github.com/vektra/mockery) from the two
interfaces that matter (`task.Repository`, `httpapi.TaskService`) and live next
to those interfaces, in `mocks/` subpackages. Regenerate with `make mocks`.

## Observability

### Metrics

`/metrics` is served from the main port. If you want a separate port in
production, that is a one-line change in `newServer`.

| Metric | Type | Labels |
| --- | --- | --- |
| `http_requests_total` | counter | `method`, `route`, `status` |
| `http_request_duration_seconds` | histogram | `method`, `route` |
| `http_requests_in_flight` | gauge | — |
| `tasks_count` | gauge | — |
| `db_pool_acquired_conns`, `db_pool_idle_conns`, `db_pool_total_conns`, `db_pool_max_conns` | gauges | — |
| `app_build_info` | gauge | `version`, `environment` |
| `go_*`, `process_*` | collectors | — |

`route` is the matched pattern (`/api/v1/tasks/:id`), never the raw path. Requests
that match nothing are labelled `unmatched`, so a client cannot create unbounded
series by requesting random URLs.

`tasks_count` is refreshed every 15 seconds from the database rather than
incremented on writes. That keeps it truthful when rows change through anything
other than this service, at the cost of up to one interval of staleness.

CPU and memory come from the Go and process collectors: `go_goroutines`,
`go_memstats_*`, `process_cpu_seconds_total`, `process_resident_memory_bytes`.
CPU is a counter, so use `rate()`; the memory series are gauges.

### Traces

Every request is a span, and every query is a child of it:

```text
POST /api/v1/tasks
  task.create
    pool.acquire
    INSERT
```

`task.create`, `task.list` and friends are the business steps, so a slow request
can be attributed to the service, to connection acquisition, or to the query —
not just to "the handler".

Spans go to stdout by default. In Compose, `OTEL_EXPORTER_OTLP_ENDPOINT` points
at Jaeger, and the UI is at <http://localhost:16686>. Propagation is W3C trace
context, so an incoming `traceparent` continues the trace.

Latency metrics carry **exemplars** with the trace ID, which is what lets a slow
histogram bucket in Grafana open the trace that produced it. This requires
OpenMetrics, which is why the handler enables it.

### Logs

JSON on stdout via `slog`, one line per request:

```json
{"level":"WARN","msg":"http request","request_id":"d1f6c89c-…",
 "trace_id":"325d3c69…","span_id":"e1e3cd2c…",
 "method":"GET","route":"/api/v1/tasks/:id","status":400,"duration_ms":0.1}
```

`X-Request-ID` is echoed to the caller and reused when supplied. The level
follows the status class: 2xx/3xx `info`, 4xx `warn`, 5xx `error`. The `trace_id`
and `span_id` make the line resolvable in the trace UI. The DSN is never logged —
only the database host and name.

## Design

```mermaid
flowchart LR
  client[Client] -->|HTTP| router[Gin router]
  router --> mw[middleware:<br/>tracing, logging, metrics]
  mw --> svc[app.TaskService]
  svc --> port[task.Repository]
  port -.-> store[postgres.Store]
  store --> db[(PostgreSQL)]

  router --> prom[Prometheus]
  router --> jaeger[Jaeger]
```

The dependency points inward. `app` depends on the `task.Repository` interface
and never imports pgx; `postgres` implements that interface; nothing in the
domain knows about HTTP.

```text
cmd/api/            wiring: config, pool, migrations, router, shutdown
internal/task/      domain: Task, Status, Repository port, errors, mocks
internal/app/       use cases: validation, defaults, pagination, spans
internal/httpapi/   transport: routes, DTOs, error mapping, mocks
internal/store/     adapter: pgx repository, migrations runner
internal/observability/  logging, metrics, tracing, middleware
migrations/         embedded SQL, applied by golang-migrate at startup
api/docs/           generated OpenAPI spec (swag)
```

Three decisions shape the rest:

**The repository is a port.** `task.Repository` is declared where it is consumed,
so the use cases are testable without a database and the domain stays free of
driver types.

**Use cases live in their own package.** With one aggregate this is a judgement
call; the package is the place where a second aggregate would be coordinated
without the domain packages importing each other.

**Tests are split by what they can prove.** Contract tests against real
PostgreSQL for storage semantics, mocked edges for everything above it.

## Trade-offs

| Decision | Alternative | Why |
| --- | --- | --- |
| UUIDv7 identifiers | `bigint` identity | Time-ordered, so inserts stay sequential, and identifiers can be generated before the insert. A `bigint` would be smaller and simpler if IDs never needed to leave the database or be created client-side. |
| Keyset (cursor) pagination | `OFFSET` / `LIMIT` | Stable while rows are inserted or deleted, and the cost of a page does not grow with depth, which is what infinite scroll needs. It gives up page numbers and a `total`: a paged UI that renders "page 3 of 17" would need an additional count endpoint. |
| `DESC` index columns | `ASC` index columns | A B-tree scans backwards, so an `ASC` index would serve `ORDER BY created_at DESC, id DESC` with an `Index Scan Backward` at the same cost. `DESC` is written to mirror the query, and so that a future mixed ordering (`created_at DESC, id ASC`) becomes an explicit change in the migration rather than a silent reinterpretation. Direction also picks the default null placement, which is moot here because `created_at` is `NOT NULL`. |
| PostgreSQL owns timestamps | Application clock | The application and the database can run on different hosts with different clocks. Letting the database stamp `created_at`/`updated_at` keeps them comparable; a test caught a real ordering bug caused by mixing the two. |
| Scheduled `tasks_count` | Increment on write | A gauge updated after each write drifts as soon as one write fails or another writer appears. A query every 15 seconds is cheap and always true, at the cost of staleness. |
| Generated mocks committed | Generate in CI | `go test ./...` works on a fresh clone with no toolchain step. Staleness is caught because the mock is passed where the interface is expected. |
| Generated docs committed | Generate at build | Same reason: `docker compose up --build` needs no `swag` on the host. `make swag` regenerates; a CI check would catch drift. |

## Assumptions and scope

- **No authentication or tenancy.** Every caller sees every task. Adding auth is
  a project, not a line.
- **`assignee` is a free-text string**, not a user entity. The service does not
  verify that the assignee exists.
- **Single instance.** Migrations run at boot, and the in-memory cache — there
  isn't one yet — would be process-local.
- **Hard deletes.** No soft-delete column, no audit trail.
- **No optimistic concurrency.** Two updates to the same task resolve last-write
  wins.
- **Health probes are outside the API contract** and deliberately absent from
  the OpenAPI document.

## Development

```bash
make help              # every target
make run               # run locally against DATABASE_URL from .env
make test              # unit tests
make test-integration  # PostgreSQL-backed tests
make cover             # coverage report
make lint              # golangci-lint (see .golangci.yml)
make mocks             # regenerate mockery mocks
make swag              # regenerate api/docs from handler annotations
make build             # binary in bin/
```

Go 1.26 or newer is required: `go.mod` pins that floor because the tool
dependencies (mockery, swag) need it, not because the service code does.

The linter runs `bodyclose`, `misspell`, `revive`, `gocritic`, `staticcheck`,
`gocognit`, `funlen`, `nestif`, `lll` and `wsl_v5` over the standard set.
Generated code is excluded; so are `lll`/`funlen` in test tables and `lll` on
Swagger annotations.
