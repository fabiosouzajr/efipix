package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, func(context.Context) error { return nil }) // ready: deps ok

	for _, path := range []string{"/health", "/live", "/ready"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, path)
	}
}

func TestReadyFailsWhenDepDown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, func(context.Context) error { return context.DeadlineExceeded })
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/ready", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}
