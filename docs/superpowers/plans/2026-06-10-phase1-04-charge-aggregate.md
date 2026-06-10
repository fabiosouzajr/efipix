# Phase 1 · File 04 — Charge Aggregate: Domain, Schema, Repository, Idempotency

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-10-phase1-00-overview.md) first. Depends on [01-foundation](2026-06-10-phase1-01-foundation.md) and [02-tenant-provider](2026-06-10-phase1-02-tenant-provider.md).

**Goal:** The Charge aggregate root (immediate/Cob for Phase 1) with its status machine and audit events; the `charges`/`payments`/`payment_events`/`outbox`/`idempotency_keys` schema with RLS; a pgx `ChargeRepository` implementing persist-first two-phase writes with optimistic locking + transactional outbox; and a DB-backed `IdempotencyStore`.

**Signature refinements (supersede the overview where noted):**
- Domain transition methods return `error` to enforce guards: `MarkActive(locationID, qrImage, pixPayload string) error` (takes primitives, **not** the provider DTO, so domain stays dependency-free) and `MarkFailed(reason string) error`.
- `IdempotencyStore` lives in `internal/platform/idempotency` (generic, not charge-specific); `OutboxEvent` + `ChargeRepository` live in `internal/charge/app`.

---

### Task 1: Charge schema + RLS

**Files:**
- Create: `db/migrations/00003_charges.sql`

> All CobV columns are included now (nullable) so Phase 2 needs no ALTER; Phase 1 only writes Cob rows. Amount is `bigint` centavos.

- [ ] **Step 1: Write the migration** (`db/migrations/00003_charges.sql`)

```sql
-- +goose Up
CREATE TABLE charges (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  payment_provider_id uuid NOT NULL REFERENCES payment_providers(id) ON DELETE RESTRICT,
  txid                text NOT NULL,
  kind                text NOT NULL CHECK (kind IN ('cob','cobv')),
  status              text NOT NULL,
  amount              bigint NOT NULL CHECK (amount > 0),
  pix_key             text NOT NULL,
  description         text NOT NULL DEFAULT '',
  expiration_seconds  int,
  due_date            date,
  validity_after_days int,
  fine_percent        numeric,
  interest_mode       text,
  interest_percent    numeric,
  discount_mode       text,
  discount_value      numeric,
  abatement_value     numeric,
  location_id         text NOT NULL DEFAULT '',
  qr_code_image       text NOT NULL DEFAULT '',
  pix_payload         text NOT NULL DEFAULT '',
  payer_doc           text NOT NULL DEFAULT '',
  payer_doc_type      text NOT NULL DEFAULT '',
  payer_name          text NOT NULL DEFAULT '',
  payer_email         text NOT NULL DEFAULT '',
  payer_phone         text NOT NULL DEFAULT '',
  external_reference  text NOT NULL DEFAULT '',
  version             int NOT NULL DEFAULT 0,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  deleted_at          timestamptz,
  CONSTRAINT uq_charges_txid UNIQUE (tenant_id, txid),
  CONSTRAINT ck_cob_fields  CHECK (kind <> 'cob'  OR (due_date IS NULL AND fine_percent IS NULL AND interest_percent IS NULL)),
  CONSTRAINT ck_cobv_fields CHECK (kind <> 'cobv' OR expiration_seconds IS NULL)
);
CREATE INDEX idx_charges_status ON charges(tenant_id, status);
CREATE INDEX idx_charges_due    ON charges(tenant_id, due_date) WHERE kind = 'cobv';

CREATE TABLE payments (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  charge_id  uuid NOT NULL REFERENCES charges(id) ON DELETE RESTRICT,
  e2e_id     text NOT NULL,
  amount     bigint NOT NULL,
  paid_at    timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT uq_payments_e2e UNIQUE (tenant_id, e2e_id)
);
CREATE INDEX idx_payments_charge ON payments(charge_id);

CREATE TABLE payment_events (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  charge_id   uuid NOT NULL REFERENCES charges(id) ON DELETE RESTRICT,
  event_type  text NOT NULL,
  payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_payment_events_charge ON payment_events(charge_id, occurred_at);

CREATE TABLE outbox (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  aggregate_id uuid NOT NULL,
  type         text NOT NULL,
  payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
  sent_at      timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_outbox_unsent ON outbox(created_at) WHERE sent_at IS NULL;

CREATE TABLE idempotency_keys (
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  key         text NOT NULL,
  fingerprint text NOT NULL,
  txid        text NOT NULL DEFAULT '',
  status      int  NOT NULL DEFAULT 0,
  response    bytea,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, key)
);

ALTER TABLE charges          ENABLE ROW LEVEL SECURITY; ALTER TABLE charges          FORCE ROW LEVEL SECURITY;
ALTER TABLE payments         ENABLE ROW LEVEL SECURITY; ALTER TABLE payments         FORCE ROW LEVEL SECURITY;
ALTER TABLE payment_events   ENABLE ROW LEVEL SECURITY; ALTER TABLE payment_events   FORCE ROW LEVEL SECURITY;
ALTER TABLE outbox           ENABLE ROW LEVEL SECURITY; ALTER TABLE outbox           FORCE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY; ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;

CREATE POLICY p_charges        ON charges          USING (tenant_id = current_tenant_id());
CREATE POLICY p_payments       ON payments         USING (tenant_id = current_tenant_id());
CREATE POLICY p_payment_events ON payment_events   USING (tenant_id = current_tenant_id());
CREATE POLICY p_outbox         ON outbox           USING (tenant_id = current_tenant_id());
CREATE POLICY p_idem           ON idempotency_keys USING (tenant_id = current_tenant_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON charges, payments, payment_events, outbox, idempotency_keys TO pix_app;

-- +goose Down
DROP TABLE idempotency_keys; DROP TABLE outbox; DROP TABLE payment_events; DROP TABLE payments; DROP TABLE charges;
```

