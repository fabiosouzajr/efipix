# Phase 1 · File 02 — Tenant, Provider Accounts, Resolution & API-Key Auth

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-10-phase1-00-overview.md) first. Depends on [01-foundation](2026-06-10-phase1-01-foundation.md).

**Goal:** Persist tenants, their EFí account configs (`payment_providers`), and `pix_keys`; authenticate client apps by API key; resolve every request into a `(tenant, provider)` context via Gin middleware, enforced by RLS.

**Note on data access:** Phase 1 repos use **direct pgx queries** for executability. `sqlc.yaml` is configured (file 01) and can generate typed queries incrementally later; not blocking. **Addendum to spec §6:** an `api_keys` table is added for client authentication (the spec's table list was non-exhaustive, like `webhook_subscriptions`).

---

### Task 1: Tenant/provider schema + RLS + dev seed

**Files:**
- Create: `db/migrations/00002_tenants.sql`
- Create: `db/seed/dev.sql`
- Modify: `Makefile` (add `seed-dev` target)

> RLS: app role `pix_app` is non-owner with FORCE RLS. `tenants` policy lets a tenant read only its own row (`id = current_tenant_id()`). `api_keys` is read during auth via the admin pool (pre-tenant-context), so it does not depend on the GUC.

- [ ] **Step 1: Write the migration** (`db/migrations/00002_tenants.sql`)

```sql
-- +goose Up
CREATE TABLE tenants (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text NOT NULL,
  status      text NOT NULL DEFAULT 'active',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz
);

CREATE TABLE api_keys (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  key_hash    text NOT NULL UNIQUE,           -- sha256 hex of the raw key
  name        text NOT NULL DEFAULT '',
  status      text NOT NULL DEFAULT 'active',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);

CREATE TABLE payment_providers (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  provider      text NOT NULL DEFAULT 'efi',
  account_label text NOT NULL DEFAULT '',
  status        text NOT NULL DEFAULT 'active',
  is_default    boolean NOT NULL DEFAULT false,
  webhook_config jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX uq_provider_default ON payment_providers(tenant_id) WHERE is_default;
CREATE INDEX idx_providers_tenant ON payment_providers(tenant_id);

CREATE TABLE pix_keys (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  payment_provider_id uuid NOT NULL REFERENCES payment_providers(id) ON DELETE RESTRICT,
  key                 text NOT NULL,
  key_type            text NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_pix_keys_provider ON pix_keys(payment_provider_id);

-- RLS
ALTER TABLE tenants            ENABLE ROW LEVEL SECURITY; ALTER TABLE tenants            FORCE ROW LEVEL SECURITY;
ALTER TABLE api_keys           ENABLE ROW LEVEL SECURITY; ALTER TABLE api_keys           FORCE ROW LEVEL SECURITY;
ALTER TABLE payment_providers  ENABLE ROW LEVEL SECURITY; ALTER TABLE payment_providers  FORCE ROW LEVEL SECURITY;
ALTER TABLE pix_keys           ENABLE ROW LEVEL SECURITY; ALTER TABLE pix_keys           FORCE ROW LEVEL SECURITY;

CREATE POLICY p_tenants  ON tenants           USING (id = current_tenant_id());
CREATE POLICY p_apikeys  ON api_keys          USING (tenant_id = current_tenant_id());
CREATE POLICY p_providers ON payment_providers USING (tenant_id = current_tenant_id());
CREATE POLICY p_pixkeys  ON pix_keys          USING (tenant_id = current_tenant_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON tenants, api_keys, payment_providers, pix_keys TO pix_app;

-- +goose Down
DROP TABLE pix_keys; DROP TABLE payment_providers; DROP TABLE api_keys; DROP TABLE tenants;
```

- [ ] **Step 2: Write the dev seed** (`db/seed/dev.sql`) — fixed IDs + a known API key `pk_dev_secret` (hashed via pgcrypto). For local/dev/e2e only.

```sql
INSERT INTO tenants (id, name) VALUES
  ('11111111-1111-1111-1111-111111111111', 'Dev Tenant')
ON CONFLICT (id) DO NOTHING;

INSERT INTO payment_providers (id, tenant_id, provider, account_label, is_default) VALUES
  ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', 'efi', 'dev-efi', true)
ON CONFLICT (id) DO NOTHING;

INSERT INTO pix_keys (id, tenant_id, payment_provider_id, key, key_type) VALUES
  ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111',
   '22222222-2222-2222-2222-222222222222', 'dev-pix-key@example.com', 'email')
ON CONFLICT (id) DO NOTHING;

INSERT INTO api_keys (tenant_id, key_hash, name) VALUES
  ('11111111-1111-1111-1111-111111111111', encode(digest('pk_dev_secret', 'sha256'), 'hex'), 'dev')
ON CONFLICT (key_hash) DO NOTHING;
```

- [ ] **Step 3: Add the `seed-dev` Make target**

Add to `Makefile`:
```makefile
seed-dev:
	psql "$$DATABASE_ADMIN_URL" -f db/seed/dev.sql
```

- [ ] **Step 4: Apply and verify**

Run:
```bash
export DATABASE_ADMIN_URL="postgres://pix:pix@localhost:5432/pix?sslmode=disable"
make migrate-up && make seed-dev
psql "$DATABASE_ADMIN_URL" -c "SELECT count(*) FROM api_keys"
```
Expected: migration OK; seed runs; count `1`.

- [ ] **Step 5: Commit**

```bash
git add db/migrations/00002_tenants.sql db/seed/dev.sql Makefile
git commit -m "feat(tenant): tenants/api_keys/providers/pix_keys schema with RLS + dev seed

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Tenant domain types

**Files:**
- Create: `internal/tenant/domain/domain.go`

> Pure structs (per locked signatures). No behaviour yet, so no unit test — covered by the repo integration test in Task 4.

- [ ] **Step 1: Implement**

```go
package domain

type Tenant struct {
	ID     string
	Name   string
	Status string
}

type PaymentProvider struct {
	ID                string
	TenantID          string
	Provider          string
	AccountLabel      string
	Status            string
	IsDefault         bool
	WebhookConfig     []byte
}

type PixKey struct {
	ID                string
	TenantID          string
	PaymentProviderID string
	Key               string
	KeyType           string
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/tenant/...`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/tenant/domain/
git commit -m "feat(tenant): domain types

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Tenant context helpers + API-key hashing

**Files:**
- Create: `internal/platform/tenantctx/tenantctx.go`
- Create: `internal/tenant/app/apikey.go`
- Test: `internal/platform/tenantctx/tenantctx_test.go`
- Test: `internal/tenant/app/apikey_test.go`

- [ ] **Step 1: Write the failing tests**

`internal/platform/tenantctx/tenantctx_test.go`:
```go
package tenantctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	ctx := With(context.Background(), &Resolved{TenantID: "t1", ProviderID: "p1", PixKey: "k1"})
	got, ok := From(ctx)
	require.True(t, ok)
	require.Equal(t, "t1", got.TenantID)
	require.Equal(t, "p1", got.ProviderID)
	require.Equal(t, "k1", got.PixKey)

	_, ok = From(context.Background())
	require.False(t, ok)
}
```

`internal/tenant/app/apikey_test.go`:
```go
package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashAPIKey(t *testing.T) {
	require.Len(t, HashAPIKey("pk_dev_secret"), 64) // sha256 hex = 64 chars
	require.Equal(t, HashAPIKey("a"), HashAPIKey("a"))
	require.NotEqual(t, HashAPIKey("a"), HashAPIKey("b"))
}
```

> The exact hash value (matching the DB `digest()` seed) is asserted by the integration test in Task 4. Here we assert length and determinism.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/platform/tenantctx/ ./internal/tenant/app/`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement `tenantctx`**

```go
package tenantctx

import "context"

type Resolved struct {
	TenantID   string
	ProviderID string
	PixKey     string
}

type ctxKey struct{}

func With(ctx context.Context, r *Resolved) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

func From(ctx context.Context) (*Resolved, bool) {
	r, ok := ctx.Value(ctxKey{}).(*Resolved)
	return r, ok
}
```

- [ ] **Step 4: Implement `HashAPIKey`** (`internal/tenant/app/apikey.go`)

```go
package app

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashAPIKey returns the sha256 hex of a raw API key, matching the DB digest().
func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 5: Simplify the apikey test** — replace the placeholder first assertion. Final `apikey_test.go`:

```go
package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashAPIKey(t *testing.T) {
	require.Len(t, HashAPIKey("pk_dev_secret"), 64) // sha256 hex
	require.Equal(t, HashAPIKey("a"), HashAPIKey("a"))
	require.NotEqual(t, HashAPIKey("a"), HashAPIKey("b"))
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/platform/tenantctx/ ./internal/tenant/app/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/tenantctx/ internal/tenant/app/apikey.go internal/tenant/app/apikey_test.go
git commit -m "feat(tenant): tenant context helpers and api-key hashing

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Tenant repository

**Files:**
- Create: `internal/tenant/app/repository.go` (interface)
- Create: `internal/tenant/infra/repository.go` (pgx impl)
- Test: `internal/tenant/infra/repository_test.go` (integration)

- [ ] **Step 1: Define the repository interface** (`internal/tenant/app/repository.go`)

```go
package app

import "context"

type Repository interface {
	// TenantByAPIKeyHash resolves an active tenant id from an API key hash (auth path; no tenant ctx).
	TenantByAPIKeyHash(ctx context.Context, keyHash string) (tenantID string, err error)
	// ResolveAccount resolves the acting payment provider and its pix key for a
	// tenant in one round trip. If explicitProviderID == "", it resolves the
	// tenant's active default provider; otherwise it validates that
	// explicitProviderID belongs to the tenant and is active. Returns
	// KindValidation if no provider resolves, or if the resolved provider has
	// no pix key.
	ResolveAccount(ctx context.Context, tenantID, explicitProviderID string) (ResolvedAccount, error)
}

// ResolvedAccount is the join result of account resolution: the acting
// payment provider and the pix key new Charges should address.
type ResolvedAccount struct {
	ProviderID string
	PixKey     string
}
```

- [ ] **Step 2: Implement the pgx repository** (`internal/tenant/infra/repository.go`)

```go
package infra

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/db"
	"github.com/efipix/pix/internal/tenant/app"
)

type Repository struct{ pool *db.Pool }

func New(pool *db.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) TenantByAPIKeyHash(ctx context.Context, keyHash string) (string, error) {
	var tenantID string
	// Auth path: admin pool (no tenant GUC yet).
	err := r.pool.WithAdminTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT k.tenant_id FROM api_keys k
			 JOIN tenants t ON t.id = k.tenant_id
			 WHERE k.key_hash = $1 AND k.status = 'active'
			   AND t.status = 'active' AND t.deleted_at IS NULL`, keyHash,
		).Scan(&tenantID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperrs.New(apperrs.KindUnauthorized, "invalid api key")
	}
	if err != nil {
		return "", err
	}
	return tenantID, nil
}

