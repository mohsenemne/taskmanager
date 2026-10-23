package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/mohsenemne/taskmanager/internal/httpapi"
	"github.com/mohsenemne/taskmanager/internal/httpapi/mocks"
	"github.com/mohsenemne/taskmanager/internal/task"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.ReleaseMode)
	os.Exit(m.Run())
}

// stubPinger stands in for the database readiness check.
type stubPinger struct{ err error }

func (p stubPinger) Ping(context.Context) error { return p.err }

type HandlerSuite struct {
	suite.Suite
	svc    *mocks.MockTaskService
	router http.Handler
}

func TestHandlerSuite(t *testing.T) {
	suite.Run(t, new(HandlerSuite))
}

func (s *HandlerSuite) SetupTest() {
	s.svc = mocks.NewMockTaskService(s.T())
	s.router = httpapi.NewRouter(s.svc, stubPinger{})
}

func (s *HandlerSuite) do(method, target, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)

	return rec
}

// assertError checks the status and the error envelope, and returns the message
// so a test can assert on the detail.
func (s *HandlerSuite) assertError(rec *httptest.ResponseRecorder, status int, code string) string {
	s.T().Helper()

	s.Equal(status, rec.Code)

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &body))
	s.Equal(code, body.Error.Code)
	s.NotEmpty(body.Error.Message)

	return body.Error.Message
}

