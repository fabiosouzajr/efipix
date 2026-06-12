# Phase 2 Implementation Plan — Overview & Conventions

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add **due-date charges (CobV)** — a charge with a due date plus payer-charge rules **multa** (fine), **juros** (interest), **desconto** (discount), **abatimento** (abatement) — to the existing `Charge` aggregate, created against EFí `PUT /v2/cobv/:txid`, reusing Phase 1's persist-first / idempotency / provider machinery. `POST /api/v1/charges` becomes CobV-only.

**Architecture:** Same Clean-Architecture, feature-first modular monolith as Phase 1. The CobV path reuses the existing `Charge` aggregate, `ChargeRepository` (two-phase persist-first), `idempotency.Middleware`, and the `PixProvider` port. New pieces: a `DueDateTerms` value object + a pure `EffectiveAmount` rule function in `internal/charge/domain`, a `brdate` platform helper for America/Sao_Paulo "today", a `charge_discounts` child table, and a `CreateDueDateCharge` provider/use-case/handler path. `domain.NewImmediate` (Cob) stays in the domain with its unit tests but is no longer reachable from the API ([ADR-0006](../../adr/0006-post-charges-cobv-only.md)).

**Tech Stack:** Go 1.25, Gin, pgx/v5 (raw SQL — this repo does **not** use sqlc-generated code despite `sqlc.yaml`), goose, testify, testcontainers-go, `github.com/efipay/sdk-go-apis-efi` v1.4.0.

**Source spec:** [2026-06-10-phase2-due-date-charges-spec.md](../specs/2026-06-10-phase2-due-date-charges-spec.md)
**Decisions:** [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md), [ADR-0005](../../adr/0005-cobv-due-date-rule-schema.md), [ADR-0006](../../adr/0006-post-charges-cobv-only.md)

---

## How this plan set is split

Phase 2 is split into this overview plus four executable files. **Execute in order** — each depends on the previous and ends green + committed.

| File | Scope | Depends on |
|---|---|---|
| [01-domain-rules](2026-06-12-phase2-01-domain-rules.md) | `brdate` platform helper; `DueDateTerms` VO + rule modes; pure `EffectiveAmount`; `NewDueDate` constructor; `Charge.Terms` field. All pure unit tests. | Phase 1 |
| [02-schema-repository](2026-06-12-phase2-02-schema-repository.md) | Migration `00004` (rename/add/drop rule columns, updated CHECKs, `charge_discounts` table + RLS); `ChargeRepository` persist + load of CobV terms & discount rows. | 01 |
| [03-provider-sdk](2026-06-12-phase2-03-provider-sdk.md) | `PixProvider.CreateDueDateCharge` + DTOs; `EfiProvider.CreateDueDateCharge`; `efiClient.CreateCobV`; `sdkClient.CreateCobV` → `CreateDueCharge` (`PUT /v2/cobv/:txid`); EFí modalidade mapping recorded in `docs/efi-sdk-review.md`. | 01 |
| [04-usecase-api](2026-06-12-phase2-04-usecase-api.md) | `CreateDueDateCharge` use case; CobV request DTO + validation; `amount_due` breakdown on responses; `cmd/server` wiring; rewritten `e2e_test.go`. | 01, 02, 03 |

### Deliberately out of this plan

- **`ChargeOverdue`/`ChargeDueSoon`/`ChargeExpired` predicates & scheduler** — spec §2/§4 defers the emitting scheduler and OVERDUE/EXPIRED transitions to Phase 3, and says Phase 2 defines overdue/due-soon "conceptually" only. `EffectiveAmount`'s `diff` already encodes the late/early distinction; a standalone predicate with no caller would be dead code (fails this repo's deletion-test convention, per cerebrum). It lands in Phase 3 with the scheduler that consumes it. Not a gap.
- **`GET /api/v1/charges` list+filter**, antecipação-rate desconto (modalidades 3-6), webhook settlement, notifications — all out of scope per spec §2.