- [ ] **Step 2: Apply and verify**

Run:
```bash
export DATABASE_ADMIN_URL="postgres://pix:pix@localhost:5432/pix?sslmode=disable"
make migrate-up
psql "$DATABASE_ADMIN_URL" -c "\d charges" | grep -c uq_charges_txid
```
Expected: migration OK; grep prints `1`.

- [ ] **Step 3: Commit**

```bash
git add db/migrations/00003_charges.sql
git commit -m "feat(charge): charges/payments/events/outbox/idempotency schema with RLS

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Charge domain — types, constructor, txid

**Files:**
- Create: `internal/charge/domain/charge.go`
- Test: `internal/charge/domain/charge_test.go`

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/money"
)

func validParams() NewImmediateParams {
	return NewImmediateParams{
		TenantID: "t1", PaymentProviderID: "p1", PixKey: "k@e.com",
		Amount: money.Centavos(1050), ExpirationSeconds: 3600,
	}
}

func TestNewTxidFormat(t *testing.T) {
	re := regexp.MustCompile(`^[a-zA-Z0-9]{26,35}$`)
	for i := 0; i < 100; i++ {
		require.Regexp(t, re, NewTxid())
	}
}

func TestNewImmediateCreatesCreated(t *testing.T) {
	c, err := NewImmediate(validParams())
	require.NoError(t, err)
	require.Equal(t, KindCob, c.Kind)
	require.Equal(t, StatusCreated, c.Status)
	require.NotEmpty(t, c.ID)
	require.NotEmpty(t, c.Txid)
	require.Equal(t, 0, c.Version)
	require.Len(t, c.Events, 1)
	require.Equal(t, "created", c.Events[0].EventType)
}

func TestNewImmediateValidates(t *testing.T) {
	p := validParams(); p.Amount = 0
	_, err := NewImmediate(p)
	require.Error(t, err)

	p = validParams(); p.PixKey = ""
	_, err = NewImmediate(p)
	require.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/charge/domain/`
Expected: FAIL.

- [ ] **Step 3: Implement** (`internal/charge/domain/charge.go`)

