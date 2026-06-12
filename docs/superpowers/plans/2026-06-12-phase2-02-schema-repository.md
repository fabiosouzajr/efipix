# Phase 2 · File 02 — Schema Migration & Repository Persistence

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-12-phase2-00-overview.md) first. Depends on File 01 (`DueDateTerms`, `Charge.Terms`).

**Scope:** Migration `00004` (per [ADR-0005](../../adr/0005-cobv-due-date-rule-schema.md)) and the `ChargeRepository` changes to persist/load CobV terms + discount rows. Integration-tested with testcontainers.

**Files:**
- Create: `db/migrations/00004_cobv_rules.sql`
- Modify: `internal/charge/infra/repository.go` (`Create` + `findBy`, new `insertDiscounts`/`loadTerms` helpers)
- Modify: `internal/charge/infra/repository_test.go` (add a CobV round-trip test)

**Note (cerebrum bug-011):** `pix_app` is created idempotently by migration `00002` (a `DO` block with `CREATE ROLE ... IF NOT EXISTS`-style guard), so by `00004` the role already exists — `GRANT ... TO pix_app` in `00004` is safe under testcontainers. No new role guard needed.

---

### Task 1: Migration `00004` — rename/add/drop rule columns, updated CHECKs, `charge_discounts` table

**Files:**
- Create: `db/migrations/00004_cobv_rules.sql`

- [ ] **Step 1: Write the migration**

Create `db/migrations/00004_cobv_rules.sql`:

```sql
-- +goose Up
-- Per ADR-0005: generic fine/interest value columns + a charge_discounts child
-- table for date-banded desconto. These columns were added by 00003 but never
-- populated (CobV was unimplemented), so rename/drop is safe with no backfill.
ALTER TABLE charges RENAME COLUMN fine_percent     TO fine_value;
ALTER TABLE charges RENAME COLUMN interest_percent TO interest_value;
ALTER TABLE charges ADD  COLUMN fine_mode text;
ALTER TABLE charges DROP COLUMN discount_value;

ALTER TABLE charges DROP CONSTRAINT ck_cob_fields;
ALTER TABLE charges DROP CONSTRAINT ck_cobv_fields;
ALTER TABLE charges ADD CONSTRAINT ck_cob_fields CHECK (
  kind <> 'cob' OR (
    due_date IS NULL AND validity_after_days IS NULL AND
    fine_mode IS NULL AND fine_value IS NULL AND
    interest_mode IS NULL AND interest_value IS NULL AND
    discount_mode IS NULL AND abatement_value IS NULL
  )
);
ALTER TABLE charges ADD CONSTRAINT ck_cobv_fields CHECK (
  kind <> 'cobv' OR (expiration_seconds IS NULL AND due_date IS NOT NULL)
);

CREATE TABLE charge_discounts (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  charge_id     uuid NOT NULL REFERENCES charges(id) ON DELETE CASCADE,
  sequence      smallint NOT NULL CHECK (sequence BETWEEN 1 AND 3),
  discount_date date NOT NULL,
  value         numeric NOT NULL,
  CONSTRAINT uq_charge_discounts_seq UNIQUE (charge_id, sequence)
);
CREATE INDEX idx_charge_discounts_charge ON charge_discounts(charge_id);

ALTER TABLE charge_discounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE charge_discounts FORCE  ROW LEVEL SECURITY;
CREATE POLICY p_charge_discounts ON charge_discounts USING (tenant_id = current_tenant_id());
GRANT SELECT, INSERT, UPDATE, DELETE ON charge_discounts TO pix_app;

-- +goose Down
DROP TABLE charge_discounts;
ALTER TABLE charges DROP CONSTRAINT ck_cob_fields;
ALTER TABLE charges DROP CONSTRAINT ck_cobv_fields;
ALTER TABLE charges ADD  COLUMN discount_value numeric;
ALTER TABLE charges DROP COLUMN fine_mode;
ALTER TABLE charges RENAME COLUMN interest_value TO interest_percent;
ALTER TABLE charges RENAME COLUMN fine_value     TO fine_percent;
ALTER TABLE charges ADD CONSTRAINT ck_cob_fields  CHECK (kind <> 'cob'  OR (due_date IS NULL AND fine_percent IS NULL AND interest_percent IS NULL));
ALTER TABLE charges ADD CONSTRAINT ck_cobv_fields CHECK (kind <> 'cobv' OR expiration_seconds IS NULL);
```