---

## Conventions (apply to every task)

- **Module path:** `github.com/efipix/pix`. **Go version:** `go 1.25.0`.
- **Testing:** `github.com/stretchr/testify` (`require`/`assert`). Integration tests use `testcontainers-go` Postgres; tag them `//go:build integration` and run with `go test -tags=integration ./...` (needs Docker). Pure unit tests have no build tag.
- **`~/go/bin` not on PATH:** prefix goose/golangci-lint/psql-less commands with `export PATH="$PATH:/home/fj/go/bin"` (per cerebrum). `goose`, `psql` are used by integration tests via `os/exec`.
- **Errors:** typed values in `internal/platform/errors` (`apperrs.New(apperrs.KindValidation, msg)`, `apperrs.Wrap(kind, msg, err)`). Never return raw pgx errors past the repo. Handlers map kinds to HTTP via `httpx.StatusFor`.
- **Context:** every public method takes `ctx context.Context` first. Tenant id travels via `tenantctx`, never global.
- **Time/IDs:** `uuid.NewString()` for keys; `time.Now().UTC()` for audit timestamps. **Business dates** use `brdate` (America/Sao_Paulo), defined in File 01.
- **Money & percent:** integer **centavos** (`money.Centavos int64`) end-to-end. EFí amounts are decimal strings (`"10.50"`). **Percentages reuse the same representation** — see "Locked decisions" below.
- **Commits:** Conventional Commits, one per task's final step. Co-author trailer:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
- **Branch:** create a feature branch `phase2-due-date-charges` before File 01 (do not commit on `main`, per cerebrum). Checkpoint with the user between files.
- **OpenWolf:** after each task, append a line to `.wolf/memory.md`; after creating/renaming files, update `.wolf/anatomy.md`; log any bug/failed-test to `.wolf/buglog.json` (read it first).
- **TDD:** write the failing test, run it red, implement minimal, run green, commit. Never write implementation before its test.

---

## Locked decisions (single source of truth — keep identical across files)

These names/semantics are referenced by multiple files. Do not change them in one file without updating this section.

### Percent representation (reuse `money.Centavos`)

A percentage is parsed from a 2-decimal string (e.g. `"2.50"`) with **`money.ParseString`** and stored as `money.Centavos` holding **hundredths of a percent** (`2.50%` → `250`, `100%` → `10000`). `Value.String()` renders it back to `"2.50"` for EFí. This means:

- **Fixed** rule values (fine `fixed`, abatement, discount `fixed`) = centavos (`"10.00"` → `1000`).
- **Percent** rule values (fine `percent`, interest, discount `percent`) = hundredths-of-a-percent (`"2.50"` → `250`).
- Validation: percent values are valid in `[0, 10000]` (i.e. `[0,100]%`); fixed values must be `> 0`.
- Applying a percent to a base: `component = roundHalfUp(base * valueHundredthsPercent, 10000)`.

### `brdate.Days` semantics (off-by-one safety)

`brdate.Days(a, b)` returns whole days `b - a` comparing each argument's **own-location civil date** (it does NOT convert `a` or `b` into the SP zone first). This is deliberate: a `date` column decoded by pgx arrives as UTC midnight, while `brdate.Today()`/`brdate.Parse` produce SP midnight; taking each value's own `.Date()` yields the intended civil date for both, avoiding a one-day drift. A test in File 01 locks this.

### EFí CobV modalidade mapping (confirm + record in `docs/efi-sdk-review.md`, File 03)

| Domain mode | EFí object | `modalidade` |
|---|---|---|
| fine `fixed` | `valor.multa` | `1` |
| fine `percent` | `valor.multa` | `2` |
| interest `daily_percent` | `valor.juros` | `2` (percentual ao dia, dias corridos) |
| interest `monthly_percent` | `valor.juros` | `5` (percentual ao mês, dias corridos) |
| discount `fixed` | `valor.desconto` | `1` (valor fixo até data) |
| discount `percent` | `valor.desconto` | `2` (percentual até data) |
| abatement (always fixed) | `valor.abatimento` | `1` |

