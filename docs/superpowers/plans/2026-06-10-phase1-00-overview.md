# Phase 1 Implementation Plan — Overview & Conventions

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Take the empty repo to Phase 1 exit criteria of the [design spec](../specs/2026-06-09-pix-payment-platform-design.md): create and fetch an immediate Pix charge (Cob) end-to-end against EFí homologation, multi-tenant, with client-defined txid + persist-first creation, required idempotency, and a failed-provider-call recorded as FAILED with audit.

**Architecture:** Clean Architecture, feature-first modular monolith (Go 1.24+, Gin, pgx + sqlc, goose, Redis, Postgres). The Charge is the single aggregate root. EFí is reached only through the `PixProvider` port; the SDK is isolated in `internal/provider/efi`. Tenant isolation via `tenant_id` row-scoping + Postgres RLS. See ADRs [0001](../../adr/0001-charge-lifecycle-status-model.md)–[0004](../../adr/0004-webhook-ingress-mtls-termination.md).

**Tech Stack:** Go 1.24, Gin, pgx/v5, sqlc, goose, go-redis/v9, golang-migrate-free (goose only), testify, testcontainers-go, `github.com/efipay/sdk-go-apis-efi`.

---

## How this plan set is split

Phase 1 is token-intensive, so it is split into five executable files plus this overview. **Execute in order** — each depends on the previous and ends green + committed.

| File | Scope | Depends on |
|---|---|---|
| [01-foundation](2026-06-10-phase1-01-foundation.md) | Go module, folder skeleton, config, structured logging, pgx pool + RLS tx helper, goose migration tooling, sqlc, `/health /ready /live`, docker-compose, CI | — |
| [02-tenant-provider](2026-06-10-phase1-02-tenant-provider.md) | `tenants`, `payment_providers`, `pix_keys` migrations + domain + repo; API-key auth; `TenantResolver` + `ProviderResolver` Gin middleware | 01 |
| [03-secrets-efi-provider](2026-06-10-phase1-03-secrets-efi-provider.md) | `SecretProvider` (env impl), `PixProvider` port + DTOs, `EfiProvider` per-account SDK client pool (P12→PEM), credential validation | 01, 02 |
| [04-charge-aggregate](2026-06-10-phase1-04-charge-aggregate.md) | Charge domain (entity, `Payer` VO, status machine, events), `charges`/`payments`/`payment_events`/`idempotency_keys`/`outbox` migrations, `ChargeRepository`, `IdempotencyStore`, `OutboxStore` | 01, 02 |
| [05-create-charge-api](2026-06-10-phase1-05-create-charge-api.md) | `CreateImmediateCharge` use case (persist-first two-phase), `GetCharge`, Gin handlers/DTOs, idempotency middleware, `cmd/server` wiring, end-to-end test | 01–04, 03 |

---

## Conventions (apply to every task)

- **Module path:** `github.com/efipix/pix`. All import paths below assume this.
- **Go version:** `go 1.24`.
- **Testing:** `github.com/stretchr/testify` (`require`/`assert`). Integration tests use `testcontainers-go` Postgres; tag them `//go:build integration` and run with `go test -tags=integration ./...`. Pure unit tests have no build tag.
- **Errors:** domain/app errors are typed values in `internal/platform/errors` (e.g. `errors.NotFound`, `errors.Conflict`, `errors.Validation`); handlers map them to HTTP status. Never return raw pgx errors past the repo.
- **Context:** every public method takes `ctx context.Context` first. Tenant id travels in context via `tenantctx` helpers (defined in 02), never as a global.
- **Time/IDs:** `uuid.NewString()` (`github.com/google/uuid`) for primary keys; `time.Now().UTC()` everywhere.
- **Money:** integer **centavos** (`int64`) end-to-end. EFí amounts are decimal strings (`"10.50"`); the adapter converts (centavos↔string) only inside `internal/provider/efi`. A `money.Centavos` type lives in `internal/platform/money` (defined in 01).
- **Commits:** Conventional Commits, one per task's final step. Co-author trailer:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
- **TDD:** write the failing test, run it red, implement minimal, run green, commit. Never write implementation before its test.

---

## Locked signatures (single source of truth — keep identical across files)

These names/signatures are referenced by multiple files. Do not rename them; if a change is needed, update this section first.