func (s *HandlerSuite) TestCreateReturnsCreatedTask() {
	id := uuid.New()
	now := time.Now().UTC()

	s.svc.EXPECT().
		Create(mock.Anything, mock.MatchedBy(func(in task.Task) bool {
			return in.Title == "write api" && in.Status == task.StatusTodo
		})).
		Return(task.Task{
			ID:        id,
			Title:     "write api",
			Status:    task.StatusTodo,
			CreatedAt: now,
			UpdatedAt: now,
		}, nil)

	rec := s.do(http.MethodPost, "/api/v1/tasks", `{"title":"write api","status":"todo"}`)
	s.Require().Equal(http.StatusCreated, rec.Code)
	s.Equal("/api/v1/tasks/"+id.String(), rec.Header().Get("Location"))

	var body struct {
		ID       string  `json:"id"`
		Title    string  `json:"title"`
		Status   string  `json:"status"`
		Assignee *string `json:"assignee"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &body))
	s.Equal(id.String(), body.ID)
	s.Equal("write api", body.Title)
	s.Equal("todo", body.Status)
	s.Nil(body.Assignee)
}

func (s *HandlerSuite) TestCreateRejectsMalformedBody() {
	rec := s.do(http.MethodPost, "/api/v1/tasks", `{"title":`)
	s.assertError(rec, http.StatusBadRequest, "invalid_request")
}

func (s *HandlerSuite) TestCreateRejectsUnknownFields() {
	rec := s.do(http.MethodPost, "/api/v1/tasks", `{"title":"write api","nope":1}`)
	s.assertError(rec, http.StatusBadRequest, "invalid_request")
}

func (s *HandlerSuite) TestCreateMapsValidationError() {
	s.svc.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(task.Task{}, fmt.Errorf("%w: title is required", task.ErrInvalid))

	rec := s.do(http.MethodPost, "/api/v1/tasks", `{}`)

	message := s.assertError(rec, http.StatusBadRequest, "invalid_request")
	s.Equal("title is required", message, "the domain sentinel prefix should not leak")
}

func (s *HandlerSuite) TestCreateMapsConflict() {
	s.svc.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(task.Task{}, fmt.Errorf("%w: duplicate", task.ErrConflict))

	rec := s.do(http.MethodPost, "/api/v1/tasks", `{"title":"duplicate"}`)
	s.assertError(rec, http.StatusConflict, "conflict")
}

func (s *HandlerSuite) TestCreateMapsInternalError() {
	s.svc.EXPECT().
		Create(mock.Anything, mock.Anything).
		Return(task.Task{}, errors.New("connection reset by peer"))

	rec := s.do(http.MethodPost, "/api/v1/tasks", `{"title":"write api"}`)

	message := s.assertError(rec, http.StatusInternalServerError, "internal")
	s.Equal("internal server error", message, "internal errors must not leak to the client")
}

func (s *HandlerSuite) TestGetReturnsTask() {
	id := uuid.New()
	s.svc.EXPECT().
		Get(mock.Anything, id).
		Return(task.Task{ID: id, Title: "lookup", Status: task.StatusDone}, nil)

	rec := s.do(http.MethodGet, "/api/v1/tasks/"+id.String(), "")
	s.Require().Equal(http.StatusOK, rec.Code)

	var body struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &body))
	s.Equal(id.String(), body.ID)
	s.Equal("lookup", body.Title)
}

func (s *HandlerSuite) TestGetRejectsInvalidID() {
	// No EXPECT: the identifier must be rejected before the service is called.
	rec := s.do(http.MethodGet, "/api/v1/tasks/not-a-uuid", "")
	s.assertError(rec, http.StatusBadRequest, "invalid_request")
}

func (s *HandlerSuite) TestGetMapsNotFound() {
	id := uuid.New()
	s.svc.EXPECT().
		Get(mock.Anything, id).
		Return(task.Task{}, fmt.Errorf("%w: %s", task.ErrNotFound, id))

	rec := s.do(http.MethodGet, "/api/v1/tasks/"+id.String(), "")
	s.assertError(rec, http.StatusNotFound, "not_found")
}

func (s *HandlerSuite) TestListReturnsPageAndCursor() {
	next := task.Cursor{CreatedAt: time.Now().UTC(), ID: uuid.New()}

	s.svc.EXPECT().
		List(mock.Anything, mock.MatchedBy(func(f task.Filter) bool {
			return f.Status == task.StatusDone &&
				f.Assignee != nil && *f.Assignee == "mohsen" &&
				f.Limit == 5 && f.After == nil
		})).
		Return(task.Page{
			Items:   []task.Task{{ID: uuid.New(), Title: "shipped", Status: task.StatusDone}},
			HasNext: true,
			Next:    &next,
		}, nil).
		Once()

	s.svc.EXPECT().
		List(mock.Anything, mock.MatchedBy(func(f task.Filter) bool {
			return f.After != nil && f.After.ID == next.ID
		})).
		Return(task.Page{Items: []task.Task{}, HasNext: false}, nil).
		Once()

	rec := s.do(
		http.MethodGet,
		"/api/v1/tasks?status=done&assignee=mohsen&limit=5",
		"",
	)
	s.Require().Equal(http.StatusOK, rec.Code)

	var first struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
		Page struct {
			HasNext    bool   `json:"has_next"`
			NextCursor string `json:"next_cursor"`
		} `json:"page"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &first))
	s.Require().Len(first.Items, 1)
	s.Equal("shipped", first.Items[0].Title)
	s.True(first.Page.HasNext)
	s.NotEmpty(first.Page.NextCursor)

	// The cursor from one response must drive the next request.
	rec = s.do(http.MethodGet, "/api/v1/tasks?cursor="+first.Page.NextCursor, "")
	s.Require().Equal(http.StatusOK, rec.Code)

	var second struct {
		Items []struct{} `json:"items"`
		Page  struct {
			HasNext    bool   `json:"has_next"`
			NextCursor string `json:"next_cursor"`
		} `json:"page"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &second))
	s.False(second.Page.HasNext)
	s.Empty(second.Page.NextCursor)
}

func (s *HandlerSuite) TestListUsesDefaultPage() {
	s.svc.EXPECT().
		List(mock.Anything, task.Filter{Limit: task.DefaultPageSize}).
		Return(task.Page{Items: []task.Task{}}, nil)

	rec := s.do(http.MethodGet, "/api/v1/tasks", "")
	s.Require().Equal(http.StatusOK, rec.Code)

	var body struct {
		Items []struct{} `json:"items"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &body))
	s.NotNil(body.Items, "an empty page must serialise as [] rather than null")
	s.Len(body.Items, 0)
}