Each object's value goes in field **`valorPerc`** (string, 2 decimals). Date-banded desconto entries go in `desconto.descontoDataFixa: [{data, valorPerc}]`. File 03 includes a step to confirm these against the SDK source/examples and record them in `docs/efi-sdk-review.md`.

### tzdata

`internal/platform/brdate` blank-imports `time/tzdata` so `America/Sao_Paulo` loads on minimal images (Alpine) without `apk add tzdata`. No Dockerfile change required.

---

## Locked signatures (keep identical across files)

```go
// internal/platform/brdate/brdate.go   (defined in 01)
package brdate
var Loc *time.Location                                   // America/Sao_Paulo
func Date(year int, month time.Month, day int) time.Time // SP midnight
func Today() time.Time                                   // current SP business date (midnight)
func Parse(s string) (time.Time, error)                  // "2006-01-02" -> SP midnight
func Days(a, b time.Time) int                            // b - a in whole days, own-location civil dates

// internal/charge/domain   (defined in 01)
type FineMode string      // "fixed" | "percent"
type InterestMode string  // "daily_percent" | "monthly_percent"
type DiscountMode string  // "fixed" | "percent"
const ( FineFixed FineMode = "fixed"; FinePercent FineMode = "percent" )
const ( InterestDailyPercent InterestMode = "daily_percent"; InterestMonthlyPercent InterestMode = "monthly_percent" )
const ( DiscountFixed DiscountMode = "fixed"; DiscountPercent DiscountMode = "percent" )

type Fine struct { Mode FineMode; Value money.Centavos }
type Interest struct { Mode InterestMode; Value money.Centavos }
type DiscountEntry struct { Date time.Time; Value money.Centavos }
type Discount struct { Mode DiscountMode; Entries []DiscountEntry } // 1..3 entries
type DueDateTerms struct {
    DueDate           time.Time
    ValidityAfterDays int
    Fine              *Fine
    Interest          *Interest
    Discount          *Discount
    Abatement         money.Centavos // 0 = not configured
}
type AmountBreakdown struct {
    Original, Discount, Abatement, Fine, Interest, Total money.Centavos
}
// Charge gains: Terms *DueDateTerms   // nil for cob

type NewDueDateParams struct {
    TenantID, PaymentProviderID, PixKey string
    Amount            money.Centavos
    Description       string
    Payer             Payer
    ExternalReference string
    Terms             DueDateTerms
    Today             time.Time // SP business date for past-date validation (app passes brdate.Today())
}
func NewDueDate(p NewDueDateParams) (*Charge, error)                       // status CREATED, Kind=cobv, event "created"
func EffectiveAmount(terms DueDateTerms, base money.Centavos, asOf time.Time) AmountBreakdown
// MarkActive / MarkFailed reused unchanged.

// internal/provider/provider.go   (defined in 03)
type FineInput struct { Mode, Value string }
type InterestInput struct { Mode, Value string }
type DiscountEntryInput struct { Date, Value string }
type DiscountInput struct { Mode string; Entries []DiscountEntryInput }
type DueDateChargeInput struct {
    Txid, PaymentProviderID, PixKey, Description     string
    Amount                                           money.Centavos
    PayerDoc, PayerDocType, PayerName                string
    DueDate                                          string // "2006-01-02"
    ValidityAfterDays                                int
    Fine                                             *FineInput
    Interest                                         *InterestInput
    Discount                                         *DiscountInput
    Abatement                                        string // decimal "10.00", "" if none
}
// PixProvider gains:
//   CreateDueDateCharge(ctx context.Context, in *DueDateChargeInput) (*ChargeResult, error)

// internal/provider/efi/client.go   (defined in 03)
type cobvInput struct { /* mirrors DueDateChargeInput, decimal-string values */ }
// efiClient gains: CreateCobV(ctx context.Context, in cobvInput) (cobOutput, error)
// efiSDKClient gains: CreateDueCharge(txid string, body map[string]interface{}) (string, error)

// internal/charge/app   (defined in 04)
type CreateDueDateChargeCmd struct {
    TenantID, PaymentProviderID, PixKey string
    Amount            money.Centavos
    Description       string
    Payer             domain.Payer
    ExternalReference string
    Terms             domain.DueDateTerms
    Today             time.Time
}
type CreateDueDateCharge struct { /* repo + prov */ }
func NewCreateDueDateCharge(repo ChargeRepository, prov provider.PixProvider) *CreateDueDateCharge
func (uc *CreateDueDateCharge) Execute(ctx context.Context, cmd CreateDueDateChargeCmd) (*domain.Charge, error)
```

