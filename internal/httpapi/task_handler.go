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

// maxBodyBytes bounds request bodies so a malformed client cannot exhaust
// memory.
const maxBodyBytes = 1 << 20 // 1 MiB

// TaskService is the part of the application layer the API depends on. It is
// declared here, where it is consumed, so the transport does not import the
// application package.
type TaskService interface {
	Create(ctx context.Context, t task.Task) (task.Task, error)
	Get(ctx context.Context, id uuid.UUID) (task.Task, error)
	List(ctx context.Context, f task.Filter) (task.Page, error)
	Update(ctx context.Context, t task.Task) (task.Task, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type taskHandler struct {
	svc TaskService
}

func newTaskHandler(svc TaskService) *taskHandler {
	return &taskHandler{svc: svc}
}

// create handles POST /api/v1/tasks.
//
// @Summary      Create a task
// @Description  Creates a task and returns it with its server-assigned identity.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        task  body      CreateTaskRequest  true  "Task to create"
// @Success      201   {object}  TaskResponse
// @Failure      400   {object}  ErrorResponse
// @Failure      409   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Header       201   {string}  Location  "URL of the created task"
// @Router       /tasks [post]
func (h *taskHandler) create(c *gin.Context) {
	var req CreateTaskRequest
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

// get handles GET /api/v1/tasks/{id}.
//
// @Summary      Get a task
// @Tags         tasks
// @Produce      json
// @Param        id   path      string  true  "Task ID"  format(uuid)
// @Success      200  {object}  TaskResponse
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /tasks/{id} [get]
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

// list handles GET /api/v1/tasks.
//
// @Summary      List tasks
// @Description  Returns one page of tasks and the total number of matches.
// @Tags         tasks
// @Produce      json
// @Param        status    query     string  false  "Filter by status"  Enums(todo, in_progress, done)
// @Param        assignee  query     string  false  "Filter by assignee"
// @Param        limit     query     int     false  "Page size"  default(20)  minimum(1)  maximum(100)
// @Param        cursor    query     string  false  "Opaque cursor from page.next_cursor"
// @Success      200       {object}  ListResponse
// @Failure      400       {object}  ErrorResponse
// @Failure      500       {object}  ErrorResponse
// @Router       /tasks [get]
func (h *taskHandler) list(c *gin.Context) {
	filter, err := parseFilter(c)
	if err != nil {
		writeBadRequest(c, err.Error())
		return
	}

	page, err := h.svc.List(c.Request.Context(), filter)
	if err != nil {
		writeError(c, err)
		return
	}

	resp := ListResponse{
		Items: make([]TaskResponse, 0, len(page.Items)),
		Page:  PageMeta{HasNext: page.HasNext},
	}
	for _, item := range page.Items {
		resp.Items = append(resp.Items, newTaskResponse(item))
	}

	if page.Next != nil {
		resp.Page.NextCursor = encodeCursor(*page.Next)
	}

	c.JSON(http.StatusOK, resp)
}

// update handles PUT /api/v1/tasks/{id}.
//
// @Summary      Replace a task
// @Description  Replaces the mutable fields of a task; every field is expected.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id    path      string             true  "Task ID"  format(uuid)
// @Param        task  body      UpdateTaskRequest  true  "New task state"
// @Success      200   {object}  TaskResponse
// @Failure      400   {object}  ErrorResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Router       /tasks/{id} [put]
func (h *taskHandler) update(c *gin.Context) {
	id, ok := taskID(c)
	if !ok {
		return
	}

	var req UpdateTaskRequest
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

// delete handles DELETE /api/v1/tasks/{id}.
//
// @Summary      Delete a task
// @Tags         tasks
// @Param        id   path  string  true  "Task ID"  format(uuid)
// @Success      204  "No Content"
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /tasks/{id} [delete]
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
			return task.Filter{}, errors.New(
				"status must be one of todo, in_progress or done",
			)
		}

		filter.Status = status
	}

	if raw := c.Query("assignee"); raw != "" {
		filter.Assignee = &raw
	}

	if raw := c.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > task.MaxPageSize {
			return task.Filter{}, fmt.Errorf(
				"limit must be an integer between 1 and %d",
				task.MaxPageSize,
			)
		}

		filter.Limit = limit
	}

	// An empty cursor means "start from the newest task".
	cursor, err := decodeCursor(c.Query("cursor"))
	if err != nil {
		return task.Filter{}, errors.New("cursor is not valid")
	}

	filter.After = cursor

	return filter, nil
}
