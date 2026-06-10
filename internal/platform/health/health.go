package health

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ReadyCheck reports whether dependencies are reachable.
type ReadyCheck func(ctx context.Context) error

func Register(r gin.IRoutes, ready ReadyCheck) {
	r.GET("/live", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "live"}) })
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/ready", func(c *gin.Context) {
		if err := ready(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready", "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
}
