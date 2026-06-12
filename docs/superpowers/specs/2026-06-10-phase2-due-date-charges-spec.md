# Phase 2 Spec — Due-Date Charges (CobV) with Fine, Interest, Discount, Abatement

**Date:** 2026-06-10 (revised 2026-06-12 after grilling session)
**Status:** Approved — decisions resolved, ready for implementation plan
**Master design:** [pix-payment-platform-design](2026-06-09-pix-payment-platform-design.md) · **Glossary:** [CONTEXT.md](../../../CONTEXT.md)
**Decisions:** [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md) (status, derived OVERDUE/EXPIRED), [ADR-0002](../../adr/0002-client-defined-txid-persist-first.md) (txid/persist-first), [ADR-0005](../../adr/0005-cobv-due-date-rule-schema.md) (charge_discounts + fine/interest value-mode columns), [ADR-0006](../../adr/0006-post-charges-cobv-only.md) (POST /charges is CobV-only)
**Depends on:** Phase 1 ([spec](2026-06-10-phase1-tenants-auth-immediate-charge-spec.md))

---

## 1. Goal

Add **due-date charges (CobV)** to the existing charge aggregate: a charge with a due date plus payer-charge rules — **multa** (fine), **juros** (interest), **desconto** (discount), **abatimento** (abatement) — created against EFí `PUT /v2/cobv/:txid`, reusing Phase 1's persist-first/idempotency/provider machinery.

## 2. Scope

### In scope

- `ChargeKind=cobv` path through the same `Charge` aggregate. `POST /api/v1/charges` now creates **only** `kind=cobv` charges — no `kind` field, `due_date` required ([ADR-0006](../../adr/0006-post-charges-cobv-only.md)). `domain.NewImmediate` (Cob) stays in `internal/charge/domain` with its existing unit tests but is unreachable via the API.
- `DueDateTerms` value object + calculation rules for multa, juros, desconto, abatimento. All four rule blocks are optional (a CobV charge may have none configured).
- CobV provider mapping in `EfiProvider`: `CreateDueDateCharge` (cobv create only). No cobv "get" — `GET /charges/{id}` reads the locally persisted charge, no provider call.
- Schema changes per [ADR-0005](../../adr/0005-cobv-due-date-rule-schema.md): rename `fine_percent`→`fine_value` + add `fine_mode`, rename `interest_percent`→`interest_value`; drop `discount_value`, keep `discount_mode` (shared modalidade); new `charge_discounts` table for date-banded desconto (modalidades 1-2, up to 3 entries). Antecipação-rate desconto (modalidades 3-6) is **not** in scope.
- Validation + CHECK enforcement of cob/cobv field pairing, updated for the renamed/added columns.
- `GET /api/v1/charges/{id}` gains an `amount_due` breakdown for cobv, computed via `EffectiveAmount(terms, amount, today)`.

### Out of scope

- Webhook settlement, lifecycle beyond ACTIVE, OVERDUE/EXPIRED transitions and the scheduler that emits them (Phase 3). Phase 2 only defines OVERDUE/Due-soon as derived predicates conceptually; the emitting scheduler lands in Phase 3.
- `GET /api/v1/charges` (list + filter by status/kind/due_date/overdue, paging) — deferred to Phase 3.
- Antecipação-rate desconto (EFí modalidades 3-6).
- Notifications about due/overdue (Phase 4).

## 3. Functional requirements

