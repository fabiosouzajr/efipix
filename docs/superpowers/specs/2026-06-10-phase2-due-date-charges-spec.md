# Phase 2 Spec — Due-Date Charges (CobV) with Fine, Interest, Discount, Abatement

**Date:** 2026-06-10
**Status:** Approved — pending implementation plan
**Master design:** [pix-payment-platform-design](2026-06-09-pix-payment-platform-design.md) · **Glossary:** [CONTEXT.md](../../../CONTEXT.md)
**Decisions:** [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md) (status, derived OVERDUE/EXPIRED), [ADR-0002](../../adr/0002-client-defined-txid-persist-first.md) (txid/persist-first)
**Depends on:** Phase 1 ([spec](2026-06-10-phase1-tenants-auth-immediate-charge-spec.md))

---

## 1. Goal

Add **due-date charges (CobV)** to the existing charge aggregate: a charge with a due date plus payer-charge rules — **multa** (fine), **juros** (interest), **desconto** (discount), **abatimento** (abatement) — created against EFí `PUT /v2/cobv/:txid`, reusing Phase 1's persist-first/idempotency/provider machinery.

## 2. Scope

**In scope**

- `ChargeKind=cobv` path through the same `Charge` aggregate and `POST /api/v1/charges`.
- `DueDateTerms` value object + calculation rules for multa, juros, desconto, abatimento.
- CobV provider mapping in `EfiProvider` (cobv create + detail).
- Typed CobV columns already present (Phase 1 migration) — Phase 2 populates and validates them; no new tables.
- Validation + CHECK enforcement of cob/cobv field pairing.

**Out of scope**

- Webhook settlement, lifecycle beyond ACTIVE, OVERDUE/EXPIRED transitions and the scheduler that emits them (Phase 3). Phase 2 only defines OVERDUE/Due-soon as derived predicates conceptually; the emitting scheduler lands in Phase 3.
- Notifications about due/overdue (Phase 4).

## 3. Functional requirements

- `POST /api/v1/charges` accepts a due-date body: `due_date`, optional `validity_after_days`, and optional rule blocks:
  - **multa**: mode (fixed value or percent) + value.
  - **juros**: mode (e.g. daily/monthly percent) + value.
  - **desconto**: mode (fixed/percent, possibly date-banded) + value.
  - **abatimento**: fixed value.
- The charge is created at EFí as CobV with these terms; response mirrors Phase 1 plus the due-date terms.
- Rule math is computed and unit-verified for representative scenarios (on-time, late with fine+interest, early with discount, with abatement). The **authoritative settled amount comes from EFí/webhook** (Phase 3); Phase 2's calculations are for quoting/validation and reporting projections.
- Validation: `due_date` required and not in the past; rule values within EFí's accepted ranges; cob-only fields rejected for cobv and vice-versa (CHECK + app validation).

## 4. Domain changes

- `DueDateTerms` VO: `DueDate`, `ValidityAfterDays`, `Fine{Mode,Value}`, `Interest{Mode,Value}`, `Discount{Mode,Value}`, `Abatement value`.
- `Charge` gains a `NewDueDate(params)` constructor (→CREATED, `Kind=cobv`, event "created"); `MarkActive` reused unchanged.
- Pure rule functions (no I/O): `EffectiveAmount(terms, base, asOf)` returning the computed amount components (fine/interest/discount/abatement applied) for a given date — used by quoting + Phase 5 reports.
- **Overdue / Due-soon** defined as derived predicates over CobV (per [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md)); the scheduler that emits `ChargeOverdue`/`ChargeDueSoon`/`ChargeExpired` is Phase 3.

## 5. Data model changes

None new. Populates existing `charges` columns: `due_date`, `validity_after_days`, `fine_percent`/fine value, `interest_mode`/`interest_percent`, `discount_mode`/`discount_value`, `abatement_value`. (If a richer discount model — multiple date-banded discounts — is required, add a `charge_discounts` child table; decide during the Phase 2 plan based on EFí's CobV discount schema.)

## 6. API

```Text
POST /api/v1/charges        # now accepts kind=cobv with due_date + multa/juros/desconto/abatimento
GET  /api/v1/charges/{id}   # returns due-date terms for cobv
GET  /api/v1/charges        # (introduced here or Phase 3) filterable list; at minimum status/kind
```

## 7. Key flow

Identical persist-first two-phase as Phase 1, calling `PUT /v2/cobv/:txid`. Differences are payload construction (calendario/devedor/valor + multa/juros/desconto/abatimento) and response mapping.

## 8. Provider / SDK

Extend `EfiProvider`/`efiClient` with `CreateCobV` + cobv detail. Confirm the SDK's cobv method name + body schema during the plan (continuing `docs/efi-sdk-review.md`). EFí CobV body nests `calendario.dataDeVencimento`, `calendario.validadeAposVencimento`, `valor.original`, `valor.multa`, `valor.juros`, `valor.desconto`, `valor.abatimento`.

## 9. Cross-cutting

No new cross-cutting; reuses idempotency, outbox, RLS, logging from Phase 1.

## 10. Dependencies

Phase 1 (charge aggregate, provider pool, API, idempotency). No webhook dependency.

## 11. Risks / open items

- EFí CobV discount modeling (single vs multiple date-banded discounts) — confirm schema; may add `charge_discounts` table.
- Interest/fine mode enumerations must match EFí's accepted codes exactly — record in the SDK review.
- Rounding rules for fine/interest/discount (centavos) — define and test explicitly.

## 12. Exit criteria

- CobV created end-to-end against EFí homologation with multa/juros/desconto/abatimento applied.
- Rule calculations unit-tested across on-time / late / early / abatement scenarios with exact centavos.
- CHECK + validation reject mismatched cob/cobv fields.
- `≥80%` coverage on the new domain rules.

## 13. Testing focus

Heavy unit testing of `DueDateTerms` math (boundary dates, rounding, combined fine+interest+discount); contract test of the cobv adapter against fixtures; homologation smoke for a real CobV.
