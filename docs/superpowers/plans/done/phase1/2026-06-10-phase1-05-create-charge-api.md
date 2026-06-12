# Phase 1 · File 05 — Create-Charge Use Case, API, Wiring & End-to-End

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-10-phase1-00-overview.md) first. Depends on files [01](2026-06-10-phase1-01-foundation.md)–[04](2026-06-10-phase1-04-charge-aggregate.md).

**Goal:** Wire the persist-first two-phase `CreateImmediateCharge` use case, expose `POST /api/v1/charges` + `GET /api/v1/charges/{id}` with required idempotency, compose everything in `cmd/server`, and prove Phase 1 end-to-end with an integration test (real DB + repos + idempotency, fake `PixProvider`).

---

### Task 1: CreateImmediateCharge use case

**Files:**

- Create: `internal/charge/app/create.go`
- Test: `internal/charge/app/create_test.go`

> Flow ([ADR-0002](../../adr/0002-client-defined-txid-persist-first.md)): build domain charge (CREATED) → `repo.Create` (tx A) → `provider.CreateImmediateCharge` → on success `MarkActive` + `repo.Save` with a `ChargeCreated` outbox event (tx B); on provider error `MarkFailed` + `repo.Save` (no outbox) and return the error. Idempotency is handled by the API layer (Task 3), not here.

- [ ] **Step 1: Write the failing test** (fakes for repo + provider)

```go
package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/charge/domain"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/provider"
)

type fakeRepo struct {
	created   *domain.Charge
	saved     *domain.Charge
	savedOut  []OutboxEvent
	store     map[string]*domain.Charge
}

func newFakeRepo() *fakeRepo { return &fakeRepo{store: map[string]*domain.Charge{}} }

func (f *fakeRepo) Create(_ context.Context, c *domain.Charge) error {
	f.created = c
	cp := *c
	f.store[c.ID] = &cp
	return nil
}
func (f *fakeRepo) Save(_ context.Context, c *domain.Charge, out ...OutboxEvent) error {
	f.saved = c
	f.savedOut = out
	cp := *c
	f.store[c.ID] = &cp
	return nil
}
func (f *fakeRepo) FindByID(_ context.Context, _, id string) (*domain.Charge, error) {
	if c, ok := f.store[id]; ok {
		return c, nil
	}
	return nil, apperrs.New(apperrs.KindNotFound, "nf")
}
func (f *fakeRepo) FindByTxID(_ context.Context, _, _ string) (*domain.Charge, error) {
	return nil, apperrs.New(apperrs.KindNotFound, "nf")
}

type fakeProv struct {
	fail bool
}

func (f *fakeProv) CreateImmediateCharge(_ context.Context, in *provider.ImmediateChargeInput) (*provider.ChargeResult, error) {
	if f.fail {
		return nil, errors.New("efi down")
	}
	return &provider.ChargeResult{Txid: in.Txid, Status: "ATIVA", LocationID: "loc1",
		QRCodeImage: "img", PixPayload: "000201..."}, nil
}
func (f *fakeProv) GetCharge(_ context.Context, _, _ string) (*provider.ChargeResult, error) {
	return nil, nil
}

func cmd() CreateImmediateChargeCmd {
	return CreateImmediateChargeCmd{
		TenantID: "t1", PaymentProviderID: "p1", PixKey: "k@e.com",
		Amount: money.Centavos(1050), ExpirationSeconds: 3600,
	}
}

func TestCreateSuccess(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateImmediateCharge(repo, &fakeProv{})
	c, err := uc.Execute(context.Background(), cmd())
	require.NoError(t, err)
	require.Equal(t, domain.StatusActive, c.Status)
	require.Equal(t, "000201...", c.PixPayload)
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.saved)
	require.Len(t, repo.savedOut, 1)
	require.Equal(t, "ChargeCreated", repo.savedOut[0].Type)
}

func TestCreateProviderFailureMarksFailed(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateImmediateCharge(repo, &fakeProv{fail: true})
	_, err := uc.Execute(context.Background(), cmd())
	require.Error(t, err)
	require.Equal(t, domain.StatusFailed, repo.saved.Status)
	require.Empty(t, repo.savedOut, "no outbox event on failure")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/charge/app/ -run TestCreate`