```go
package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
)

type ChargeKind string

const (
	KindCob  ChargeKind = "cob"
	KindCobV ChargeKind = "cobv"
)

type ChargeStatus string

const (
	StatusCreated   ChargeStatus = "CREATED"
	StatusActive    ChargeStatus = "ACTIVE"
	StatusPending   ChargeStatus = "PENDING"
	StatusPaid      ChargeStatus = "PAID"
	StatusExpired   ChargeStatus = "EXPIRED"
	StatusCancelled ChargeStatus = "CANCELLED"
	StatusRefunded  ChargeStatus = "REFUNDED"
	StatusFailed    ChargeStatus = "FAILED"
)

type Payer struct {
	Doc     string
	DocType string
	Name    string
	Email   string
	Phone   string
}

type PaymentEvent struct {
	ID         string
	ChargeID   string
	EventType  string
	Payload    []byte
	OccurredAt time.Time
}

type Charge struct {
	ID                string
	TenantID          string
	PaymentProviderID string
	Txid              string
	Kind              ChargeKind
	Status            ChargeStatus
	Amount            money.Centavos
	PixKey            string
	Description       string
	ExpirationSeconds int
	LocationID        string
	QRCodeImage       string
	PixPayload        string
	Payer             Payer
	ExternalReference string
	Version           int
	Events            []PaymentEvent // pending audit events to persist
}

type NewImmediateParams struct {
	TenantID          string
	PaymentProviderID string
	PixKey            string
	Amount            money.Centavos
	Description       string
	ExpirationSeconds int
	Payer             Payer
	ExternalReference string
}

// NewTxid returns a client-defined txid: 32 lowercase-hex chars (within EFí's 26–35 alnum range).
func NewTxid() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func NewImmediate(p NewImmediateParams) (*Charge, error) {
	if p.Amount <= 0 {
		return nil, apperrs.New(apperrs.KindValidation, "amount must be positive")
	}
	if p.PixKey == "" {
		return nil, apperrs.New(apperrs.KindValidation, "pix key required")
	}
	if p.ExpirationSeconds <= 0 {
		p.ExpirationSeconds = 3600
	}
	id := uuid.NewString()
	c := &Charge{
		ID: id, TenantID: p.TenantID, PaymentProviderID: p.PaymentProviderID,
		Txid: NewTxid(), Kind: KindCob, Status: StatusCreated, Amount: p.Amount,
		PixKey: p.PixKey, Description: p.Description, ExpirationSeconds: p.ExpirationSeconds,
		Payer: p.Payer, ExternalReference: p.ExternalReference, Version: 0,
	}
	c.appendEvent("created")
	return c, nil
}

func (c *Charge) appendEvent(t string) {
	c.Events = append(c.Events, PaymentEvent{
		ID: uuid.NewString(), ChargeID: c.ID, EventType: t,
		Payload: []byte("{}"), OccurredAt: time.Now().UTC(),
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/charge/domain/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/charge/domain/
git commit -m "feat(charge): domain types, immediate constructor, txid generation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Charge status transitions

**Files:**
- Modify: `internal/charge/domain/charge.go` (add `MarkActive`, `MarkFailed`)
- Test: `internal/charge/domain/transitions_test.go`

- [ ] **Step 1: Write the failing test** (`internal/charge/domain/transitions_test.go`)

```go
package domain

