# 5. CobV due-date rule schema: charge_discounts table and generic fine/interest value columns

Date: 2026-06-12
Status: Accepted

## Context

[Phase 2 spec](../superpowers/specs/2026-06-10-phase2-due-date-charges-spec.md) §5 assumed the nullable CobV columns added by Phase 1's migration (`fine_percent`, `interest_mode`/`interest_percent`, `discount_mode`/`discount_value`, `abatement_value`) would suffice with no new tables, deferring only the desconto question ("if a richer discount model ... is required, add a `charge_discounts` table").

Reviewing EFí's CobV body (`valor.multa`/`valor.juros`/`valor.desconto`, confirmed against `efipay/sdk-go-apis-efi` examples) surfaced two gaps:

1. **multa and juros support both "valor fixo" and "percentual" modalidades**, but `fine_percent`/`interest_percent` are percent-only — there's no column to hold a fixed-centavos fine/interest value.
2. **desconto can be a date-banded list** of up to 3 `(data, value)` entries (`descontoDataFixa`), one shared `modalidade` across all entries. A pair of scalar columns can't represent that.

## Decision

- Rename `fine_percent` → `fine_value` and `interest_percent` → `interest_value` (generic numeric, interpreted per the sibling `*_mode`); add `fine_mode text` (`interest_mode` already existed from Phase 1).
- Drop `discount_value`. Keep `discount_mode text` on `charges` — one modalidade (fixed | percent) shared by all of a charge's discount entries, matching EFí's singular `desconto.modalidade`.
- New table `charge_discounts(id, tenant_id, charge_id, sequence smallint CHECK 1-3, discount_date date, value numeric)`, `UNIQUE(charge_id, sequence)`, RLS-scoped like `charges`.
- Scope: only **date-banded** desconto (EFí modalidades 1-2). Antecipação-rate desconto (modalidades 3-6: a single fixed/percent per-day rate, no dates) is **not** supported in Phase 2.

## Consequences

- These columns were added by Phase 1's migration but never populated (CobV was unimplemented until now), so renaming/dropping them in Phase 2's migration is safe — no data backfill needed.
- `ck_cob_fields`/`ck_cobv_fields` (from `db/migrations/00003_charges.sql`) must be updated for the renamed columns, and `charge_discounts` rows are only meaningful when `kind='cobv' AND discount_mode IS NOT NULL`.
- If antecipação-rate desconto is needed later, it's additive: new scalar columns on `charges` plus a CHECK ensuring a charge doesn't mix antecipação columns with `charge_discounts` rows.