Expected: FAIL (NewCreateImmediateCharge undefined).

- [ ] **Step 3: Implement** (`internal/charge/app/create.go`)

```go
package app

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/efipix/pix/internal/charge/domain"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/provider"
)

type CreateImmediateChargeCmd struct {
	TenantID          string
	PaymentProviderID string
	PixKey            string
	Amount            money.Centavos
	Description       string
	ExpirationSeconds int
	Payer             domain.Payer
	ExternalReference string
}

type CreateImmediateCharge struct {
	repo ChargeRepository
	prov provider.PixProvider
}

func NewCreateImmediateCharge(repo ChargeRepository, prov provider.PixProvider) *CreateImmediateCharge {
	return &CreateImmediateCharge{repo: repo, prov: prov}
}

func (uc *CreateImmediateCharge) Execute(ctx context.Context, cmd CreateImmediateChargeCmd) (*domain.Charge, error) {
	c, err := domain.NewImmediate(domain.NewImmediateParams{
		TenantID: cmd.TenantID, PaymentProviderID: cmd.PaymentProviderID, PixKey: cmd.PixKey,
		Amount: cmd.Amount, Description: cmd.Description, ExpirationSeconds: cmd.ExpirationSeconds,
		Payer: cmd.Payer, ExternalReference: cmd.ExternalReference,
	})
	if err != nil {
		return nil, err
	}

	// tx A: record intent as CREATED before calling the provider.
	if err := uc.repo.Create(ctx, c); err != nil {
		return nil, err
	}

	res, perr := uc.prov.CreateImmediateCharge(ctx, &provider.ImmediateChargeInput{
		Txid: c.Txid, PaymentProviderID: c.PaymentProviderID, Amount: c.Amount, PixKey: c.PixKey,
		Description: c.Description, ExpirationSeconds: c.ExpirationSeconds,
		PayerDoc: c.Payer.Doc, PayerDocType: c.Payer.DocType, PayerName: c.Payer.Name,
	})
	if perr != nil {
		_ = c.MarkFailed(perr.Error())
		if serr := uc.repo.Save(ctx, c); serr != nil {
			return nil, serr
		}
		// Classify as a provider failure so the API maps it to 502 regardless of the
		// underlying error's kind (the real EfiProvider already wraps as KindProvider).
		return nil, apperrs.Wrap(apperrs.KindProvider, "provider charge creation failed", perr)
	}

	if err := c.MarkActive(res.LocationID, res.QRCodeImage, res.PixPayload); err != nil {
		return nil, err
	}
	evt := OutboxEvent{
		ID: uuid.NewString(), TenantID: c.TenantID, AggregateID: c.ID,
		Type: "ChargeCreated", Payload: chargeCreatedPayload(c),
	}
	if err := uc.repo.Save(ctx, c, evt); err != nil {
		return nil, err
	}
	return c, nil
}

func chargeCreatedPayload(c *domain.Charge) []byte {
	b, _ := json.Marshal(map[string]any{
		"charge_id": c.ID, "txid": c.Txid, "status": string(c.Status), "amount": int64(c.Amount),
	})
	return b
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/charge/app/ -run TestCreate`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/charge/app/create.go internal/charge/app/create_test.go
git commit -m "feat(charge): CreateImmediateCharge use case (persist-first two-phase)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: HTTP DTOs + error mapping

**Files:**
- Create: `internal/charge/api/dto.go`
- Create: `internal/platform/httpx/errors.go`
- Test: `internal/platform/httpx/errors_test.go`

- [ ] **Step 1: Write the failing test** for status mapping (`internal/platform/httpx/errors_test.go`)