// ResolveAccount joins payment_providers to pix_keys in one query: when
// explicitProviderID == "", it matches the tenant's active default provider;
// otherwise it matches that provider id, validating it belongs to the tenant
// and is active. On no match, a same-tx existence check disambiguates "no
// such provider" from "provider has no pix key".
func (r *Repository) ResolveAccount(ctx context.Context, tenantID, explicitProviderID string) (app.ResolvedAccount, error) {
	var acct app.ResolvedAccount
	err := r.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(ctx,
			`SELECT pp.id, pk.key
			 FROM payment_providers pp
			 JOIN pix_keys pk ON pk.payment_provider_id = pp.id AND pk.tenant_id = pp.tenant_id
			 WHERE pp.tenant_id = $1 AND pp.status = 'active'
			   AND (($2 = '' AND pp.is_default) OR pp.id::text = $2)
			 ORDER BY pk.created_at ASC, pk.id ASC
			 LIMIT 1`,
			tenantID, explicitProviderID,
		).Scan(&acct.ProviderID, &acct.PixKey)
		if scanErr == nil {
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}

		// No joined row: disambiguate "no such provider" from "provider has no pix key".
		var providerID string
		existsErr := tx.QueryRow(ctx,
			`SELECT id FROM payment_providers
			 WHERE tenant_id = $1 AND status = 'active'
			   AND (($2 = '' AND is_default) OR id::text = $2)`,
			tenantID, explicitProviderID,
		).Scan(&providerID)
		switch {
		case errors.Is(existsErr, pgx.ErrNoRows):
			if explicitProviderID == "" {
				return apperrs.New(apperrs.KindValidation, "no default provider for tenant")
			}
			return apperrs.New(apperrs.KindValidation, "unknown or inactive provider")
		case existsErr != nil:
			return existsErr
		default:
			return apperrs.New(apperrs.KindValidation, "no pix key for provider")
		}
	})
	if err != nil {
		return app.ResolvedAccount{}, err
	}
	return acct, nil
}
```

- [ ] **Step 3: Write the failing integration test** (`internal/tenant/infra/repository_test.go`)

```go
//go:build integration