---

## `EffectiveAmount` rules (authoritative — implemented in File 01)

Let `diff = brdate.Days(terms.DueDate, asOf)` (asOf − due, in days).

- **`diff < 0` (early):** `Fine = Interest = 0`. Discount = the entry with the **smallest `Date` ≥ `asOf`** (nearest deadline still met); none qualifies ⇒ `0`. Fixed ⇒ `entry.Value`; percent ⇒ `roundHalfUp(base*entry.Value, 10000)`.
- **`diff == 0` (on time):** no discount, no fine, no interest.
- **`diff > 0` (late):** no discount. `Fine`: fixed ⇒ `Value`; percent ⇒ `roundHalfUp(base*Value, 10000)`. `Interest`: `daily_percent` ⇒ `roundHalfUp(base*Value*diff, 10000)`; `monthly_percent` pro-rates daily ⇒ `roundHalfUp(base*Value*diff, 10000*30)`.
- **Abatement** is **always** subtracted (any timing).
- `roundHalfUp(num, den) = money.Centavos((2*num + den) / (2*den))` (half-up for non-negative values).
- `Total = max(0, Original − Discount − Abatement + Fine + Interest)`.

---

## Phase 2 exit checklist (verify after file 04)

- [ ] `POST /api/v1/charges` creates a `kind=cobv` charge (due_date required, no `kind` field): row persisted CREATED → EFí `PUT /v2/cobv/:txid` → ACTIVE; fine/interest/discount/abatement applied in the EFí body.
- [ ] `EffectiveAmount` unit-tested across on-time / late (fine+daily interest) / late (monthly pro-ration) / early (discount fixed + percent, selection rule) / abatement / rounding / total-clamp, with exact centavos.
- [ ] `GET /api/v1/charges/{id}` returns due-date terms + an `amount_due` breakdown for cobv charges.
- [ ] CHECK + app validation reject cob/cobv field mismatches and out-of-range rule values (percent ∉ [0,100], fixed ≤ 0, past due_date, >3 discount entries, discount date ∉ (today, due_date]).
- [ ] `internal/charge/api/e2e_test.go` rewritten for the cobv-only `POST /charges`; `domain.NewImmediate`'s unit tests (`charge_test.go`, `transitions_test.go`) remain green.
- [ ] `≥80%` coverage on `internal/charge/domain` (`go test -cover ./internal/charge/domain`).
- [ ] EFí CobV modalidade mapping recorded in `docs/efi-sdk-review.md`.
- [ ] `go vet ./...`, `golangci-lint run ./...`, `go test -race ./...`, and `go test -race -tags=integration ./...` all green.

---

## Execution Handoff

Execute File 01 → 02 → 03 → 04 in order, each green + committed, checkpointing with the user between files (per cerebrum preference). The exit checklist above is the acceptance gate. The homologation smoke test (a real CobV against EFí) is gated on `EFI_CREDENTIALS` and may be skipped in this environment, as in Phase 1.
