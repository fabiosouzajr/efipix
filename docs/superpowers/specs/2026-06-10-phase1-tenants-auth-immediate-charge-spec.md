# Phase 1 Spec — Tenants, EFí Auth & Immediate Charges (Cob)

**Date:** 2026-06-10
**Status:** Approved — implementation plan exists ([phase1 plan set](../plans/2026-06-10-phase1-00-overview.md))
**Master design:** [pix-payment-platform-design](2026-06-09-pix-payment-platform-design.md) · **Glossary:** [CONTEXT.md](../../../CONTEXT.md)
**Decisions:** [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md) (status), [ADR-0002](../../adr/0002-client-defined-txid-persist-first.md) (txid/persist-first), [ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md) (tenancy)

---

## 1. Goal

Stand up the multi-tenant foundation and the first money path: a client application authenticates by API key, resolves to a tenant + EFí account, and creates/fetches an **immediate Pix charge (Cob)** end-to-end against EFí, with client-defined txid, persist-first audit, and required idempotency.

## 2. Scope

**In scope**
- Foundation (the Phase 0 subset this path needs): Go module + Clean-Arch skeleton, config, structured logging with PII masking, pgx pool + RLS transaction helper, goose migrations, sqlc config, health endpoints, docker-compose (Postgres/Redis/RabbitMQ/nginx mTLS proxy), CI.
- Tenancy: `tenants`, `api_keys`, `payment_providers`, `pix_keys` with RLS; API-key authentication; `TenantResolver` + `ProviderResolver` middleware.
- Secrets: `SecretProvider` (env-JSON impl) keyed by `payment_provider_id`; P12→PEM conversion.
- Provider: `PixProvider` port; `EfiProvider` with a per-account SDK client pool (PEM cert, SDK-managed OAuth).
- Charge aggregate (Cob only): domain entity, `Payer` VO, status machine `CREATED→ACTIVE|FAILED`, `payment_events` audit; client-defined txid; persist-first two-phase create.
- Idempotency store (DB-backed) + required `Idempotency-Key` on `POST /charges`.
- Transactional outbox **table** + write-on-commit (relay deferred — §9).
- API: `POST /api/v1/charges` (immediate), `GET /api/v1/charges/{id}`.

**Out of scope (later phases)**
- Due-date charges / fine·interest·discount (Phase 2).
- Webhooks, payment settlement, refunds, lifecycle beyond ACTIVE/FAILED, reconciliation (Phase 3).
- Notifications, webhook forwarding (Phase 4).
- Reporting (Phase 5).
- OTel tracing, Prometheus metrics, retry, circuit breaker, RabbitMQ relay/consumers, RBAC, rate limiting, Vault/AWS-SM secret backends, Helm/K8s (Phase 6 / as noted).

## 3. Functional requirements

- A client app calls the API with `X-Api-Key` (or `Authorization: ApiKey …`); optional `X-Provider-Id` selects a non-default account.
- Every request executes in exactly one tenant context; cross-tenant reads are impossible (RLS + repo filter).
- `POST /api/v1/charges` with an immediate-charge body returns `201` with `{ txid, chargeId, status, amount, qr_code_image, pix_payload, location_id }`.
- The platform mints the txid (client-defined; 26–35 alphanumeric) and records the charge as `CREATED` **before** calling EFí; on EFí success → `ACTIVE` with QR + payload; on EFí failure → `FAILED` with an audit event and a `502` response.
- `Idempotency-Key` is required on create; same key+body replays the original response (same txid); same key+different body → `422`; missing key → `400`.
- `GET /api/v1/charges/{id}` returns the charge, tenant-scoped (`404` if absent/other tenant).

## 4. Domain changes

- `Charge` aggregate root (Cob): identity, `Kind=cob`, `ChargeStatus`, `Amount` (centavos), `PixKey`, `ExpirationSeconds`, `Payer` VO (doc/docType/name/email/phone), `ExternalReference`, `Version`, pending `PaymentEvent`s.
- Status subset used this phase: `CREATED`, `ACTIVE`, `FAILED` (full 8-value enum defined per [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md)).
- Transitions: `NewImmediate` (→CREATED, event "created"), `MarkActive` (→ACTIVE, event "activated"), `MarkFailed` (→FAILED, event "failed"). Guards reject illegal moves.
- Domain raises no external events yet except `ChargeCreated` written to the outbox on activation (no consumer until Phase 4).

## 5. Data model changes

New tables (all tenant-scoped, RLS-forced): `tenants`, `api_keys` (addendum to master §6), `payment_providers`, `pix_keys`, `charges`, `payments`, `payment_events`, `outbox`, `idempotency_keys`. CobV columns exist on `charges` (nullable) so Phase 2 needs no ALTER. Key constraints: `UNIQUE(tenant_id, txid)`; `UNIQUE(tenant_id, e2e_id)` on payments; `idempotency_keys` PK `(tenant_id, key)`; CHECK enforcing cob/cobv column pairing. RLS GUC `app.tenant_id` set via `set_config(...,true)` ([ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md)).

## 6. API

```Text
POST /api/v1/charges        # immediate (Cob); Idempotency-Key REQUIRED
GET  /api/v1/charges/{id}
GET  /health /ready /live
```

## 7. Key flow

Create immediate charge — persist-first two-phase (master §7.1, [ADR-0002](../../adr/0002-client-defined-txid-persist-first.md)): validate → resolve tenant+provider → idempotency reserve → mint txid → insert CREATED (tx A) → `PUT /v2/cob/:txid` → MarkActive + outbox `ChargeCreated` (tx B) or MarkFailed (tx B) → respond.

## 8. Provider / SDK

`EfiProvider` isolates `github.com/efipay/sdk-go-apis-efi`. Per master §16: cert is PEM (P12→PEM) bound per client instance ⇒ one cached SDK client per `payment_provider_id`; SDK manages OAuth. Phase-1 task: confirm SDK method names + record in `docs/efi-sdk-review.md`.

## 9. Cross-cutting introduced

Idempotency (DB authority); transactional outbox **table** + write within the charge tx. The outbox **relay → RabbitMQ** and consumers are **deferred to Phase 3/4** (no consumer needs them yet). Structured JSON logging with CPF/CNPJ masking is in from the start.

## 10. Dependencies

None (greenfield). Produces the foundation every later phase builds on.

## 11. Risks / open items

- SDK exact method names/error shapes — confirm in implementation.
- `payer_doc` at-rest encryption deferred to Phase 6.
- Local dev needs EFí homologation credentials for the end-to-end smoke; unit/integration use a fake provider.

## 12. Exit criteria

- Create + fetch an immediate charge end-to-end against EFí homologation.
- Forced EFí failure → `FAILED` charge + audit event + `502`.
- Idempotency: missing→400, replay→same txid, conflict→422.
- RLS denies cross-tenant reads (proven in a test).
- `≥80%` coverage on `internal/charge/domain` + `internal/charge/app`.
- `docs/efi-sdk-review.md` committed; CI (lint+test+build) green.

## 13. Testing focus

Domain unit (validation, txid, transitions); integration (repos + RLS + idempotency via testcontainers); EfiProvider mapping/pooling (fake client); end-to-end create/replay/conflict/failure (fake provider); homologation smoke (tagged, manual).