import (
	"testing"

	"github.com/stretchr/testify/require"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

func TestMarkActiveFromCreated(t *testing.T) {
	c, _ := NewImmediate(validParams())
	err := c.MarkActive("loc1", "imgb64", "000201...")
	require.NoError(t, err)
	require.Equal(t, StatusActive, c.Status)
	require.Equal(t, "loc1", c.LocationID)
	require.Equal(t, "000201...", c.PixPayload)
	require.Equal(t, "activated", c.Events[len(c.Events)-1].EventType)
}

func TestMarkActiveIllegalFromFailed(t *testing.T) {
	c, _ := NewImmediate(validParams())
	require.NoError(t, c.MarkFailed("boom"))
	err := c.MarkActive("l", "i", "p")
	require.Equal(t, apperrs.KindConflict, apperrs.KindOf(err))
}

func TestMarkFailedFromCreated(t *testing.T) {
	c, _ := NewImmediate(validParams())
	require.NoError(t, c.MarkFailed("provider down"))
	require.Equal(t, StatusFailed, c.Status)
	require.Equal(t, "failed", c.Events[len(c.Events)-1].EventType)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/charge/domain/ -run TestMark`
Expected: FAIL (MarkActive/MarkFailed undefined).

- [ ] **Step 3: Implement** (append to `internal/charge/domain/charge.go`)

```go
// MarkActive transitions CREATED -> ACTIVE, storing provider location/QR/payload.
func (c *Charge) MarkActive(locationID, qrImage, pixPayload string) error {
	if c.Status != StatusCreated {
		return apperrs.New(apperrs.KindConflict, "charge not in CREATED state")
	}
	c.Status = StatusActive
	c.LocationID = locationID
	c.QRCodeImage = qrImage
	c.PixPayload = pixPayload
	c.appendEvent("activated")
	return nil
}

// MarkFailed transitions CREATED -> FAILED (provider create failed).
func (c *Charge) MarkFailed(reason string) error {
	if c.Status != StatusCreated {
		return apperrs.New(apperrs.KindConflict, "charge not in CREATED state")
	}
	c.Status = StatusFailed
	c.appendEvent("failed")
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/charge/domain/`
Expected: PASS (all domain tests).

- [ ] **Step 5: Commit**

```bash
git add internal/charge/domain/
git commit -m "feat(charge): MarkActive/MarkFailed transitions with guards

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Repository interface + OutboxEvent

**Files:**
- Create: `internal/charge/app/repository.go`

- [ ] **Step 1: Implement**

```go
package app

import (
	"context"

	"github.com/efipix/pix/internal/charge/domain"
)

type OutboxEvent struct {
	ID          string
	TenantID    string
	AggregateID string
	Type        string
	Payload     []byte
}

type ChargeRepository interface {
	// Create = tx A: insert CREATED charge + its pending audit events.
	Create(ctx context.Context, c *domain.Charge) error
	// Save = tx B: optimistic-lock update by (id, version)->version+1, insert the charge's new
	// audit events, and insert the given outbox events — all in one tenant-scoped tx.
	Save(ctx context.Context, c *domain.Charge, out ...OutboxEvent) error
	FindByID(ctx context.Context, tenantID, id string) (*domain.Charge, error)
	FindByTxID(ctx context.Context, tenantID, txid string) (*domain.Charge, error)
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/charge/app/`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/charge/app/repository.go
git commit -m "feat(charge): ChargeRepository interface and OutboxEvent

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: pgx ChargeRepository

**Files:**
- Create: `internal/charge/infra/repository.go`
- Test: `internal/charge/infra/repository_test.go` (integration)

- [ ] **Step 1: Implement** (`internal/charge/infra/repository.go`)

```go
package infra

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	chargeapp "github.com/efipix/pix/internal/charge/app"
	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/db"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
)

type Repository struct{ pool *db.Pool }

func New(pool *db.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, c *domain.Charge) error {
	return r.pool.WithTenantTx(ctx, c.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO charges
			 (id, tenant_id, payment_provider_id, txid, kind, status, amount, pix_key,
			  description, expiration_seconds, payer_doc, payer_doc_type, payer_name,
			  payer_email, payer_phone, external_reference, version)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			c.ID, c.TenantID, c.PaymentProviderID, c.Txid, c.Kind, c.Status, int64(c.Amount), c.PixKey,
			c.Description, c.ExpirationSeconds, c.Payer.Doc, c.Payer.DocType, c.Payer.Name,
			c.Payer.Email, c.Payer.Phone, c.ExternalReference, c.Version)
		if err != nil {
			return apperrs.Wrap(apperrs.KindConflict, "insert charge", err)
		}
		return insertEvents(ctx, tx, c)
	})
}

func (r *Repository) Save(ctx context.Context, c *domain.Charge, out ...chargeapp.OutboxEvent) error {
	return r.pool.WithTenantTx(ctx, c.TenantID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx,
			`UPDATE charges SET status=$1, location_id=$2, qr_code_image=$3, pix_payload=$4,
			   version=version+1, updated_at=now()
			 WHERE id=$5 AND tenant_id=$6 AND version=$7`,
			c.Status, c.LocationID, c.QRCodeImage, c.PixPayload, c.ID, c.TenantID, c.Version)
		if err != nil {
			return apperrs.Wrap(apperrs.KindUnknown, "update charge", err)
		}
		if ct.RowsAffected() != 1 {
			return apperrs.New(apperrs.KindConflict, "optimistic lock conflict")
		}
		c.Version++
		if err := insertEvents(ctx, tx, c); err != nil {
			return err
		}
		for _, e := range out {
			if _, err := tx.Exec(ctx,
				`INSERT INTO outbox (id, tenant_id, aggregate_id, type, payload)
				 VALUES ($1,$2,$3,$4,$5)`,
				e.ID, e.TenantID, e.AggregateID, e.Type, e.Payload); err != nil {
				return apperrs.Wrap(apperrs.KindUnknown, "insert outbox", err)
			}
		}
		return nil
	})
}

// insertEvents writes the charge's pending audit events and clears them.
func insertEvents(ctx context.Context, tx pgx.Tx, c *domain.Charge) error {
	for _, e := range c.Events {
		if _, err := tx.Exec(ctx,
			`INSERT INTO payment_events (id, tenant_id, charge_id, event_type, payload, occurred_at)
			 VALUES ($1,$2,$3,$4,$5,$6)`,
			e.ID, c.TenantID, c.ID, e.EventType, e.Payload, e.OccurredAt); err != nil {
			return apperrs.Wrap(apperrs.KindUnknown, "insert event", err)
		}
	}
	c.Events = nil
	return nil
}

func (r *Repository) FindByID(ctx context.Context, tenantID, id string) (*domain.Charge, error) {
	return r.findBy(ctx, tenantID, "id", id)
}

func (r *Repository) FindByTxID(ctx context.Context, tenantID, txid string) (*domain.Charge, error) {
	return r.findBy(ctx, tenantID, "txid", txid)
}

func (r *Repository) findBy(ctx context.Context, tenantID, col, val string) (*domain.Charge, error) {
	var c domain.Charge
	var amount int64
	err := r.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id, tenant_id, payment_provider_id, txid, kind, status, amount, pix_key,
			        description, COALESCE(expiration_seconds,0), location_id, qr_code_image, pix_payload,
			        payer_doc, payer_doc_type, payer_name, payer_email, payer_phone,
			        external_reference, version
			 FROM charges WHERE `+col+`=$1 AND tenant_id=$2 AND deleted_at IS NULL`, val, tenantID,
		).Scan(&c.ID, &c.TenantID, &c.PaymentProviderID, &c.Txid, &c.Kind, &c.Status, &amount, &c.PixKey,
			&c.Description, &c.ExpirationSeconds, &c.LocationID, &c.QRCodeImage, &c.PixPayload,
			&c.Payer.Doc, &c.Payer.DocType, &c.Payer.Name, &c.Payer.Email, &c.Payer.Phone,
			&c.ExternalReference, &c.Version)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrs.New(apperrs.KindNotFound, "charge not found")
	}
	if err != nil {
		return nil, err
	}
	c.Amount = money.Centavos(amount)
	return &c, nil
}
```

- [ ] **Step 2: Write the failing integration test** (`internal/charge/infra/repository_test.go`)

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

	chargeapp "github.com/efipix/pix/internal/charge/app"
	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/db"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/google/uuid"
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
	run(t, dsn, "../../../db/migrations", "../../../db/seed/dev.sql")
	t.Setenv("DATABASE_ADMIN_URL", dsn)
	pool, err := db.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func run(t *testing.T, dsn, migrations, seed string) {
	g := exec.Command("goose", "-dir", migrations, "postgres", dsn, "up")
	g.Stdout, g.Stderr = os.Stdout, os.Stderr
	require.NoError(t, g.Run())
	s := exec.Command("psql", dsn, "-f", seed)
	s.Stdout, s.Stderr = os.Stdout, os.Stderr
	require.NoError(t, s.Run())
}

const (
	devTenant   = "11111111-1111-1111-1111-111111111111"
	devProvider = "22222222-2222-2222-2222-222222222222"
)

func newCharge() *domain.Charge {
	c, _ := domain.NewImmediate(domain.NewImmediateParams{
		TenantID: devTenant, PaymentProviderID: devProvider, PixKey: "k@e.com",
		Amount: money.Centavos(1050), ExpirationSeconds: 3600,
	})
	return c
}

func TestCreateThenActivate(t *testing.T) {
	ctx := context.Background()
	r := New(setup(t))
	c := newCharge()

	require.NoError(t, r.Create(ctx, c))

	got, err := r.FindByID(ctx, devTenant, c.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusCreated, got.Status)
	require.Equal(t, money.Centavos(1050), got.Amount)

	require.NoError(t, got.MarkActive("loc1", "img", "000201..."))
	out := chargeapp.OutboxEvent{ID: uuid.NewString(), TenantID: devTenant, AggregateID: got.ID, Type: "ChargeCreated", Payload: []byte("{}")}
	require.NoError(t, r.Save(ctx, got, out))

	reloaded, err := r.FindByID(ctx, devTenant, c.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusActive, reloaded.Status)
	require.Equal(t, 1, reloaded.Version)
	require.Equal(t, "000201...", reloaded.PixPayload)
}

func TestSaveOptimisticConflict(t *testing.T) {
	ctx := context.Background()
	r := New(setup(t))
	c := newCharge()
	require.NoError(t, r.Create(ctx, c))

	a, _ := r.FindByID(ctx, devTenant, c.ID)
	b, _ := r.FindByID(ctx, devTenant, c.ID)
	require.NoError(t, a.MarkActive("l", "i", "p"))
	require.NoError(t, r.Save(ctx, a)) // version 0 -> 1

	require.NoError(t, b.MarkActive("l", "i", "p")) // b still at version 0
	err := r.Save(ctx, b)
	require.Error(t, err)
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test -tags=integration ./internal/charge/infra/`
Expected: PASS (2 tests).