```go
package httpx

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

func TestStatusForKind(t *testing.T) {
	require.Equal(t, http.StatusNotFound, StatusFor(apperrs.New(apperrs.KindNotFound, "x")))
	require.Equal(t, http.StatusConflict, StatusFor(apperrs.New(apperrs.KindConflict, "x")))
	require.Equal(t, http.StatusUnprocessableEntity, StatusFor(apperrs.New(apperrs.KindValidation, "x")))
	require.Equal(t, http.StatusUnauthorized, StatusFor(apperrs.New(apperrs.KindUnauthorized, "x")))
	require.Equal(t, http.StatusBadGateway, StatusFor(apperrs.New(apperrs.KindProvider, "x")))
	require.Equal(t, http.StatusInternalServerError, StatusFor(apperrs.New(apperrs.KindUnknown, "x")))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/httpx/`
Expected: FAIL.

- [ ] **Step 3: Implement** (`internal/platform/httpx/errors.go`)

```go
package httpx

import (
	"net/http"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

func StatusFor(err error) int {
	switch apperrs.KindOf(err) {
	case apperrs.KindNotFound:
		return http.StatusNotFound
	case apperrs.KindConflict:
		return http.StatusConflict
	case apperrs.KindValidation:
		return http.StatusUnprocessableEntity
	case apperrs.KindUnauthorized:
		return http.StatusUnauthorized
	case apperrs.KindProvider:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
```

- [ ] **Step 4: Implement DTOs** (`internal/charge/api/dto.go`)

```go
package api

import (
	"github.com/efipix/pix/internal/charge/domain"
)

type createChargeRequest struct {
	Amount            string `json:"amount" binding:"required"` // decimal "10.50"
	Description       string `json:"description"`
	ExpirationSeconds int    `json:"expiration_seconds"`
	Payer             struct {
		Doc     string `json:"doc"`
		DocType string `json:"doc_type"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Phone   string `json:"phone"`
	} `json:"payer"`
	ExternalReference string `json:"external_reference"`
}

type chargeResponse struct {
	ID         string `json:"id"`
	Txid       string `json:"txid"`
	Status     string `json:"status"`
	Amount     string `json:"amount"`
	QRCode     string `json:"qr_code_image"`
	PixPayload string `json:"pix_payload"`
	Location   string `json:"location_id"`
}

func toResponse(c *domain.Charge) chargeResponse {
	return chargeResponse{
		ID: c.ID, Txid: c.Txid, Status: string(c.Status), Amount: c.Amount.String(),
		QRCode: c.QRCodeImage, PixPayload: c.PixPayload, Location: c.LocationID,
	}
}
```

- [ ] **Step 5: Run test to verify it passes + build**

Run: `go test ./internal/platform/httpx/ && go build ./internal/charge/api/`
Expected: PASS; build exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/httpx/ internal/charge/api/dto.go
git commit -m "feat(api): error->status mapping and charge DTOs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Charge handlers with required idempotency

**Files:**
- Create: `internal/charge/api/handler.go`

> Idempotency is enforced by `idempotency.Middleware` ([File 04](2026-06-10-phase1-04-charge-aggregate.md), `internal/platform/idempotency`), registered on `POST /charges` only — required `Idempotency-Key`, sha256 fingerprint, replay/conflict(422)/inflight(409)/new, persists the handler's response for replay. `create()` itself has zero idempotency code: read body → validate → run the use case → respond. Errors map via `httpx.StatusFor`.

- [ ] **Step 1: Implement** (`internal/charge/api/handler.go`)

```go
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	chargeapp "github.com/efipix/pix/internal/charge/app"
	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/httpx"
	"github.com/efipix/pix/internal/platform/idempotency"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/platform/tenantctx"
)

type Handler struct {
	uc   *chargeapp.CreateImmediateCharge
	repo chargeapp.ChargeRepository
}