package infra

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/db"
	tapp "github.com/efipix/pix/internal/tenant/app"
)

func setup(t *testing.T) *db.Pool {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
		tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Run migrations + seed as admin (the superuser created by testcontainers IS the owner).
	run(t, "goose", "-dir", "../../../db/migrations", "postgres", dsn, "up")
	run(t, "psql", dsn, "-f", "../../../db/seed/dev.sql")

	t.Setenv("DATABASE_ADMIN_URL", dsn) // admin path uses the same superuser here
	pool, err := db.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func run(t *testing.T, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "command %s failed", name)
}

func TestResolveChain(t *testing.T) {
	ctx := context.Background()
	pool := setup(t)
	r := New(pool)

	tenantID, err := r.TenantByAPIKeyHash(ctx, tapp.HashAPIKey("pk_dev_secret"))
	require.NoError(t, err)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", tenantID)

	_, err = r.TenantByAPIKeyHash(ctx, tapp.HashAPIKey("wrong"))
	require.Error(t, err)

	acct, err := r.ResolveAccount(ctx, tenantID, "")
	require.NoError(t, err)
	require.Equal(t, "22222222-2222-2222-2222-222222222222", acct.ProviderID)
	require.Equal(t, "dev-pix-key@example.com", acct.PixKey)

	acct2, err := r.ResolveAccount(ctx, tenantID, acct.ProviderID)
	require.NoError(t, err)
	require.Equal(t, acct, acct2)

	_, err = r.ResolveAccount(ctx, tenantID, "00000000-0000-0000-0000-000000000000")
	require.Equal(t, apperrs.KindValidation, apperrs.KindOf(err))
}
```

> The test invokes `goose` and `psql` CLIs (present in CI per file 01 setup). The hash assertion proves `HashAPIKey` matches the DB `digest()` seed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -tags=integration ./internal/tenant/infra/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tenant/app/repository.go internal/tenant/infra/
git commit -m "feat(tenant): pgx repository for tenant/provider/pixkey resolution

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Resolver service (tenant + provider)

**Files:**
- Create: `internal/tenant/app/resolver.go`
- Test: `internal/tenant/app/resolver_test.go`

- [ ] **Step 1: Write the failing test** (uses a fake Repository — unit, no DB)

```go
package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

