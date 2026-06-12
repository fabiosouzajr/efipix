package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/tenantctx"
	tapp "github.com/efipix/pix/internal/tenant/app"
)

type fakeRepo struct{}

func (fakeRepo) TenantByAPIKeyHash(_ context.Context, h string) (string, error) {
	if h == tapp.HashAPIKey("good") {
		return "t1", nil
	}
	return "", apperrs.New(apperrs.KindUnauthorized, "invalid")
}
func (fakeRepo) ResolveAccount(_ context.Context, _, providerID string) (tapp.ResolvedAccount, error) {
	if providerID == "" || providerID == "pdef" {
		return tapp.ResolvedAccount{ProviderID: "pdef", PixKey: "k"}, nil
	}
	return tapp.ResolvedAccount{}, apperrs.New(apperrs.KindValidation, "unknown")
}

func testRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	mw := Middleware(tapp.NewResolver(fakeRepo{}))
	r.GET("/x", mw, func(c *gin.Context) {
		res, _ := tenantctx.From(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"tenant": res.TenantID, "provider": res.ProviderID, "pixkey": res.PixKey})
	})
	return r
}

func TestMiddlewareOK(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Api-Key", "good")
	testRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"tenant":"t1"`)
	require.Contains(t, w.Body.String(), `"provider":"pdef"`)
	require.Contains(t, w.Body.String(), `"pixkey":"k"`)
}

func TestMiddlewareMissingKey(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	testRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewareBadProvider(t *testing.T) {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Api-Key", "good")
	req.Header.Set("X-Provider-Id", "nope")
	testRouter().ServeHTTP(w, req)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
}
