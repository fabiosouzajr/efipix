//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	chargeapi "github.com/efipix/pix/internal/charge/api"
	chargeapp "github.com/efipix/pix/internal/charge/app"
	chargeinfra "github.com/efipix/pix/internal/charge/infra"
	"github.com/efipix/pix/internal/platform/db"
	"github.com/efipix/pix/internal/platform/idempotency"
	"github.com/efipix/pix/internal/provider"
	tenantapi "github.com/efipix/pix/internal/tenant/api"
	tenantapp "github.com/efipix/pix/internal/tenant/app"
	tenantinfra "github.com/efipix/pix/internal/tenant/infra"
)

type fakeProv struct{ fail bool }

func (f *fakeProv) CreateImmediateCharge(_ context.Context, in *provider.ImmediateChargeInput) (*provider.ChargeResult, error) {
	if f.fail {
		return nil, &providerErr{}
	}
	return &provider.ChargeResult{Txid: in.Txid, Status: "ATIVA", LocationID: "loc1", QRCodeImage: "img", PixPayload: "000201..."}, nil
}
func (f *fakeProv) GetCharge(context.Context, string, string) (*provider.ChargeResult, error) { return nil, nil }

type providerErr struct{}

func (providerErr) Error() string { return "efi down" }

func run(t *testing.T, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "command %s failed", name)
}

func boot(t *testing.T, prov provider.PixProvider) *gin.Engine {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
		tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	run(t, "goose", "-dir", "../../../db/migrations", "postgres", dsn, "up")
	run(t, "psql", dsn, "-f", "../../../db/seed/dev.sql")

	t.Setenv("DATABASE_ADMIN_URL", dsn)
	pool, err := db.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	tenantRepo := tenantinfra.New(pool)
	chargeRepo := chargeinfra.New(pool)
	uc := chargeapp.NewCreateImmediateCharge(chargeRepo, prov)
	h := chargeapi.NewHandler(uc, chargeRepo)
	idem := idempotency.NewPg(pool)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(tenantapi.Middleware(tenantapp.NewResolver(tenantRepo)))
	chargeapi.RegisterRoutes(v1, h, idem)
	return r
}

func post(r *gin.Engine, key, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/charges", bytes.NewBufferString(body))
	req.Header.Set("X-Api-Key", "pk_dev_secret")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestE2ECreateActiveAndReplay(t *testing.T) {
	r := boot(t, &fakeProv{})
	body := `{"amount":"10.50","description":"test"}`

	w := post(r, "key-1", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ACTIVE", resp["status"])
	require.Equal(t, "10.50", resp["amount"])
	require.NotEmpty(t, resp["txid"])
	firstTxid := resp["txid"]

	// Replay: same key + body returns the same charge.
	w2 := post(r, "key-1", body)
	require.Equal(t, http.StatusCreated, w2.Code)
	var resp2 map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
	require.Equal(t, firstTxid, resp2["txid"])

	// GET the charge.
	id := resp["id"].(string)
	gw := httptest.NewRecorder()
	greq, _ := http.NewRequest(http.MethodGet, "/api/v1/charges/"+id, nil)
	greq.Header.Set("X-Api-Key", "pk_dev_secret")
	r.ServeHTTP(gw, greq)
	require.Equal(t, http.StatusOK, gw.Code)
}

func TestE2EMissingIdempotencyKey(t *testing.T) {
	r := boot(t, &fakeProv{})
	w := post(r, "", `{"amount":"5.00"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestE2EConflictOnDifferentBody(t *testing.T) {
	r := boot(t, &fakeProv{})
	require.Equal(t, http.StatusCreated, post(r, "k", `{"amount":"1.00"}`).Code)
	require.Equal(t, http.StatusUnprocessableEntity, post(r, "k", `{"amount":"2.00"}`).Code)
}

func TestE2EProviderFailureRecordsFailed(t *testing.T) {
	r := boot(t, &fakeProv{fail: true})
	w := post(r, "kf", `{"amount":"3.00"}`)
	require.Equal(t, http.StatusBadGateway, w.Code)
	// Replay returns the same recorded failure.
	require.Equal(t, http.StatusBadGateway, post(r, "kf", `{"amount":"3.00"}`).Code)
}