type fakeRepo struct {
	tenantID string
	accounts map[string]ResolvedAccount // keyed by explicit providerID; "" = default
}

func (f *fakeRepo) TenantByAPIKeyHash(_ context.Context, h string) (string, error) {
	if h == HashAPIKey("good") {
		return f.tenantID, nil
	}
	return "", apperrs.New(apperrs.KindUnauthorized, "invalid api key")
}
func (f *fakeRepo) ResolveAccount(_ context.Context, _, providerID string) (ResolvedAccount, error) {
	if a, ok := f.accounts[providerID]; ok {
		return a, nil
	}
	return ResolvedAccount{}, apperrs.New(apperrs.KindValidation, "unknown or inactive provider")
}

func newFake() *fakeRepo {
	return &fakeRepo{
		tenantID: "t1",
		accounts: map[string]ResolvedAccount{
			"":   {ProviderID: "pdef", PixKey: "k-def"},
			"pX": {ProviderID: "pX", PixKey: "k-x"},
		},
	}
}

func TestResolveDefaultProvider(t *testing.T) {
	r := NewResolver(newFake())
	res, err := r.Resolve(context.Background(), "good", "")
	require.NoError(t, err)
	require.Equal(t, "t1", res.TenantID)
	require.Equal(t, "pdef", res.ProviderID)
	require.Equal(t, "k-def", res.PixKey)
}

