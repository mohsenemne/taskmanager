// Package httpapi exposes the task application services over HTTP.
package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// Pinger reports whether a dependency, typically the database, is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewRouter builds the HTTP surface: the versioned task API plus the liveness
// and readiness probes. Middleware is installed before any route so it also
// covers endpoints the caller mounts afterwards.
func NewRouter(svc TaskService, db Pinger, middleware ...gin.HandlerFunc) *gin.Engine {
	// Reject unknown JSON fields on every request.
	binding.EnableDecoderDisallowUnknownFields = true

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware...)

	h := newTaskHandler(svc)

	v1 := router.Group("/api/v1")
	v1.POST("/tasks", h.create)
	v1.GET("/tasks", h.list)
	v1.GET("/tasks/:id", h.get)
	v1.PUT("/tasks/:id", h.update)
	v1.DELETE("/tasks/:id", h.delete)

	router.GET("/healthz", func(c *gin.Context) {
		c.String(http.StatusOK, "ok\n")
	})

	router.GET("/readyz", func(c *gin.Context) {
		if err := db.Ping(c.Request.Context()); err != nil {
			c.String(http.StatusServiceUnavailable, "not ready\n")
			return
		}
		c.String(http.StatusOK, "ok\n")
	})

	return router
}
