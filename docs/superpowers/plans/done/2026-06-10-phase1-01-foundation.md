# Phase 1 · File 01 — Foundation

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-10-phase1-00-overview.md) first — it locks the module path, conventions, and shared signatures.

**Goal:** Empty repo → a bootable Gin service with config, structured logging, a pgx pool with an RLS-aware transaction helper, goose migration tooling, health endpoints, docker-compose (Postgres/Redis/RabbitMQ/nginx mTLS proxy), and CI.

**Prerequisites:** Go 1.24+, Docker, `goose` (`go install github.com/pressly/goose/v3/cmd/goose@latest`), `sqlc` (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest`), `golangci-lint`.

---

## Task 1: Module skeleton + tooling config

**Files:**

- Create: `go.mod`, `.gitignore`, `Makefile`, `sqlc.yaml`, `.golangci.yml`
- Create dirs (with `.gitkeep`): `cmd/server/`, `internal/platform/`, `db/migrations/`, `db/queries/`, `deploy/compose/initdb/`

- [ ] **Step 1: Init module and directories**

```bash
cd /home/fj/git/efipix
go mod init github.com/efipix/pix
mkdir -p cmd/server internal/platform db/migrations db/queries deploy/compose/initdb deploy/docker
touch internal/platform/.gitkeep db/queries/.gitkeep
```

- [ ] **Step 2: Write `.gitignore`**

```gitignore
/bin/
*.env
.env
coverage.out
/tmp/
*.pem
*.p12
```

- [ ] **Step 3: Write `sqlc.yaml`**

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "db/queries"
    schema: "db/migrations"
    gen:
      go:
        package: "sqlcgen"
        out: "internal/platform/sqlcgen"
        sql_package: "pgx/v5"
        emit_pointers_for_null_types: true
```

- [ ] **Step 4: Write `.golangci.yml`**

```yaml
run:
  timeout: 5m
linters:
  enable:
    - gofmt
    - govet
    - staticcheck
    - errcheck
    - ineffassign
    - unused
    - misspell
```

- [ ] **Step 5: Write `Makefile`**

```makefile
.PHONY: build test test-int lint migrate-up migrate-down sqlc up down
build:
 go build -o bin/server ./cmd/server
test:
 go test -race -cover ./...
test-int:
 go test -race -tags=integration ./...
lint:
 golangci-lint run
sqlc:
 sqlc generate
migrate-up:
 goose -dir db/migrations postgres "$$DATABASE_ADMIN_URL" up
migrate-down:
 goose -dir db/migrations postgres "$$DATABASE_ADMIN_URL" down
up:
 docker compose -f deploy/compose/docker-compose.yml up -d
down:
 docker compose -f deploy/compose/docker-compose.yml down -v
```

- [ ] **Step 6: Add core dependencies**

```bash
go get github.com/jackc/pgx/v5@latest
go get github.com/jackc/pgx/v5/pgxpool@latest
go get github.com/google/uuid@latest
go get github.com/gin-gonic/gin@latest
go get github.com/stretchr/testify@latest
go get github.com/redis/go-redis/v9@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
go mod tidy
```

- [ ] **Step 7: Commit**

```bash
git init -q 2>/dev/null; git add -A
git commit -m "chore: scaffold go module, tooling, and project layout

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: `money.Centavos`

**Files:**

- Create: `internal/platform/money/money.go`
- Test: `internal/platform/money/money_test.go`

- [ ] **Step 1: Write the failing test**

```go
package money

import "testing"
import "github.com/stretchr/testify/require"

func TestCentavosString(t *testing.T) {
 require.Equal(t, "10.50", Centavos(1050).String())
 require.Equal(t, "0.05", Centavos(5).String())
 require.Equal(t, "100.00", Centavos(10000).String())
}

func TestParseString(t *testing.T) {
 c, err := ParseString("10.50")
 require.NoError(t, err)
 require.Equal(t, Centavos(1050), c)

 c, err = ParseString("0.05")
 require.NoError(t, err)
 require.Equal(t, Centavos(5), c)

 _, err = ParseString("abc")
 require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/money/`
Expected: FAIL (undefined: Centavos / ParseString).

- [ ] **Step 3: Implement**

```go
package money

import (
 "fmt"
 "strconv"
 "strings"
)

// Centavos is an integer amount in BRL cents.
type Centavos int64

// String renders as a decimal "reais.centavos" string for the EFí API.
func (c Centavos) String() string {
 return fmt.Sprintf("%d.%02d", c/100, abs(int64(c))%100)
}

// ParseString parses "10.50" into Centavos(1050).
func ParseString(s string) (Centavos, error) {
 parts := strings.SplitN(strings.TrimSpace(s), ".", 2)
 reais, err := strconv.ParseInt(parts[0], 10, 64)
 if err != nil {
  return 0, fmt.Errorf("money: invalid amount %q: %w", s, err)
 }
 var cents int64
 if len(parts) == 2 {
  frac := (parts[1] + "00")[:2]
  cents, err = strconv.ParseInt(frac, 10, 64)
  if err != nil {
   return 0, fmt.Errorf("money: invalid fraction %q: %w", s, err)
  }
 }
 return Centavos(reais*100 + cents), nil
}

func abs(x int64) int64 {
 if x < 0 {
  return -x
 }
 return x
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/money/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/money/
git commit -m "feat(money): centavos type with string parse/format

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: Typed errors

**Files:**

- Create: `internal/platform/errors/errors.go`
- Test: `internal/platform/errors/errors_test.go`

- [ ] **Step 1: Write the failing test**

```go
package errors

import (
 "errors"
 "testing"

 "github.com/stretchr/testify/require"
)

func TestKindOf(t *testing.T) {
 e := New(KindNotFound, "missing")
 require.Equal(t, KindNotFound, KindOf(e))
 require.Equal(t, "missing", e.Error())

 wrapped := Wrap(KindConflict, "dup", errors.New("pg: unique"))
 require.Equal(t, KindConflict, KindOf(wrapped))
 require.Contains(t, wrapped.Error(), "pg: unique")

 require.Equal(t, KindUnknown, KindOf(errors.New("plain")))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/errors/`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement**

```go
package errors

import (
 "errors"
 "fmt"
)

type Kind int

const (
 KindUnknown Kind = iota
 KindNotFound
 KindConflict
 KindValidation
 KindUnauthorized
 KindProvider
)

type Error struct {
 Kind Kind
 Msg  string
 Err  error
}

func (e *Error) Error() string {
 if e.Err != nil {
  return fmt.Sprintf("%s: %v", e.Msg, e.Err)
 }
 return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

func New(kind Kind, msg string) *Error            { return &Error{Kind: kind, Msg: msg} }
func Wrap(kind Kind, msg string, err error) *Error { return &Error{Kind: kind, Msg: msg, Err: err} }

func KindOf(err error) Kind {
 var e *Error
 if errors.As(err, &e) {
  return e.Kind
 }
 return KindUnknown
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/errors/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/errors/
git commit -m "feat(errors): typed error kinds with KindOf

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: Config loader

**Files:**

- Create: `internal/platform/config/config.go`
- Test: `internal/platform/config/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
package config

import (
 "testing"

 "github.com/stretchr/testify/require"
)

func TestLoadDefaultsAndRequired(t *testing.T) {
 t.Setenv("DATABASE_URL", "postgres://app:pw@localhost:5432/pix")
 c, err := Load()
 require.NoError(t, err)
 require.Equal(t, "postgres://app:pw@localhost:5432/pix", c.DatabaseURL)
 require.Equal(t, "8080", c.HTTPPort) // default
 require.Equal(t, "info", c.LogLevel) // default
}

func TestLoadMissingRequired(t *testing.T) {
 t.Setenv("DATABASE_URL", "")
 _, err := Load()
 require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/config/`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
package config

import (
 "fmt"
 "os"
)

type Config struct {
 DatabaseURL      string
 DatabaseAdminURL string
 RedisURL         string
 HTTPPort         string
 LogLevel         string
 EfiSandbox       bool
}

func Load() (*Config, error) {
 c := &Config{
  DatabaseURL:      os.Getenv("DATABASE_URL"),
  DatabaseAdminURL: os.Getenv("DATABASE_ADMIN_URL"),
  RedisURL:         getDefault("REDIS_URL", "redis://localhost:6379/0"),
  HTTPPort:         getDefault("HTTP_PORT", "8080"),
  LogLevel:         getDefault("LOG_LEVEL", "info"),
  EfiSandbox:       os.Getenv("EFI_SANDBOX") != "false",
 }
 if c.DatabaseURL == "" {
  return nil, fmt.Errorf("config: DATABASE_URL is required")
 }
 return c, nil
}

func getDefault(key, def string) string {
 if v := os.Getenv(key); v != "" {
  return v
 }
 return def
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/config/
git commit -m "feat(config): env-based config loader

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: Structured logging + PII masking

**Files:**

- Create: `internal/platform/logging/logging.go`
- Test: `internal/platform/logging/logging_test.go`

- [ ] **Step 1: Write the failing test**

```go
package logging

import (
 "testing"

 "github.com/stretchr/testify/require"
)

func TestMaskDoc(t *testing.T) {
 require.Equal(t, "***.***.789-**", MaskDoc("12345678900"))   // cpf 11 digits
 require.Equal(t, "**.***.***/4567-**", MaskDoc("12345678000145")) // cnpj 14 digits
 require.Equal(t, "", MaskDoc(""))
 require.Equal(t, "****", MaskDoc("ab")) // too short -> fully masked
}

func TestNewReturnsLogger(t *testing.T) {
 require.NotNil(t, New("info"))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/logging/`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
package logging

import (
 "log/slog"
 "os"
)

// New returns a JSON slog.Logger at the given level.
func New(level string) *slog.Logger {
 lvl := slog.LevelInfo
 switch level {
 case "debug":
  lvl = slog.LevelDebug
 case "warn":
  lvl = slog.LevelWarn
 case "error":
  lvl = slog.LevelError
 }
 h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
 return slog.New(h)
}

// MaskDoc masks a CPF/CNPJ for logs, never emitting the full document.
func MaskDoc(doc string) string {
 switch len(doc) {
 case 11: // CPF: ***.***.NNN-**
  return "***.***." + doc[6:9] + "-**"
 case 14: // CNPJ: **.***.***/NNNN-**
  return "**.***.***/" + doc[8:12] + "-**"
 case 0:
  return ""
 default:
  return "****"
 }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/logging/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/logging/
git commit -m "feat(logging): json slog logger and CPF/CNPJ masking

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: Bootstrap migration + roles + RLS GUC convention

**Files:**

- Create: `deploy/compose/initdb/00-roles.sql`
- Create: `db/migrations/00001_bootstrap.sql`

> RLS strategy ([ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md)): app connects as non-owner `pix_app` with FORCE RLS; migrations run as owner `pix` via `DATABASE_ADMIN_URL`. Tenant scoping uses the tx-local GUC `app.tenant_id`, set with `set_config('app.tenant_id', $1, true)` (NOT `SET LOCAL`, which cannot be parameterized). Policies read `current_setting('app.tenant_id', true)`.

- [ ] **Step 1: Write the roles init script** (`deploy/compose/initdb/00-roles.sql`, run once by Postgres on first boot)

```sql
-- Owner/migrator role is the compose superuser "pix" (POSTGRES_USER).
-- Create the least-privilege application login role.
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'pix_app') THEN
    CREATE ROLE pix_app LOGIN PASSWORD 'pix_app_pw';
  END IF;
END$$;
GRANT USAGE ON SCHEMA public TO pix_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pix_app;
```

- [ ] **Step 2: Write the bootstrap migration** (`db/migrations/00001_bootstrap.sql`)

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Helper used by every tenant-scoped policy.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION current_tenant_id() RETURNS uuid AS $$
  SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS current_tenant_id();
DROP EXTENSION IF EXISTS pgcrypto;
```

- [ ] **Step 3: Verify migration applies** (after Task 10 brings up compose; if running now, start a throwaway PG)

Run:

```bash
export DATABASE_ADMIN_URL="postgres://pix:pix@localhost:5432/pix?sslmode=disable"
make migrate-up
```

Expected: `OK   00001_bootstrap.sql` and `goose: successfully migrated`.

- [ ] **Step 4: Commit**

```bash
git add db/migrations/00001_bootstrap.sql deploy/compose/initdb/00-roles.sql
git commit -m "feat(db): bootstrap migration, app role, and current_tenant_id() helper

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: pgx Pool with RLS-aware transactions

**Files:**

- Create: `internal/platform/db/db.go`
- Test: `internal/platform/db/db_test.go` (integration, testcontainers)

- [ ] **Step 1: Implement the Pool** (write before the integration test so the container test compiles)

```go
package db

import (
 "context"
 "fmt"
 "os"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
 app   *pgxpool.Pool
 admin *pgxpool.Pool // optional; from DATABASE_ADMIN_URL
}

func New(ctx context.Context, dsn string) (*Pool, error) {
 app, err := pgxpool.New(ctx, dsn)
 if err != nil {
  return nil, fmt.Errorf("db: connect app pool: %w", err)
 }
 p := &Pool{app: app}
 if adminDSN := os.Getenv("DATABASE_ADMIN_URL"); adminDSN != "" {
  admin, err := pgxpool.New(ctx, adminDSN)
  if err != nil {
   return nil, fmt.Errorf("db: connect admin pool: %w", err)
  }
  p.admin = admin
 }
 return p, nil
}

func (p *Pool) Close() {
 p.app.Close()
 if p.admin != nil {
  p.admin.Close()
 }
}

func (p *Pool) Ping(ctx context.Context) error { return p.app.Ping(ctx) }

// WithTenantTx runs fn inside a tx scoped to tenantID via the app.tenant_id GUC.
func (p *Pool) WithTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
 return p.runTx(ctx, p.app, tenantID, fn)
}

// WithAdminTx runs fn inside a tx on the BYPASSRLS admin pool (no tenant scoping).
func (p *Pool) WithAdminTx(ctx context.Context, fn func(pgx.Tx) error) error {
 if p.admin == nil {
  return fmt.Errorf("db: admin pool not configured (set DATABASE_ADMIN_URL)")
 }
 return p.runTx(ctx, p.admin, "", fn)
}

func (p *Pool) runTx(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(pgx.Tx) error) (err error) {
 tx, err := pool.Begin(ctx)
 if err != nil {
  return fmt.Errorf("db: begin: %w", err)
 }
 defer func() {
  if err != nil {
   _ = tx.Rollback(ctx)
  }
 }()
 if tenantID != "" {
  if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
   return fmt.Errorf("db: set tenant: %w", err)
  }
 }
 if err = fn(tx); err != nil {
  return err
 }
 if err = tx.Commit(ctx); err != nil {
  return fmt.Errorf("db: commit: %w", err)
 }
 return nil
}
```

- [ ] **Step 2: Write the failing integration test**

```go
//go:build integration

package db

import (
 "context"
 "testing"

 "github.com/jackc/pgx/v5"
 "github.com/stretchr/testify/require"
 "github.com/testcontainers/testcontainers-go/modules/postgres"
 tc "github.com/testcontainers/testcontainers-go"
 "github.com/testcontainers/testcontainers-go/wait"
 "time"
)

func startPG(t *testing.T) string {
 ctx := context.Background()
 ctr, err := postgres.Run(ctx, "postgres:16-alpine",
  postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
  tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
 )
 require.NoError(t, err)
 t.Cleanup(func() { _ = ctr.Terminate(ctx) })
 dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
 require.NoError(t, err)
 return dsn
}

func TestWithTenantTxSetsGUC(t *testing.T) {
 ctx := context.Background()
 dsn := startPG(t)
 p, err := New(ctx, dsn)
 require.NoError(t, err)
 defer p.Close()
 require.NoError(t, p.Ping(ctx))

 var got string
 err = p.WithTenantTx(ctx, "11111111-1111-1111-1111-111111111111", func(tx pgx.Tx) error {
  return tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&got)
 })
 require.NoError(t, err)
 require.Equal(t, "11111111-1111-1111-1111-111111111111", got)
}

func TestWithTenantTxRollsBackOnError(t *testing.T) {
 ctx := context.Background()
 dsn := startPG(t)
 p, err := New(ctx, dsn)
 require.NoError(t, err)
 defer p.Close()

 _, err = p.app.Exec(ctx, "CREATE TABLE t (n int)")
 require.NoError(t, err)
 wantErr := pgx.ErrNoRows
 _ = p.WithTenantTx(ctx, "22222222-2222-2222-2222-222222222222", func(tx pgx.Tx) error {
  _, _ = tx.Exec(ctx, "INSERT INTO t VALUES (1)")
  return wantErr
 })
 var count int
 require.NoError(t, p.app.QueryRow(ctx, "SELECT count(*) FROM t").Scan(&count))
 require.Equal(t, 0, count, "insert must roll back when fn errors")
}
```

- [ ] **Step 3: Run test to verify it passes** (Docker required)

Run: `go test -tags=integration ./internal/platform/db/`
Expected: PASS (two tests). If Docker is unavailable the test errors at container start — that's an environment problem, not a code failure.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/db/
git commit -m "feat(db): pgx pool with tenant-scoped and admin transactions

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: Health endpoints

**Files:**

- Create: `internal/platform/health/health.go`
- Test: `internal/platform/health/health_test.go`

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/health/`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/health/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/health/
git commit -m "feat(health): live/health/ready endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: Server wiring + graceful shutdown

**Files:**

- Create: `cmd/server/main.go`

> This file grows in file 05 (charge routes). Here it wires config → logger → db → gin → health only.

- [ ] **Step 1: Implement**

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

 "github.com/efipix/pix/internal/platform/config"
 "github.com/efipix/pix/internal/platform/db"
 "github.com/efipix/pix/internal/platform/health"
 "github.com/efipix/pix/internal/platform/logging"
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

 r := gin.New()
 r.Use(gin.Recovery())
 health.Register(r, pool.Ping)

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

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./cmd/server`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(server): wire config, db, health, graceful shutdown

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 10: Docker Compose + Dockerfile + nginx mTLS proxy + env template

**Files:**

- Create: `deploy/docker/Dockerfile`
- Create: `deploy/compose/docker-compose.yml`
- Create: `deploy/compose/nginx-webhook.conf`
- Create: `deploy/env/.env.example`

- [ ] **Step 1: Write the Dockerfile** (`deploy/docker/Dockerfile`)

```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

- [ ] **Step 2: Write the nginx mTLS proxy config** (`deploy/compose/nginx-webhook.conf`) — proxies EFí webhooks to the app, terminating mTLS ([ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md)). Cert files are mounted in real environments; local dev can disable verification.

```nginx
events {}
http {
  server {
    listen 8443 ssl;
    # In real envs: require EFí client cert.
    # ssl_client_certificate /etc/nginx/certs/efi-ca.pem;
    # ssl_verify_client on;
    ssl_certificate     /etc/nginx/certs/server.crt;
    ssl_certificate_key /etc/nginx/certs/server.key;
    location /webhooks/efi {
      proxy_pass http://app:8080/api/v1/webhooks/efi;
      proxy_set_header X-Forwarded-For $remote_addr;
    }
  }
}
```

- [ ] **Step 3: Write docker-compose** (`deploy/compose/docker-compose.yml`)

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: pix
      POSTGRES_PASSWORD: pix
      POSTGRES_DB: pix
    ports: ["5432:5432"]
    volumes:
      - ./initdb:/docker-entrypoint-initdb.d:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U pix"]
      interval: 5s
      timeout: 3s
      retries: 10
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
  rabbitmq:
    image: rabbitmq:3-management-alpine
    ports: ["5672:5672", "15672:15672"]
  app:
    build:
      context: ../..
      dockerfile: deploy/docker/Dockerfile
    environment:
      DATABASE_URL: postgres://pix_app:pix_app_pw@postgres:5432/pix?sslmode=disable
      DATABASE_ADMIN_URL: postgres://pix:pix@postgres:5432/pix?sslmode=disable
      REDIS_URL: redis://redis:6379/0
      EFI_SANDBOX: "true"
    depends_on:
      postgres:
        condition: service_healthy
    ports: ["8080:8080"]
```

- [ ] **Step 4: Write env template** (`deploy/env/.env.example`)

```bash
DATABASE_URL=postgres://pix_app:pix_app_pw@localhost:5432/pix?sslmode=disable
DATABASE_ADMIN_URL=postgres://pix:pix@localhost:5432/pix?sslmode=disable
REDIS_URL=redis://localhost:6379/0
HTTP_PORT=8080
LOG_LEVEL=info
EFI_SANDBOX=true
```

- [ ] **Step 5: Verify the stack boots and migrations apply**

Run:

```bash
make up
sleep 8
export DATABASE_ADMIN_URL="postgres://pix:pix@localhost:5432/pix?sslmode=disable"
make migrate-up
curl -fsS localhost:8080/health
```

Expected: compose services healthy; `goose: successfully migrated`; `{"status":"ok"}`.

- [ ] **Step 6: Commit**

```bash
git add deploy/
git commit -m "feat(deploy): dockerfile, compose (pg/redis/rabbit/nginx), env template

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 11: CI pipeline

**Files:**

- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Write the workflow**

```yaml
name: ci
on:
  push: { branches: [main] }
  pull_request:
jobs:
  build-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.24" }
      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with: { version: latest }
      - name: Unit tests
        run: go test -race -cover ./...
      - name: Integration tests
        run: go test -race -tags=integration ./...
      - name: Build
        run: go build ./...
```

- [ ] **Step 2: Verify YAML is valid locally**

Run: `go run github.com/mikefarah/yq/v4@latest '.jobs."build-test".steps | length' .github/workflows/ci.yml`
Expected: prints `6`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: lint, unit, integration, build pipeline

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## File 01 exit criteria

- [ ] `go build ./...` and `go test ./...` green.
- [ ] `make up && make migrate-up` brings up the stack and applies `00001_bootstrap.sql`.
- [ ] `curl localhost:8080/health` → `{"status":"ok"}`; `/ready` reflects DB reachability.
- [ ] `WithTenantTx` integration test proves the `app.tenant_id` GUC is set and rolls back on error.

Proceed to [02-tenant-provider](2026-06-10-phase1-02-tenant-provider.md).