func NewHandler(uc *chargeapp.CreateImmediateCharge, repo chargeapp.ChargeRepository) *Handler {
	return &Handler{uc: uc, repo: repo}
}

// RegisterRoutes wires the charge endpoints. POST /charges runs behind
// idempotency.Middleware (File 04): required Idempotency-Key, replay on
// retry. GET /charges/:id is a plain read with no idempotency protocol.
func RegisterRoutes(rg gin.IRoutes, h *Handler, idem idempotency.Store) {
	rg.POST("/charges", idempotency.Middleware(idem), h.create)
	rg.GET("/charges/:id", h.get)
}

func (h *Handler) create(c *gin.Context) {
	ctx := c.Request.Context()
	res, _ := tenantctx.From(ctx)

	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	status, body := h.process(ctx, res, raw)
	c.Data(status, "application/json; charset=utf-8", body)
}

// process runs validation + use case, returning the final status and JSON body.
func (h *Handler) process(ctx context.Context, res *tenantctx.Resolved, raw []byte) (int, []byte) {
	var req createChargeRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Amount == "" {
		return http.StatusUnprocessableEntity, mustJSON(gin.H{"error": "invalid request body"})
	}
	amount, err := money.ParseString(req.Amount)
	if err != nil || amount <= 0 {
		return http.StatusUnprocessableEntity, mustJSON(gin.H{"error": "invalid amount"})
	}
	charge, err := h.uc.Execute(ctx, chargeapp.CreateImmediateChargeCmd{
		TenantID: res.TenantID, PaymentProviderID: res.ProviderID, PixKey: res.PixKey,
		Amount: amount, Description: req.Description, ExpirationSeconds: req.ExpirationSeconds,
		Payer: domain.Payer{
			Doc: req.Payer.Doc, DocType: req.Payer.DocType, Name: req.Payer.Name,
			Email: req.Payer.Email, Phone: req.Payer.Phone,
		},
		ExternalReference: req.ExternalReference,
	})
	if err != nil {
		return httpx.StatusFor(err), mustJSON(gin.H{"error": err.Error()})
	}
	return http.StatusCreated, mustJSON(toResponse(charge))
}