- `POST /api/v1/charges` accepts a due-date body: `due_date` (required), optional `validity_after_days` (default `30` if omitted — EFí's documented default, confirm in SDK review), and optional rule blocks:
  - **fine** (multa): `mode` ∈ `fixed | percent` + `value`.
  - **interest** (juros): `mode` ∈ `daily_percent | monthly_percent` + `value`.
  - **discount** (desconto): shared `mode` ∈ `fixed | percent` + up to 3 date-banded `entries: [{date, value}]` (each `date` ∈ `(today, due_date]`).
  - **abatement** (abatimento): fixed `value`.
- The charge is created at EFí as CobV with these terms; response mirrors Phase 1 plus the due-date terms, plus an `amount_due` breakdown (see §6).
- Rule math is computed and unit-verified for representative scenarios (on-time, late with fine+interest, early with discount, with abatement). The **authoritative settled amount comes from EFí/webhook** (Phase 3); Phase 2's calculations are for quoting/validation and reporting projections.
- "Today" for due-date validation and `EffectiveAmount`'s default `asOf` is the current date in **America/Sao_Paulo** (not UTC) — Brazilian business date. Requires `tzdata` available at runtime (Alpine: `time/tzdata` blank-import or `apk add tzdata`).
- Validation: `due_date` required and not in the past (America/Sao_Paulo "today"); percent fields in `[0,100]`; fixed fine/abatement values `> 0` centavos; each discount entry's `date` in `(today, due_date]`; `validity_after_days >= 0`; cob-only fields rejected for cobv and vice-versa (CHECK + app validation).

## 4. Domain changes

- `DueDateTerms` VO: `DueDate`, `ValidityAfterDays`, `Fine{Mode,Value}`, `Interest{Mode,Value}`, `Discount{Mode, Entries []{Date,Value}}` (≤3 entries), `Abatement value`. Each of `Fine`/`Interest`/`Discount`/`Abatement` is optional (nil/zero = not configured).
- `Charge` gains a `NewDueDate(params)` constructor (→CREATED, `Kind=cobv`, event "created"); `MarkActive` reused unchanged.
- Pure rule function (no I/O): `EffectiveAmount(terms, base, asOf) -> {Original, Discount, Abatement, Fine, Interest, Total}` — used by `amount_due` (§6) and Phase 5 reports. Rules:
  - `asOf < due_date` (early): discount entry chosen = smallest `Entries[i].Date >= asOf` (the nearest deadline still met); no entry qualifies ⇒ discount = 0. Fine/interest = 0.
  - `asOf == due_date` (on time): no discount, no fine/interest.
  - `asOf > due_date` (late): fine + interest apply. `monthly_percent` interest pro-rates daily: `dailyRate = rate/30`, `interest = round(base * dailyRate/100 * days_late)`. No discount.
  - Abatement is **always** subtracted, regardless of timing.
  - Each component rounds half-up to centavos; `Total = max(0, Original - Discount - Abatement + Fine + Interest)`.
- **Overdue / Due-soon** defined as derived predicates over CobV (per [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md)); the scheduler that emits `ChargeOverdue`/`ChargeDueSoon`/`ChargeExpired` is Phase 3.

## 5. Data model changes

Per [ADR-0005](../../adr/0005-cobv-due-date-rule-schema.md):

- Rename `fine_percent`→`fine_value`, `interest_percent`→`interest_value`; add `fine_mode text` (`interest_mode` already existed). Both `*_value` columns are generic numerics interpreted per the sibling `*_mode`.
- Drop `discount_value`. Keep `discount_mode text` on `charges` — shared modalidade for all of a charge's discount entries.
- New table `charge_discounts(id, tenant_id, charge_id, sequence smallint CHECK 1-3, discount_date date, value numeric)`, `UNIQUE(charge_id, sequence)`, RLS-scoped like `charges`.
- `due_date`, `validity_after_days`, `abatement_value` unchanged from Phase 1's migration.
- `ck_cob_fields`/`ck_cobv_fields` updated for the renamed columns; `charge_discounts` rows valid only when `kind='cobv' AND discount_mode IS NOT NULL`.

## 6. API

```Text
POST /api/v1/charges        # always creates kind=cobv; due_date required + optional fine/interest/discount/abatement
GET  /api/v1/charges/{id}   # returns due-date terms + amount_due breakdown for cobv
```

`GET /api/v1/charges` (list + filter) deferred to Phase 3.

## 7. Key flow

Identical persist-first two-phase as Phase 1, calling `PUT /v2/cobv/:txid`. Differences are payload construction (calendario/devedor/valor + multa/juros/desconto/abatimento), response mapping, and the `amount_due` computation on `GET /charges/{id}` (local, no provider call).

## 8. Provider / SDK

`PixProvider` port gains `CreateDueDateCharge`. `EfiProvider`/`efiClient` gain `CreateCobV`, wrapping the SDK's confirmed `CreateDueCharge(txid, body) -> PUT /v2/cobv/:txid` (`endpoints_pix.go`). No cobv "get" needed (§2). EFí CobV body nests `calendario.dataDeVencimento`, `calendario.validadeAposVencimento`, `valor.original`, `valor.multa`, `valor.juros`, `valor.desconto` (`{modalidade, descontoDataFixa: [{data, value}]}`), `valor.abatimento`. Exact EFí modalidade codes for fine/interest modes — record in `docs/efi-sdk-review.md` during the plan.

## 9. Cross-cutting

No new cross-cutting; reuses idempotency, outbox, RLS, logging from Phase 1.

## 10. Dependencies

Phase 1 (charge aggregate, provider pool, API, idempotency). No webhook dependency.

## 11. Risks / open items

- ~~EFí CobV discount modeling~~ → resolved: `charge_discounts` table, date-banded only ([ADR-0005](../../adr/0005-cobv-due-date-rule-schema.md)).
- ~~Rounding rules for fine/interest/discount~~ → resolved: round half-up per component, total clamped `>=0` (§4).
- Interest/fine mode enumerations must match EFí's accepted codes exactly — record in the SDK review (`docs/efi-sdk-review.md`).
- `tzdata` availability in the deploy image (America/Sao_Paulo date math, §3) — confirm during the plan.

## 12. Exit criteria

- CobV created end-to-end against EFí homologation with fine/interest/discount/abatement applied.
- Rule calculations (`EffectiveAmount`) unit-tested across on-time / late / early / abatement scenarios with exact centavos, including the discount-entry-selection and monthly-interest-pro-ration rules (§4).
- `GET /api/v1/charges/{id}` returns the `amount_due` breakdown for cobv charges.
- CHECK + validation reject mismatched cob/cobv fields and out-of-range rule values (§3).
- Phase 1's `internal/charge/api/e2e_test.go` rewritten for the cobv-only `POST /charges` ([ADR-0006](../../adr/0006-post-charges-cobv-only.md)); `domain.NewImmediate`'s existing unit tests remain green.
- `≥80%` coverage on the new domain rules.

## 13. Testing focus

Heavy unit testing of `DueDateTerms`/`EffectiveAmount` math (boundary dates incl. the America/Sao_Paulo "today" edge cases, rounding, discount-entry selection, combined fine+interest+discount+abatement); contract test of the cobv adapter against fixtures; homologation smoke for a real CobV.
