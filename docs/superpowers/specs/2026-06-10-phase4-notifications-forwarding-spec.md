# Phase 4 Spec — Notifications & Webhook Forwarding

**Date:** 2026-06-10
**Status:** Approved — pending implementation plan
**Master design:** [pix-payment-platform-design](2026-06-09-pix-payment-platform-design.md) · **Glossary:** [CONTEXT.md](../../../CONTEXT.md)
**Decisions:** master §8 (single event path), §10/§15.1 (notification model), §6 (`webhook_subscriptions`)
**Depends on:** Phase 3 ([spec](2026-06-10-phase3-webhooks-lifecycle-refunds-spec.md)) — consumes the events it publishes

---

## 1. Goal

Turn domain events into outbound communication: **customer notifications** (email / SMS / WhatsApp) to the payer, and **webhook forwarding** (HMAC-signed egress) to partner systems that subscribe to platform events. Both are idempotent RabbitMQ consumers fed by the Phase 3 outbox relay.

## 2. Scope

**In scope**
- `Notifier` abstraction + email / SMS / WhatsApp implementations (provider-global integrations, per-tenant sender identity — master §15.1).
- Customer-notify consumer: on `ChargeCreated`, `ChargeDueSoon`, `ChargeOverdue`, `ChargePaid`, `RefundCompleted` → resolve recipient (payer contact on the charge) → render → send → log; idempotent dedup on `(event_id, channel)`.
- Webhook **forwarding**: `webhook_subscriptions` registration API; a separate egress consumer that fans out matching events to subscriber URLs, HMAC-SHA256 signed, with retry → DLQ; per-attempt `webhook_delivery_logs`.
- Notification persistence: `notifications` + `notification_logs`.
- Retry + DLQ policy for both consumers (DLQ scaffolded in Phase 3, exercised here); emit `NotificationSent`/`NotificationFailed`.

**Out of scope**
- Reporting on notification/forwarding metrics (Phase 5 reads these logs).
- Full observability dashboards (Phase 6).

## 3. Functional requirements

- Each relevant domain event fans out to (a) zero-or-one customer notification per enabled channel with a resolvable recipient, and (b) every active `webhook_subscription` whose `event_types` include the event.
- Customer channels: platform-global provider creds (SMTP / SMS gateway / WhatsApp BSP); per-tenant sender identity (from-name, from-email, reply-to, WhatsApp sender) and per-channel enable/disable.
- Forwarding deliveries are signed `HMAC-SHA256` over the body with the subscription's secret (in SecretProvider); partners verify authenticity. Each delivery attempt is logged (HTTP status, response snippet, attempt #, outcome).
- Both consumers are **idempotent**: redelivered events (at-least-once) don't double-send — customer dedup on `(event_id, channel)`, forwarding dedup on `(event_id, subscription_id)`.
- Failures retry with exponential backoff + jitter; exhausted retries land in a per-consumer DLQ for inspection/replay.
- **PII minimization**: forwarded event payloads exclude raw CPF/CNPJ unless the subscription is explicitly authorized; customer messages never log raw documents.

## 4. Domain changes

- `Notification` entity (channel, event_type, recipient, status) + `notification_logs` attempts. (Notifications target the payer; not part of the Charge aggregate — own small module.)
- `WebhookSubscription` entity (target_url, event_types, signing_secret_ref, status).
- Events consumed (from Phase 3 + Phase 1): `ChargeCreated`, `ChargeDueSoon`, `ChargeOverdue`, `ChargePaid`, `RefundCompleted`. Events emitted: `NotificationSent`, `NotificationFailed`.

## 5. Data model changes

New tables: `webhook_subscriptions`, `webhook_delivery_logs`, `notifications`, `notification_logs` (all tenant-scoped + RLS). Recipient contact reuses the payer fields already on `charges` (Phase 1).

## 6. API

```Text
POST /api/v1/webhooks/register       # create/configure a WebhookSubscription (forwarding)
GET  /api/v1/webhooks/subscriptions  # list a tenant's subscriptions
# (notification channel config per tenant: admin/config surface — finalize in plan)
```

## 7. Key flow

Outbox relay (Phase 3) publishes to the RabbitMQ topic exchange → two queues bound to the relevant routing keys: (1) customer-notify consumer, (2) forwarding consumer. Each consumes idempotently, performs side effects, logs, and acks; failures retry then DLQ.

## 8. Provider / SDK

No EFí SDK changes. New external integrations: an email provider (SMTP or API), an SMS gateway, a WhatsApp BSP — each behind a `Notifier` channel implementation; creds in SecretProvider (platform-level keys).

## 9. Cross-cutting

Consumes the Phase 3 event transport (RabbitMQ + outbox relay). Exercises retry + DLQ in anger. No new infra beyond the message consumers and outbound HTTP client (with timeouts).

## 10. Dependencies

Phase 3 (events published to RabbitMQ; `webhook_logs`/refund/charge events). Phase 1 payer contact data.

## 11. Risks / open items

- Per-tenant vs platform-global provider credentials — Phase-4 default is global integrations + per-tenant sender identity (master §15.1); revisit if a product needs its own gateway.
- WhatsApp BSP template approval + opt-in compliance.
- Notification templating/localization approach (per-tenant templates?) — decide in the plan.
- Forwarding endpoint security: enforce HTTPS targets, SSRF protection (block internal IPs).

## 12. Exit criteria

- A `ChargePaid` event produces a customer notification (sent + logged) AND a forwarded, HMAC-valid delivery to a subscriber.
- Redelivered events don't double-send (idempotent consumers).
- Induced channel failure retries per policy and lands in the DLQ; DLQ replay works.
- PII is absent from unauthorized forwarded payloads.

## 13. Testing focus

Consumer idempotency (duplicate event_id); HMAC signature verification by a test subscriber; retry/backoff + DLQ; SSRF/HTTPS guard on forwarding targets; template rendering; channel-failure handling.
