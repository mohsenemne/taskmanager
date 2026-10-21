package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/mohsenemne/taskmanager/internal/task"
)

// maxBodyBytes bounds request bodies so a malformed client cannot exhaust memory.
const maxBodyBytes = 1 << 20 // 1 MiB

// TaskService is the part of the application layer the API depends on. It is
// declared here, where it is consumed, so the transport does not import the
// application package.
type TaskService interface {
	Create(ctx context.Context, t task.Task) (task.Task, error)
	Get(ctx context.Context, id uuid.UUID) (task.Task, error)
	List(ctx context.Context, f task.Filter) ([]task.Task, int, error)
	Update(ctx context.Context, t task.Task) (task.Task, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type taskHandler struct {
	svc TaskService
}

func newTaskHandler(svc TaskService) *taskHandler {
	return &taskHandler{svc: svc}
}

func (h *taskHandler) create(c *gin.Context) {
	var req createTaskRequest
	if err := bindJSON(c, &req); err != nil {
		writeBadRequest(c, err.Error())
		return
	}

	created, err := h.svc.Create(c.Request.Context(), req.toTask())
	if err != nil {
		writeError(c, err)
		return
	}

	c.Header("Location", "/api/v1/tasks/"+created.ID.String())
	c.JSON(http.StatusCreated, newTaskResponse(created))
}

func (h *taskHandler) get(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}

	found, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, newTaskResponse(found))
}

func (h *taskHandler) list(c *gin.Context) {
	filter, err := parseFilter(c)
	if err != nil {
		writeBadRequest(c, err.Error())
		return
	}

	items, total, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		writeError(c, err)
		return
	}

	resp := listResponse{
		Items: make([]taskResponse, 0, len(items)),
		Page:  pageMeta{Total: total, Limit: filter.Limit, Offset: filter.Offset},
	}
	for _, item := range items {
		resp.Items = append(resp.Items, newTaskResponse(item))
	}

	c.JSON(http.StatusOK, resp)
}

func (h *taskHandler) update(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}

	var req updateTaskRequest
	if err := bindJSON(c, &req); err != nil {
		writeBadRequest(c, err.Error())
		return
	}

	updated, err := h.svc.Update(c.Request.Context(), req.toTask(id))
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, newTaskResponse(updated))
}

func (h *taskHandler) delete(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// bindJSON decodes a request body into dst, rejecting unknown fields so a typo
// in a client payload fails loudly instead of being silently ignored.
func bindJSON(c *gin.Context, dst any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	if err := c.ShouldBindJSON(dst); err != nil {
		return errors.New("malformed request body")
	}
	return nil
}

// taskID parses the {id} path parameter, responding with 400 when it is not a
// UUID.
func taskID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeBadRequest(c, "task id must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}

// parseFilter reads the listing query parameters. It rejects out-of-range
// values instead of silently clamping them, so a client cannot believe it saw
// 500 results when it saw 100.
func parseFilter(c *gin.Context) (task.Filter, error) {
	filter := task.Filter{Limit: task.DefaultPageSize}

	if raw := c.Query("status"); raw != "" {
		status, err := task.ParseStatus(raw)
		if err != nil {
			return task.Filter{}, errors.New("status must be one of todo, in_progress or done")
		}
		filter.Status = status
	}

	if raw := c.Query("assignee"); raw != "" {
		filter.Assignee = &raw
	}

	if raw := c.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > task.MaxPageSize {
			return task.Filter{}, fmt.Errorf("limit must be an integer between 1 and %d", task.MaxPageSize)
		}
		filter.Limit = limit
	}

	if raw := c.Query("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return task.Filter{}, errors.New("offset must be a non-negative integer")
		}
		filter.Offset = offset
	}

	return filter, nil
}