- [ ] **Step 4: Commit**

```bash
git add internal/charge/infra/
git commit -m "feat(charge): pgx repository with two-phase persist, optimistic lock, outbox

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Idempotency store

**Files:**
- Create: `internal/platform/idempotency/idempotency.go`
- Test: `internal/platform/idempotency/idempotency_test.go` (integration)

- [ ] **Step 1: Implement** (`internal/platform/idempotency/idempotency.go`)

```go
package idempotency

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/efipix/pix/internal/platform/db"
)

type Reservation struct {
	State        string // "new" | "replay" | "conflict" | "inflight"
	StoredStatus int
	StoredBody   []byte
}

type Store interface {
	Reserve(ctx context.Context, tenantID, key, fingerprint string) (Reservation, error)
	SaveResult(ctx context.Context, tenantID, key string, status int, body []byte) error
}

type PgStore struct{ pool *db.Pool }

func NewPg(pool *db.Pool) *PgStore { return &PgStore{pool: pool} }

func (s *PgStore) Reserve(ctx context.Context, tenantID, key, fingerprint string) (Reservation, error) {
	var res Reservation
	err := s.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx,
			`INSERT INTO idempotency_keys (tenant_id, key, fingerprint)
			 VALUES ($1,$2,$3) ON CONFLICT (tenant_id, key) DO NOTHING`,
			tenantID, key, fingerprint)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 1 {
			res = Reservation{State: "new"}
			return nil
		}
		var fp string
		var status int
		var body []byte
		if err := tx.QueryRow(ctx,
			`SELECT fingerprint, status, response FROM idempotency_keys
			 WHERE tenant_id=$1 AND key=$2`, tenantID, key,
		).Scan(&fp, &status, &body); err != nil {
			return err
		}
		switch {
		case fp != fingerprint:
			res = Reservation{State: "conflict"}
		case status > 0:
			res = Reservation{State: "replay", StoredStatus: status, StoredBody: body}
		default:
			res = Reservation{State: "inflight"}
		}
		return nil
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, err
	}
	return res, nil
}

