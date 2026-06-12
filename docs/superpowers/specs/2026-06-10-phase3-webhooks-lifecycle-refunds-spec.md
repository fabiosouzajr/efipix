# Phase 3 Spec — Webhooks, Payment Lifecycle, Refunds & Reconciliation

**Date:** 2026-06-10
**Status:** Approved — pending implementation plan
**Master design:** [pix-payment-platform-design](2026-06-09-pix-payment-platform-design.md) · **Glossary:** [CONTEXT.md](../../../CONTEXT.md)
**Decisions:** [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md) (status + derived states), [ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md) (webhook mTLS), master §7.2/§7.3/§16
**Depends on:** Phase 1 ([spec](2026-06-10-phase1-tenants-auth-immediate-charge-spec.md)), Phase 2 ([spec](2026-06-10-phase2-due-date-charges-spec.md))

---

## 1. Goal

Make EFí webhooks the **authoritative payment source**: securely ingest batched Pix notifications, settle charges (`PAID`), record refunds (devolução) through their async lifecycle, complete the full charge state machine, and add a reconciliation backstop + the time-derived scheduler. This phase also introduces the **outbox relay → RabbitMQ** so downstream phases can consume events.

## 2. Scope

**In scope**
- Secure webhook ingestion endpoint behind the nginx mTLS proxy ([ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md)): hmac-query + IP-allowlist validation, request-level audit (`webhook_logs`), 60s fast-ack.
- **Batch `pix[]`** processing with per-item classification (received / devolução / sent) and per-`e2eId` idempotent dedup (`UNIQUE(tenant_id, e2e_id)`).
- `payments` settlement + `payment_events`; full charge state machine (CREATED, ACTIVE, PENDING, PAID, EXPIRED, CANCELLED, REFUNDED, FAILED).
- `unmatched_payments` for pix with no/unknown txid.
- **Refunds (devolução)**: `POST /api/v1/refunds`, two-phase create (`PUT /v2/pix/:e2eId/devolucao/:id`), partial + multiple with `Σ ≤ paid`, async completion via webhook (`devolucoes[]`) or reconciliation; derived `REFUNDED`.
- **Outbox relay → RabbitMQ** topic exchange + the events `ChargePaid`, `ChargeExpired`, `RefundRequested`, `RefundCompleted` (plus Phase 1's `ChargeCreated`).
- **Scheduler**: emits `ChargeDueSoon`/`ChargeOverdue`, performs `ACTIVE→EXPIRED` (+`ChargeExpired`), cleans up stuck `CREATED`, runs reconciliation.
- Webhook registration management: configure EFí webhook per Pix key (`PUT /v2/webhook/:chave`), store registered keys + hmac secret reference in `payment_providers.webhook_config` + SecretProvider.
- `GET /api/v1/charges` list/filter (status, kind, overdue, paging) and `GET /api/v1/refunds/{id}`.

**Out of scope**
- Sending notifications / forwarding events to subscribers (Phase 4 consumes the events this phase publishes).
- Reporting queries (Phase 5).

## 3. Functional requirements

- EFí POSTs are accepted only when hmac + source IP `34.193.116.226` validate (mTLS terminated at proxy); the raw body is stored once (`webhook_logs`) for audit and acked within 60s after durable acceptance.
- Each `pix[]` item is classified: `devolucoes[]` ⇒ refund; `tipo`+`status` ⇒ sent (ignored); else ⇒ received payment.
- A received payment matched by txid settles its charge: insert `payment(e2eId)` (idempotent), transition charge to `PAID`, emit `ChargePaid`. Re-delivered e2eIds are skipped via the unique constraint. Unmatched payments are recorded, never dropped, and emit a metric.
- A refund notification updates the matching refund: `PROCESSING→COMPLETED|FAILED`; when `Σ refunds == paid`, the charge becomes `REFUNDED`; emit `RefundCompleted`.
- `POST /api/v1/refunds` validates `Σ ≤ paid`, persists `REQUESTED`, mints the devolução id, calls EFí, moves to `PROCESSING`, emits `RefundRequested`.
- The scheduler (configurable cadence): emits `ChargeDueSoon`/`ChargeOverdue` once per cadence per charge; flips overdue-past-validity (and Cob past expiration) to `EXPIRED`; resolves stuck `CREATED`; reconciles `ACTIVE`/`PROCESSING` items via `GetCharge`/`GetRefund` and corrects drift.
- All emitted events flow through the outbox → relay → RabbitMQ (at-least-once).

## 4. Domain changes

- Refund entity (inside the Charge aggregate): `devolucaoId`, `amount`, status `REQUESTED→PROCESSING→COMPLETED|FAILED`; aggregate invariant `Σ refund.amount ≤ payment.amount`.
- Charge transitions added: `MarkPaid(payment)`, `MarkExpired`, `MarkCancelled`, refund-driven `MarkRefunded` (derived), and `MarkPending` (amount-mismatch review). Guards per [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md).
- Domain events: `ChargePaid`, `ChargeExpired`, `ChargeDueSoon`, `ChargeOverdue`, `RefundRequested`, `RefundCompleted`.

## 5. Data model changes

New tables: `refunds`, `webhook_logs`, `unmatched_payments`. (`payments`, `payment_events`, `outbox` exist from Phase 1.) Constraints: `UNIQUE(tenant_id, devolucao_id)` on refunds; `webhook_logs` request-hash index; `unmatched_payments(e2e_id, txid, raw)`. Add `charges.last_reconciled_at` and per-charge "last emitted" tracking for the scheduler (column or a small `charge_schedule_state` table — decide in the plan). Outbox gains `sent_at` relay bookkeeping (column exists).

## 6. API

```Text
POST /api/v1/refunds                 # Idempotency-Key REQUIRED
GET  /api/v1/refunds/{id}
GET  /api/v1/charges                 # list + filter (status, kind, overdue, paging)
POST /api/v1/webhooks/efi            # EFí inbound (behind nginx mTLS proxy) — not public-listed
# webhook registration is an internal/admin operation against EFí, not a public client route
```

## 7. Key flows

Webhook ingestion (master §7.2): proxy mTLS → hmac+IP → store raw → per-item tx with e2e dedup → settle/refund/unmatched → 200. Refund (master §7.3): two-phase, async completion over the same webhook. Scheduler (master §7.4): time-derived events + reconciliation, all via outbox.

## 8. Provider / SDK

Extend `EfiProvider`/`efiClient`: `Refund` (`PUT /v2/pix/:e2eId/devolucao/:id`), `GetRefund`, `GetCharge` (reconciliation), webhook config/list. Confirm method names + the devolução/webhook payload shapes against the SDK; the webhook payload field names are pinned in master §16.

## 9. Cross-cutting introduced

**Outbox relay → RabbitMQ** (topic exchange, routing key per event type) + a generic Rabbit publisher; consumers are idempotent and arrive in Phase 4. DLQ wiring for consumer failures is scaffolded here, exercised in Phase 4. The **scheduler** runtime (leader-safe periodic job; runs per-tenant or with an elevated RLS-bypass role).

## 10. Dependencies

Phases 1–2 (charge aggregate, provider pool, outbox table). Requires the nginx mTLS proxy from Phase 1's compose; production proxy/ingress is hardened in Phase 6.

## 11. Risks / open items

- Confirm EFí pushes devolução status over the webhook vs poll-only — verify against homologation early; reconciliation is the fallback either way.
- Exact webhook signature/mTLS contract + whether `x-skip-mtls-checking` is required for the chosen ingress.
- Scheduler idempotency for `DueSoon`/`Overdue` (fire-once semantics) and leader election if multiple replicas run it.
- Amount-mismatch policy (when a payment value ≠ charge amount): define PENDING handling.

## 12. Exit criteria

- A batched webhook settles multiple charges idempotently; replayed e2eIds are skipped; unmatched recorded.
- A partial refund processes and completes via webhook-or-reconciliation; full refund flips charge to `REFUNDED`.
- Scheduler emits DueSoon/Overdue, expires overdue-past-validity, and reconciliation recovers a simulated missed webhook.
- Events land in RabbitMQ via the outbox relay (verified by a test consumer).
- `≥80%` coverage on lifecycle + refund domain.

## 13. Testing focus

Webhook tests (batch `pix[]`, signature/hmac, IP, replay, dedup, unmatched, malformed); refund lifecycle + invariant; state-machine guards; outbox-relay round-trip to a test Rabbit consumer; reconciliation drift correction; scheduler fire-once.