func (h *Handler) get(c *gin.Context) {
	ctx := c.Request.Context()
	res, _ := tenantctx.From(ctx)
	charge, err := h.repo.FindByID(ctx, res.TenantID, c.Param("id"))
	if err != nil {
		c.JSON(httpx.StatusFor(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toResponse(charge))
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/charge/api/`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/charge/api/handler.go
git commit -m "feat(api): charge create/get handlers, idempotency via platform middleware

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Wire everything in cmd/server

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Replace `main.go` with the full wiring**

```go
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	chargeapp "github.com/efipix/pix/internal/charge/app"
	chargeapi "github.com/efipix/pix/internal/charge/api"
	chargeinfra "github.com/efipix/pix/internal/charge/infra"
	"github.com/efipix/pix/internal/platform/config"
	"github.com/efipix/pix/internal/platform/db"
	"github.com/efipix/pix/internal/platform/health"
	"github.com/efipix/pix/internal/platform/idempotency"
	"github.com/efipix/pix/internal/platform/logging"
	"github.com/efipix/pix/internal/platform/secrets"
	"github.com/efipix/pix/internal/provider/efi"
	tenantapi "github.com/efipix/pix/internal/tenant/api"
	tenantapp "github.com/efipix/pix/internal/tenant/app"
	tenantinfra "github.com/efipix/pix/internal/tenant/infra"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logging.New(cfg.LogLevel)
	ctx := context.Background()

	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sp, err := secrets.NewEnv()
	if err != nil {
		log.Error("secrets", "err", err)
		os.Exit(1)
	}

	tenantRepo := tenantinfra.New(pool)
	resolver := tenantapp.NewResolver(tenantRepo)
	chargeRepo := chargeinfra.New(pool)
	idem := idempotency.NewPg(pool)
	prov := efi.New(sp, efi.SDKFactory)
	createUC := chargeapp.NewCreateImmediateCharge(chargeRepo, prov)
	chargeHandler := chargeapi.NewHandler(createUC, chargeRepo)

	r := gin.New()
	r.Use(gin.Recovery())
	health.Register(r, pool.Ping)

	v1 := r.Group("/api/v1")
	v1.Use(tenantapi.Middleware(resolver))
	chargeapi.RegisterRoutes(v1, chargeHandler, idem)

	srv := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: r}
	go func() {
		log.Info("listening", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}
```

- [ ] **Step 2: Build**

Run: `go build ./cmd/server`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): wire tenant middleware, charge routes, efi provider

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: End-to-end integration test (real DB + repos + idempotency, fake provider)

**Files:**
- Create: `internal/charge/api/e2e_test.go` (integration)

> Wires the real stack against a testcontainers Postgres seeded with the dev tenant; substitutes a fake `PixProvider` so no EFí call is made. Proves the Phase 1 acceptance criteria.

- [ ] **Step 1: Write the failing e2e test**

```go
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

func boot(t *testing.T, prov provider.PixProvider) *gin.Engine {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
		tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	g := exec.Command("goose", "-dir", "../../../db/migrations", "postgres", dsn, "up")
	g.Stdout, g.Stderr = os.Stdout, os.Stderr
	require.NoError(t, g.Run())
	s := exec.Command("psql", dsn, "-f", "../../../db/seed/dev.sql")
	s.Stdout, s.Stderr = os.Stdout, os.Stderr
	require.NoError(t, s.Run())

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
```

- [ ] **Step 2: Run test to verify it passes** (Docker required)

Run: `go test -tags=integration ./internal/charge/api/`
Expected: PASS (4 tests). 201 ACTIVE; replay returns same txid; missing key → 400; different body → 422; provider failure → 502 and replays 502.

- [ ] **Step 3: Commit**

```bash
git add internal/charge/api/e2e_test.go
git commit -m "test(charge): end-to-end create/replay/conflict/failure with fake provider

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Verify Phase 1 exit criteria

- [ ] **Step 1: Full unit + integration run**

Run:
```bash
go vet ./...
go test -race -cover ./...
go test -race -tags=integration ./...
```
Expected: all green.

- [ ] **Step 2: Coverage gate on domain + app**

Run: `go test -cover ./internal/charge/domain/ ./internal/charge/app/`
Expected: each ≥ 80.0% statements.

- [ ] **Step 3: Manual smoke against EFí homologation** (requires real `EFI_CREDENTIALS` for the dev provider id `22222222-2222-2222-2222-222222222222`)

Run:
```bash
make up && export DATABASE_ADMIN_URL="postgres://pix:pix@localhost:5432/pix?sslmode=disable" && make migrate-up && make seed-dev
# set EFI_CREDENTIALS env for the running app (compose override), then:
curl -sS -X POST localhost:8080/api/v1/charges \
  -H "X-Api-Key: pk_dev_secret" -H "Idempotency-Key: smoke-1" \
  -H "Content-Type: application/json" -d '{"amount":"1.00"}'
```
Expected: `201` with `status":"ACTIVE"`, a non-empty `pix_payload` and `qr_code_image`.

- [ ] **Step 4: Confirm `docs/efi-sdk-review.md` has no remaining `<confirm:…>` placeholders.**

- [ ] **Step 5: Final commit (if any pending docs)**

```bash
git add -A
git commit -m "chore(phase1): complete immediate charge slice

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>" || true
```

---

## Phase 1 complete

All five files implemented + the [overview exit checklist](2026-06-10-phase1-00-overview.md#phase-1-exit-checklist-verify-after-file-05) satisfied. Immediate Pix charges work end-to-end, multi-tenant, idempotent, persist-first with audit. Phases 2–6 each get their own plan set following this split-by-file pattern.
