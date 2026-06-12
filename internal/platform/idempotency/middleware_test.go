package idempotency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/tenantctx"
)

type fakeStore struct {
	reserveFn func() (Reservation, error)
	saved     struct {
		status int
		body   []byte
	}
}

func (f *fakeStore) Reserve(context.Context, string, string, string) (Reservation, error) {
	return f.reserveFn()
}

func (f *fakeStore) SaveResult(_ context.Context, _, _ string, status int, body []byte) error {
	f.saved.status, f.saved.body = status, body
	return nil
}

func newTestRouter(store Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		ctx := tenantctx.With(c.Request.Context(), &tenantctx.Resolved{TenantID: "t1"})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	r.POST("/x", Middleware(store), func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})
	return r
}

func postX(r *gin.Engine, key, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestMiddlewareMissingKey(t *testing.T) {
	r := newTestRouter(&fakeStore{})
	require.Equal(t, http.StatusBadRequest, postX(r, "", `{}`).Code)
}

func TestMiddlewareNewRunsHandlerAndSaves(t *testing.T) {
	store := &fakeStore{reserveFn: func() (Reservation, error) { return Reservation{State: "new"}, nil }}
	r := newTestRouter(store)

	w := postX(r, "k1", `{"a":1}`)
	require.Equal(t, http.StatusCreated, w.Code)
	require.JSONEq(t, `{"ok":true}`, w.Body.String())
	require.Equal(t, http.StatusCreated, store.saved.status)
	require.JSONEq(t, `{"ok":true}`, string(store.saved.body))
}

func TestMiddlewareReplay(t *testing.T) {
	store := &fakeStore{reserveFn: func() (Reservation, error) {
		return Reservation{State: "replay", StoredStatus: http.StatusCreated, StoredBody: []byte(`{"ok":true}`)}, nil
	}}
	r := newTestRouter(store)

	w := postX(r, "k1", `{}`)
	require.Equal(t, http.StatusCreated, w.Code)
	require.JSONEq(t, `{"ok":true}`, w.Body.String())
}

func TestMiddlewareConflict(t *testing.T) {
	store := &fakeStore{reserveFn: func() (Reservation, error) { return Reservation{State: "conflict"}, nil }}
	r := newTestRouter(store)
	require.Equal(t, http.StatusUnprocessableEntity, postX(r, "k1", `{}`).Code)
}

func TestMiddlewareInflight(t *testing.T) {
	store := &fakeStore{reserveFn: func() (Reservation, error) { return Reservation{State: "inflight"}, nil }}
	r := newTestRouter(store)
	require.Equal(t, http.StatusConflict, postX(r, "k1", `{}`).Code)
}