- [ ] **Step 2: Verify the migration applies and rolls back cleanly**

Use a throwaway Postgres on an alternate host port (cerebrum: host 5432 is occupied; `postgres:16-alpine` false-readies on first boot — wait for the 2nd "ready" log line).

```bash
export PATH="$PATH:/home/fj/go/bin"
docker run -d --rm --name pix-mig-test -e POSTGRES_USER=pix -e POSTGRES_PASSWORD=pix -e POSTGRES_DB=pix -p 55432:5432 postgres:16-alpine
# wait for the SECOND "ready to accept connections" before connecting:
until [ "$(docker logs pix-mig-test 2>&1 | grep -c 'ready to accept connections')" -ge 2 ]; do sleep 1; done
DSN="postgres://pix:pix@localhost:55432/pix?sslmode=disable"
goose -dir db/migrations postgres "$DSN" up
goose -dir db/migrations postgres "$DSN" down   # rolls back 00004
goose -dir db/migrations postgres "$DSN" up      # re-applies cleanly
docker stop pix-mig-test
```

Expected: `goose ... up`, `down`, `up` all succeed with no errors. (`down` only rolls back the latest migration, 00004.)

- [ ] **Step 3: Commit**

```bash
git add db/migrations/00004_cobv_rules.sql
git commit -m "feat(db): migration 00004 — CobV rule columns and charge_discounts table"
```

Append to `.wolf/memory.md`; add `00004_cobv_rules.sql` to `.wolf/anatomy.md` under `## db/migrations/`.

---

### Task 2: Persist & load CobV terms + discount rows in the repository

**Files:**
- Modify: `internal/charge/infra/repository.go`
- Test: `internal/charge/infra/repository_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/charge/infra/repository_test.go` (the `setup`, `run`, `devTenant`, `devProvider` helpers already exist):

```go
func newCobV() *domain.Charge {
	c, _ := domain.NewDueDate(domain.NewDueDateParams{
		TenantID: devTenant, PaymentProviderID: devProvider, PixKey: "k@e.com",
		Amount: money.Centavos(10000),
		Terms: domain.DueDateTerms{
			DueDate:           brdate.Date(2026, time.December, 31),
			ValidityAfterDays: 30,
			Fine:              &domain.Fine{Mode: domain.FinePercent, Value: 200},          // 2.00%
			Interest:          &domain.Interest{Mode: domain.InterestMonthlyPercent, Value: 100}, // 1.00%/mo
			Discount: &domain.Discount{Mode: domain.DiscountFixed, Entries: []domain.DiscountEntry{
				{Date: brdate.Date(2026, time.December, 20), Value: 500},
				{Date: brdate.Date(2026, time.December, 25), Value: 300},
			}},
			Abatement: money.Centavos(150),
		},
		Today: brdate.Date(2026, time.June, 1),
	})
	return c
}

func TestCreateCobVRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := New(setup(t))
	c := newCobV()
	require.NoError(t, r.Create(ctx, c))

	got, err := r.FindByID(ctx, devTenant, c.ID)
	require.NoError(t, err)
	require.Equal(t, domain.KindCobV, got.Kind)
	require.Equal(t, 0, got.ExpirationSeconds) // NULL -> zero value
	require.NotNil(t, got.Terms)

	tm := got.Terms
	require.Equal(t, "2026-12-31", tm.DueDate.Format("2006-01-02"))
	require.Equal(t, 30, tm.ValidityAfterDays)
	require.Equal(t, domain.FinePercent, tm.Fine.Mode)
	require.Equal(t, money.Centavos(200), tm.Fine.Value)
	require.Equal(t, domain.InterestMonthlyPercent, tm.Interest.Mode)
	require.Equal(t, money.Centavos(100), tm.Interest.Value)
	require.Equal(t, money.Centavos(150), tm.Abatement)

	require.Equal(t, domain.DiscountFixed, tm.Discount.Mode)
	require.Len(t, tm.Discount.Entries, 2)
	require.Equal(t, "2026-12-20", tm.Discount.Entries[0].Date.Format("2006-01-02"))
	require.Equal(t, money.Centavos(500), tm.Discount.Entries[0].Value)
	require.Equal(t, "2026-12-25", tm.Discount.Entries[1].Date.Format("2006-01-02"))
	require.Equal(t, money.Centavos(300), tm.Discount.Entries[1].Value)
}
```