func (s *PgStore) SaveResult(ctx context.Context, tenantID, key string, status int, body []byte) error {
	return s.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE idempotency_keys SET status=$1, response=$2 WHERE tenant_id=$3 AND key=$4`,
			status, body, tenantID, key)
		return err
	})
}
```

- [ ] **Step 2: Write the failing integration test** (`internal/platform/idempotency/idempotency_test.go`)

```go
//go:build integration

package idempotency

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

	"github.com/efipix/pix/internal/platform/db"
)

const devTenant = "11111111-1111-1111-1111-111111111111"

func setup(t *testing.T) *db.Pool {
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
	return pool
}

func TestReserveLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewPg(setup(t))

	r, err := s.Reserve(ctx, devTenant, "k1", "fpA")
	require.NoError(t, err)
	require.Equal(t, "new", r.State)

	r, err = s.Reserve(ctx, devTenant, "k1", "fpA") // still processing
	require.NoError(t, err)
	require.Equal(t, "inflight", r.State)

	r, err = s.Reserve(ctx, devTenant, "k1", "fpB") // different body
	require.NoError(t, err)
	require.Equal(t, "conflict", r.State)

	require.NoError(t, s.SaveResult(ctx, devTenant, "k1", 201, []byte(`{"txid":"x"}`)))
	r, err = s.Reserve(ctx, devTenant, "k1", "fpA")
	require.NoError(t, err)
	require.Equal(t, "replay", r.State)
	require.Equal(t, 201, r.StoredStatus)
	require.JSONEq(t, `{"txid":"x"}`, string(r.StoredBody))
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test -tags=integration ./internal/platform/idempotency/`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/platform/idempotency/
git commit -m "feat(idempotency): DB-backed reserve/replay/conflict/inflight store

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## File 04 exit criteria

- [ ] `go test ./internal/charge/domain/` green; `go test -tags=integration ./internal/charge/infra/ ./internal/platform/idempotency/` green.
- [ ] Two-phase persist proven: Create→CREATED, Save→ACTIVE with version bump + outbox row + 2 payment_events.
- [ ] Optimistic-lock conflict raises `KindConflict`.
- [ ] Idempotency Reserve returns new/inflight/conflict/replay correctly.
- [ ] `≥80%` coverage on `internal/charge/domain` (`go test -cover ./internal/charge/domain/`).

Proceed to [05-create-charge-api](2026-06-10-phase1-05-create-charge-api.md).