func (s *HandlerSuite) TestListRejectsInvalidCursor() {
	// No EXPECT: a malformed cursor is rejected before the service is called.
	rec := s.do(http.MethodGet, "/api/v1/tasks?cursor=not-a-cursor", "")
	s.assertError(rec, http.StatusBadRequest, "invalid_request")
}

func (s *HandlerSuite) TestListRejectsUnknownStatus() {
	// No EXPECT: the filter must be rejected before the service is called.
	rec := s.do(http.MethodGet, "/api/v1/tasks?status=blocked", "")
	s.assertError(rec, http.StatusBadRequest, "invalid_request")
}

func (s *HandlerSuite) TestListRejectsOutOfRangeLimit() {
	rec := s.do(http.MethodGet, "/api/v1/tasks?limit=500", "")
	message := s.assertError(rec, http.StatusBadRequest, "invalid_request")
	s.Contains(message, "limit")
}

func (s *HandlerSuite) TestUpdateReturnsTask() {
	id := uuid.New()
	s.svc.EXPECT().
		Update(mock.Anything, mock.MatchedBy(func(in task.Task) bool {
			return in.ID == id && in.Title == "after" && in.Status == task.StatusDone
		})).
		Return(task.Task{ID: id, Title: "after", Status: task.StatusDone}, nil)

	rec := s.do(http.MethodPut, "/api/v1/tasks/"+id.String(),
		`{"title":"after","status":"done"}`)
	s.Require().Equal(http.StatusOK, rec.Code)

	var body struct {
		Title string `json:"title"`
	}
	s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &body))
	s.Equal("after", body.Title)
}

func (s *HandlerSuite) TestUpdateRejectsInvalidID() {
	rec := s.do(http.MethodPut, "/api/v1/tasks/nope", `{"title":"after"}`)
	s.assertError(rec, http.StatusBadRequest, "invalid_request")
}

func (s *HandlerSuite) TestUpdateMapsNotFound() {
	id := uuid.New()
	s.svc.EXPECT().
		Update(mock.Anything, mock.Anything).
		Return(task.Task{}, fmt.Errorf("%w: %s", task.ErrNotFound, id))

	rec := s.do(http.MethodPut, "/api/v1/tasks/"+id.String(), `{"title":"ghost","status":"todo"}`)
	s.assertError(rec, http.StatusNotFound, "not_found")
}

func (s *HandlerSuite) TestDeleteReturnsNoContent() {
	id := uuid.New()
	s.svc.EXPECT().
		Delete(mock.Anything, id).
		Return(nil)

	rec := s.do(http.MethodDelete, "/api/v1/tasks/"+id.String(), "")
	s.Equal(http.StatusNoContent, rec.Code)
	s.Empty(rec.Body.String())
}

func (s *HandlerSuite) TestDeleteMapsNotFound() {
	id := uuid.New()
	s.svc.EXPECT().
		Delete(mock.Anything, id).
		Return(fmt.Errorf("%w: %s", task.ErrNotFound, id))

	rec := s.do(http.MethodDelete, "/api/v1/tasks/"+id.String(), "")
	s.assertError(rec, http.StatusNotFound, "not_found")
}

func (s *HandlerSuite) TestHealthz() {
	rec := s.do(http.MethodGet, "/healthz", "")
	s.Equal(http.StatusOK, rec.Code)
}

func (s *HandlerSuite) TestReadyzHealthy() {
	rec := s.do(http.MethodGet, "/readyz", "")
	s.Equal(http.StatusOK, rec.Code)
}

func (s *HandlerSuite) TestReadyzUnavailable() {
	router := httpapi.NewRouter(s.svc, stubPinger{err: fmt.Errorf("database down")})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	s.Equal(http.StatusServiceUnavailable, rec.Code)
}