```go
// internal/platform/money/money.go
package money
type Centavos int64
func (c Centavos) String() string            // "1050" -> "10.50"
func ParseString(s string) (Centavos, error) // "10.50" -> 1050

// internal/platform/errors/errors.go
package errors
type Kind int
const ( KindUnknown Kind = iota; KindNotFound; KindConflict; KindValidation; KindUnauthorized; KindProvider )
type Error struct { Kind Kind; Msg string; Err error }
func (e *Error) Error() string
func New(kind Kind, msg string) *Error
func Wrap(kind Kind, msg string, err error) *Error
func KindOf(err error) Kind

// internal/platform/db/db.go
package db
type Pool struct { /* wraps *pgxpool.Pool */ }
func New(ctx context.Context, dsn string) (*Pool, error)
func (p *Pool) Close()
func (p *Pool) Ping(ctx context.Context) error
// WithTenantTx opens a tx, runs `SET LOCAL app.tenant_id = $tenantID`, then fn. Commits on nil error.
func (p *Pool) WithTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error
// WithAdminTx opens a tx WITHOUT setting app.tenant_id (RLS-bypassing role) for migrations/admin.
func (p *Pool) WithAdminTx(ctx context.Context, fn func(pgx.Tx) error) error

// internal/platform/tenantctx/tenantctx.go   (defined in 02)
package tenantctx
func With(ctx context.Context, t *Resolved) context.Context
func From(ctx context.Context) (*Resolved, bool)
type Resolved struct { TenantID string; ProviderID string; PixKey string } // ProviderID = resolved default/explicit payment_provider; PixKey = its acting pix key

// internal/tenant/domain   (defined in 02)
type Tenant struct { ID, Name, Status string }
type PaymentProvider struct { ID, TenantID, Provider, AccountLabel, Status string; IsDefault bool; WebhookConfig []byte }
type PixKey struct { ID, TenantID, PaymentProviderID, Key, KeyType string }

// internal/platform/secrets/secrets.go   (defined in 03)
package secrets
type ProviderCreds struct { ClientID, ClientSecret string; CertPEM, KeyPEM []byte; Sandbox bool }
type SecretProvider interface {
    ProviderCredentials(ctx context.Context, paymentProviderID string) (*ProviderCreds, error)
}

// internal/provider/provider.go   (defined in 03)
package provider
type ImmediateChargeInput struct {
    Txid              string        // client-defined, 26-35 alphanumeric
    PaymentProviderID string
    Amount            money.Centavos
    PixKey            string
    Description       string
    ExpirationSeconds int
    PayerDoc          string // cpf/cnpj digits, optional
    PayerDocType      string // "cpf" | "cnpj" | ""
    PayerName         string
}
type ChargeResult struct {
    Txid       string
    Status     string // raw provider status, e.g. "ATIVA"
    LocationID string
    QRCodeImage string // base64 PNG
    PixPayload  string // copia-e-cola
}
type PixProvider interface {
    CreateImmediateCharge(ctx context.Context, in *ImmediateChargeInput) (*ChargeResult, error)
    GetCharge(ctx context.Context, paymentProviderID, txid string) (*ChargeResult, error)
}

// internal/charge/domain   (defined in 04)
type ChargeKind string   // "cob" | "cobv"
type ChargeStatus string // CREATED, ACTIVE, PENDING, PAID, EXPIRED, CANCELLED, REFUNDED, FAILED
type Payer struct { Doc, DocType, Name, Email, Phone string }
type Charge struct {
    ID, TenantID, PaymentProviderID, Txid string
    Kind   ChargeKind
    Status ChargeStatus
    Amount money.Centavos
    PixKey, LocationID, QRCodeImage, PixPayload, Description, ExternalReference string
    ExpirationSeconds int
    Payer  Payer
    Version int
    Events []PaymentEvent  // pending audit events to persist
}
type PaymentEvent struct { ID, ChargeID, EventType string; Payload []byte; OccurredAt time.Time }
// constructor + transitions (defined in 04). Transitions take primitives (NOT the provider DTO,
// so domain imports nothing outward) and return error to enforce guards:
func NewImmediate(p NewImmediateParams) (*Charge, error)                    // status CREATED, appends event "created"
func (c *Charge) MarkActive(locationID, qrImage, pixPayload string) error   // CREATED->ACTIVE, event "activated"
func (c *Charge) MarkFailed(reason string) error                            // CREATED->FAILED, event "failed"

// internal/charge/app   (defined in 04 & 05)
type ChargeRepository interface {
    // Create = tx A: insert CREATED charge + its pending audit PaymentEvents.
    Create(ctx context.Context, c *domain.Charge) error
    // Save = tx B: optimistic-lock update by (id, version)→version+1, insert the charge's new
    // audit PaymentEvents, and insert the given outbox events — all in ONE tenant-scoped tx.
    Save(ctx context.Context, c *domain.Charge, out ...OutboxEvent) error
    FindByID(ctx context.Context, tenantID, id string) (*domain.Charge, error)
    FindByTxID(ctx context.Context, tenantID, txid string) (*domain.Charge, error)
}
type OutboxEvent struct { ID, TenantID, AggregateID, Type string; Payload []byte }
// Phase 1 writes the `outbox` table directly inside ChargeRepository.Save. The full
// OutboxStore (FetchUnsent/MarkSent) + RabbitMQ relay arrive in Phase 3/4.

// internal/platform/idempotency   (defined in 04 — generic, not charge-specific)
type Reservation struct { State string; StoredStatus int; StoredBody []byte } // State: new|replay|conflict|inflight
type Store interface {
    Reserve(ctx context.Context, tenantID, key, fingerprint string) (Reservation, error)
    SaveResult(ctx context.Context, tenantID, key string, status int, body []byte) error
}
```

---

## Phase 1 exit checklist (verify after file 05)

- [ ] `POST /api/v1/charges` (immediate) creates a charge: row persisted CREATED → EFí `PUT /v2/cob/:txid` → ACTIVE with QR + payload returned.
- [ ] A forced EFí failure leaves the charge `FAILED` with a `payment_events` "failed" row (audit), and returns 502.
- [ ] `GET /api/v1/charges/{id}` returns the charge, tenant-scoped (RLS denies cross-tenant).
- [ ] Missing `Idempotency-Key` on POST → 400; repeated key+same body → replay (same txid); same key+different body → 422.
- [ ] `≥80%` coverage on `internal/charge/domain` and `internal/charge/app` (`go test -cover`).
- [ ] SDK capability review note committed at `docs/efi-sdk-review.md`.
- [ ] `go vet ./...`, `golangci-lint run`, and `go test ./...` all green in CI.

---

## Execution Handoff

After all five files are implemented, the Phase 1 exit checklist above is the acceptance gate. Phases 2–6 get their own plans (one per phase) following this same split-by-file pattern when the user requests them.