func TestResolveExplicitProvider(t *testing.T) {
	r := NewResolver(newFake())
	res, err := r.Resolve(context.Background(), "good", "pX")
	require.NoError(t, err)
	require.Equal(t, "pX", res.ProviderID)
	require.Equal(t, "k-x", res.PixKey)
}

func TestResolveBadKey(t *testing.T) {
	r := NewResolver(newFake())
	_, err := r.Resolve(context.Background(), "bad", "")
	require.Equal(t, apperrs.KindUnauthorized, apperrs.KindOf(err))
}

func TestResolveUnknownExplicitProvider(t *testing.T) {
	r := NewResolver(newFake())
	_, err := r.Resolve(context.Background(), "good", "nope")
	require.Equal(t, apperrs.KindValidation, apperrs.KindOf(err))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tenant/app/ -run TestResolve`
Expected: FAIL (NewResolver undefined).

- [ ] **Step 3: Implement** (`internal/tenant/app/resolver.go`)

```go
package app

import (
	"context"

	"github.com/efipix/pix/internal/platform/tenantctx"
)

type Resolver struct{ repo Repository }

func NewResolver(repo Repository) *Resolver { return &Resolver{repo: repo} }

// Resolve maps a raw API key (+ optional explicit provider id) to the acting
// tenant/provider/pix-key context.
func (r *Resolver) Resolve(ctx context.Context, rawAPIKey, explicitProviderID string) (*tenantctx.Resolved, error) {
	tenantID, err := r.repo.TenantByAPIKeyHash(ctx, HashAPIKey(rawAPIKey))
	if err != nil {
		return nil, err
	}
	acct, err := r.repo.ResolveAccount(ctx, tenantID, explicitProviderID)
	if err != nil {
		return nil, err
	}
	return &tenantctx.Resolved{TenantID: tenantID, ProviderID: acct.ProviderID, PixKey: acct.PixKey}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tenant/app/ -run TestResolve`
Expected: PASS (4 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/tenant/app/resolver.go internal/tenant/app/resolver_test.go
git commit -m "feat(tenant): resolver mapping api key + provider to tenant context

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Gin auth + resolution middleware

**Files:**
- Create: `internal/tenant/api/middleware.go`
- Test: `internal/tenant/api/middleware_test.go`

> Reads the raw key from `X-Api-Key` (or `Authorization: ApiKey <key>`), optional explicit provider from `X-Provider-Id`, resolves, and stores `*tenantctx.Resolved` in the request context. On failure: 401 (bad/missing key) or 422 (bad provider).

- [ ] **Step 1: Write the failing test** (uses the fake repo + resolver from Task 5)

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tenant/api/`
Expected: FAIL (Middleware undefined).

- [ ] **Step 3: Implement** (`internal/tenant/api/middleware.go`)

```go
package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/tenantctx"
	tapp "github.com/efipix/pix/internal/tenant/app"
)

func Middleware(resolver *tapp.Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("X-Api-Key")
		if raw == "" {
			if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "ApiKey ") {
				raw = strings.TrimPrefix(h, "ApiKey ")
			}
		}
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing api key"})
			return
		}
		res, err := resolver.Resolve(c.Request.Context(), raw, c.GetHeader("X-Provider-Id"))
		if err != nil {
			switch apperrs.KindOf(err) {
			case apperrs.KindUnauthorized:
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			case apperrs.KindValidation:
				c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			default:
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "resolution failed"})
			}
			return
		}
		c.Request = c.Request.WithContext(tenantctx.With(c.Request.Context(), res))
		c.Next()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tenant/api/`
Expected: PASS (3 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/tenant/api/
git commit -m "feat(tenant): gin auth + tenant/provider resolution middleware

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## File 02 exit criteria

- [ ] `go test ./...` green; `go test -tags=integration ./internal/tenant/...` green (Docker).
- [ ] API key `pk_dev_secret` resolves to the dev tenant + default provider, including its `PixKey`.
- [ ] Middleware sets `tenantctx.Resolved` (with `PixKey`) and returns 401/422 on bad key/provider.
- [ ] RLS proven: `TenantByAPIKeyHash` works via admin pool; tenant-scoped reads work via `WithTenantTx`.

Proceed to [03-secrets-efi-provider](2026-06-10-phase1-03-secrets-efi-provider.md).