Add the new imports `"time"` (already present) and `"github.com/efipix/pix/internal/platform/brdate"` to the test file's import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=integration ./internal/charge/infra/ -run TestCreateCobVRoundTrip -v`
Expected: FAIL — `Create` inserts no due-date columns/discount rows, so `got.Terms` is nil (panics on `tm.DueDate`). (Needs Docker.)

- [ ] **Step 3: Write minimal implementation**

Replace the `Create` method in `internal/charge/infra/repository.go` with this version (it now writes the CobV columns when `Terms` is set and inserts discount rows):

```go
func (r *Repository) Create(ctx context.Context, c *domain.Charge) error {
	return r.pool.WithTenantTx(ctx, c.TenantID, func(tx pgx.Tx) error {
		var (
			expiration any = c.ExpirationSeconds
			dueDate, validity, fineMode, fineVal, intMode, intVal, discMode, abatement any
		)
		if c.Kind == domain.KindCobV && c.Terms != nil {
			t := c.Terms
			expiration = nil
			dueDate = t.DueDate
			validity = t.ValidityAfterDays
			if t.Fine != nil {
				fineMode, fineVal = string(t.Fine.Mode), int64(t.Fine.Value)
			}
			if t.Interest != nil {
				intMode, intVal = string(t.Interest.Mode), int64(t.Interest.Value)
			}
			if t.Discount != nil {
				discMode = string(t.Discount.Mode)
			}
			if t.Abatement > 0 {
				abatement = int64(t.Abatement)
			}
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO charges
			 (id, tenant_id, payment_provider_id, txid, kind, status, amount, pix_key,
			  description, expiration_seconds, due_date, validity_after_days,
			  fine_mode, fine_value, interest_mode, interest_value, discount_mode, abatement_value,
			  payer_doc, payer_doc_type, payer_name, payer_email, payer_phone,
			  external_reference, version)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)`,
			c.ID, c.TenantID, c.PaymentProviderID, c.Txid, c.Kind, c.Status, int64(c.Amount), c.PixKey,
			c.Description, expiration, dueDate, validity,
			fineMode, fineVal, intMode, intVal, discMode, abatement,
			c.Payer.Doc, c.Payer.DocType, c.Payer.Name, c.Payer.Email, c.Payer.Phone,
			c.ExternalReference, c.Version)
		if err != nil {
			return apperrs.Wrap(apperrs.KindConflict, "insert charge", err)
		}
		if err := insertEvents(ctx, tx, c); err != nil {
			return err
		}
		return insertDiscounts(ctx, tx, c)
	})
}

