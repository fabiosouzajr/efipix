# 1. Charge lifecycle status model

Date: 2026-06-09
Status: Accepted

## Context

The platform must track and report charge state, and downstream consumers (notifications, reporting, webhook forwarding, client applications) depend on a stable status vocabulary. EFí's Pix API reports only four cob/cobv statuses — `ATIVA`, `CONCLUIDA`, `REMOVIDA_PELO_USUARIO_RECEBEDOR`, `REMOVIDA_PELO_PSP` — which is narrower than the platform needs:

- EFí keeps an unpaid charge `ATIVA` past its expiration; it never emits an "expired" status.
- Pix settlement is instant, so there is no provider notion of a "pending" charge.
- A Refund (devolução) carries its own provider status (`EM_PROCESSAMENTO`, `DEVOLVIDO`, `NAO_REALIZADO`), separate from the charge.

We need an internal status model that is provider-agnostic (a future provider must map onto it) and richer than EFí's, while staying honest about which states are provider-reported versus platform-derived.

## Decision

The Charge aggregate owns an 8-value internal status and guards all transitions (illegal moves such as PAID→ACTIVE are rejected). A provider→internal mapping lives in the adapter layer.

| Internal status | Source / trigger |
| --- | --- |
| CREATED | local insert, before the provider call |
| ACTIVE | EFí `ATIVA` confirmed |
| PENDING | payment reported but not validated (amount mismatch / awaiting reconciliation) — transient review state, rarely hit in v1 |
| PAID | EFí `CONCLUIDA` / full amount settled (driven by webhook) |
| EXPIRED | **platform-derived**: scheduled job + lazy check on read flips ACTIVE→EXPIRED past expiration / due_date+validity while unpaid |
| CANCELLED | EFí `REMOVIDA_PELO_USUARIO_RECEBEDOR` / `REMOVIDA_PELO_PSP`, or platform-initiated cancel |
| REFUNDED | **platform-derived**: refunded total == paid total. A partial refund leaves the charge PAID |
| FAILED | provider charge creation failed |

Refund status is tracked separately on the Refund entity and never overwrites charge status except via the derived REFUNDED rule.

Provider status mapping (EFí):
- `ATIVA` → ACTIVE
- `CONCLUIDA` → PAID
- `REMOVIDA_PELO_USUARIO_RECEBEDOR` / `REMOVIDA_PELO_PSP` → CANCELLED

## Derived conditions (not statuses)

Some lifecycle signals the platform must report and notify on are **time-derived predicates**, not stored statuses:

- **Overdue** = `kind=cobv AND due_date < today AND status=ACTIVE`. A past-due CobV is still payable (fine/interest is the point), so it stays ACTIVE — Overdue never becomes a stored status. Drives the `ChargeOverdue` event and the overdue report.
- **Due soon** = `kind=cobv AND due_date within N days AND status=ACTIVE` (N configurable per tenant). Drives `ChargeDueSoon`.

A single periodic **scheduler** scans active charges and emits these as domain events through the outbox (same delivery path as all events), and performs the `ACTIVE→EXPIRED` transition (Cob past expiration; CobV past `due_date + validity_after_days`), emitting `ChargeExpired`. The scheduler tracks last-emitted per charge/event so `DueSoon`/`Overdue` fire once (or once per configured cadence), not on every scan.

## Consequences

- EXPIRED requires a scheduled job and a lazy-on-read check; it is never sourced from EFí. A charge can read as ACTIVE in EFí while EXPIRED locally — this is intended and the local view governs platform behaviour.
- REFUNDED is computed from refund totals, not reported; reporting must derive "fully refunded" from refunds, not assume a provider flag.
- PENDING exists mainly as a safety state for amount-mismatch / reconciliation review; v1 may rarely populate it. Kept to avoid a future migration if mismatch handling grows.
- Consumers can rely on a stable 8-value enum independent of provider. Adding a provider means writing a new mapping table, not changing the enum.
- Transitions are enforced in one place (the Charge entity), so audit (PaymentEvent) and optimistic locking are consistent.