// insertDiscounts writes a cobv charge's date-banded discount entries (sequence 1..N).
func insertDiscounts(ctx context.Context, tx pgx.Tx, c *domain.Charge) error {
	if c.Terms == nil || c.Terms.Discount == nil {
		return nil
	}
	for i, e := range c.Terms.Discount.Entries {
		if _, err := tx.Exec(ctx,
			`INSERT INTO charge_discounts (tenant_id, charge_id, sequence, discount_date, value)
			 VALUES ($1,$2,$3,$4,$5)`,
			c.TenantID, c.ID, i+1, e.Date, int64(e.Value)); err != nil {
			return apperrs.Wrap(apperrs.KindConflict, "insert discount", err)
		}
	}
	return nil
}
```

Then replace `findBy` (and add `loadTerms`) in the same file. The new `findBy` scans the extra rule columns into pointers and, for cobv, loads the terms + discount rows:

```go
func (r *Repository) findBy(ctx context.Context, tenantID, col, val string) (*domain.Charge, error) {
	var c domain.Charge
	var amount int64
	var (
		expiration                  *int
		dueDate                     *time.Time
		validity                    *int
		fineMode, intMode, discMode *string
		fineVal, intVal, abatement  *int64
	)
	err := r.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT id, tenant_id, payment_provider_id, txid, kind, status, amount, pix_key,
			        description, expiration_seconds, due_date, validity_after_days,
			        fine_mode, fine_value::bigint, interest_mode, interest_value::bigint,
			        discount_mode, abatement_value::bigint,
			        location_id, qr_code_image, pix_payload,
			        payer_doc, payer_doc_type, payer_name, payer_email, payer_phone,
			        external_reference, version
			 FROM charges WHERE `+col+`=$1 AND tenant_id=$2 AND deleted_at IS NULL`, val, tenantID,
		).Scan(&c.ID, &c.TenantID, &c.PaymentProviderID, &c.Txid, &c.Kind, &c.Status, &amount, &c.PixKey,
			&c.Description, &expiration, &dueDate, &validity,
			&fineMode, &fineVal, &intMode, &intVal, &discMode, &abatement,
			&c.LocationID, &c.QRCodeImage, &c.PixPayload,
			&c.Payer.Doc, &c.Payer.DocType, &c.Payer.Name, &c.Payer.Email, &c.Payer.Phone,
			&c.ExternalReference, &c.Version); err != nil {
			return err
		}
		if c.Kind != domain.KindCobV || dueDate == nil {
			return nil
		}
		return loadTerms(ctx, tx, tenantID, &c, dueDate, validity, fineMode, fineVal, intMode, intVal, discMode, abatement)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrs.New(apperrs.KindNotFound, "charge not found")
	}
	if err != nil {
		return nil, err
	}
	if expiration != nil {
		c.ExpirationSeconds = *expiration
	}
	c.Amount = money.Centavos(amount)
	return &c, nil
}

// loadTerms reconstructs DueDateTerms (incl. discount rows) for a cobv charge.
func loadTerms(ctx context.Context, tx pgx.Tx, tenantID string, c *domain.Charge,
	dueDate *time.Time, validity *int, fineMode *string, fineVal *int64,
	intMode *string, intVal *int64, discMode *string, abatement *int64) error {
	t := domain.DueDateTerms{DueDate: *dueDate}
	if validity != nil {
		t.ValidityAfterDays = *validity
	}
	if fineMode != nil && fineVal != nil {
		t.Fine = &domain.Fine{Mode: domain.FineMode(*fineMode), Value: money.Centavos(*fineVal)}
	}
	if intMode != nil && intVal != nil {
		t.Interest = &domain.Interest{Mode: domain.InterestMode(*intMode), Value: money.Centavos(*intVal)}
	}
	if abatement != nil {
		t.Abatement = money.Centavos(*abatement)
	}
	if discMode != nil {
		rows, err := tx.Query(ctx,
			`SELECT discount_date, value::bigint FROM charge_discounts
			 WHERE charge_id=$1 AND tenant_id=$2 ORDER BY sequence`, c.ID, tenantID)
		if err != nil {
			return apperrs.Wrap(apperrs.KindUnknown, "load discounts", err)
		}
		defer rows.Close()
		d := &domain.Discount{Mode: domain.DiscountMode(*discMode)}
		for rows.Next() {
			var dt time.Time
			var v int64
			if err := rows.Scan(&dt, &v); err != nil {
				return apperrs.Wrap(apperrs.KindUnknown, "scan discount", err)
			}
			d.Entries = append(d.Entries, domain.DiscountEntry{Date: dt, Value: money.Centavos(v)})
		}
		if err := rows.Err(); err != nil {
			return apperrs.Wrap(apperrs.KindUnknown, "iterate discounts", err)
		}
		t.Discount = d
	}
	c.Terms = &t
	return nil
}
```

Add `"time"` to the import block of `internal/charge/infra/repository.go` (it currently imports `context`, `errors`, pgx, and the project packages).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags=integration ./internal/charge/infra/ -v`
Expected: PASS — `TestCreateCobVRoundTrip` plus the existing `TestCreateThenActivate` / `TestSaveOptimisticConflict` (the cob path: `Terms == nil` → `expiration_seconds` written, due-date columns NULL, `ck_cob_fields` satisfied).

- [ ] **Step 5: Commit**

```bash
git add internal/charge/infra/repository.go internal/charge/infra/repository_test.go
git commit -m "feat(charge): persist and load CobV terms and discount rows"
```

Append to `.wolf/memory.md`. (No new files — `anatomy.md` entries for `repository.go`/`repository_test.go` already exist; update their descriptions if you maintain them.)

---

## File 02 done — checkpoint

```bash
export PATH="$PATH:/home/fj/go/bin"
go vet ./internal/charge/...
go test -race ./internal/charge/...                      # unit
go test -race -tags=integration ./internal/charge/infra/ # integration (Docker)
golangci-lint run ./internal/charge/...
```

Expected: all green. Proceed to [03-provider-sdk](2026-06-12-phase2-03-provider-sdk.md).
