# Design Spec — Enterprise Pix Payment Platform (EFí)

**Date:** 2026-06-09 (revised 2026-06-10 after grilling session)
**Status:** Approved design — ready for implementation planning
**Source requirements:** [docs/prompt.md](../../prompt.md)
**Glossary:** [CONTEXT.md](../../../CONTEXT.md) · **Decisions:** [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md), [ADR-0002](../../adr/0002-client-defined-txid-persist-first.md), [ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md), [ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md)
**Deliverable of this doc:** design + phased roadmap. No application code yet.

---

## 1. Goal & scope

Build a production-grade, standalone Pix payment **microservice** that becomes the centralized payment infrastructure for all company products. It exposes REST APIs, emits events, and ingests EFí webhooks. It supports multiple independent EFí accounts (multi-tenant) and is designed so EFí can be replaced by another Pix provider without major refactoring.

Functional surface:

- Immediate Pix charges (Cob) and due-date charges (CobV)
- Fine / interest / discount / abatement (multa, juros, desconto, abatimento)
- QR code + Pix copy-and-paste payload generation
- Payment lifecycle + status tracking, reconciliation
- Secure EFí webhook ingestion (authoritative payment source)
- Notifications (email / SMS / WhatsApp) + webhook forwarding to partner systems
- Financial / collection / operational reporting with CSV / XLSX / JSON export
- Multi-tenant, containerized, stateless

---

## 2. Key decisions (locked)

| Decision | Choice | Rationale / ADR |
| --- | --- | --- |
| Architecture | Clean Architecture, **feature-first modular monolith** | Reuse across products; per-module testing; modules extractable later |
| Aggregate model | **Charge is the single aggregate root**; Payment, Refund, PaymentEvent are entities inside it | One tx + one optimistic lock per charge; no cross-module FK (Q1) |
| Multi-tenancy | **Shared DB, `tenant_id` row-scoping + Postgres RLS** | Lowest ops cost; RLS = defense-in-depth — [ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md) |
| Account identity | On **`payment_providers`**, not tenant; secrets in SecretProvider keyed by `payment_provider_id` | Multi-account / provider-swap — [ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md) |
| txid + creation | **Client-defined txid, persist-first two-phase** | Audit + retry-safety — [ADR-0002](../../adr/0002-client-defined-txid-persist-first.md) |
| Charge status | 8-value internal enum; EXPIRED/REFUNDED derived; OVERDUE/DueSoon = predicates | [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md) |
| Events | **Single path: outbox (in tx) → relay → RabbitMQ → idempotent consumers** | No in-process bus; at-least-once (Q8) |
| Language | Go 1.24+ | Per requirements |
| HTTP framework | **Gin** | Largest ecosystem |
| Data access | **pgx + sqlc** | Type-safe raw SQL, no ORM leakage |
| Migrations | **goose** | Plain SQL, ordered |
| Cache | Redis | Token cache, idempotency fast-path, rate limiting |
| Queue | RabbitMQ | Outbox relay target, async consumers, DLQ |
| Observability | OpenTelemetry + Prometheus + structured JSON logs | Per requirements |
| Provider SDK | `github.com/efipay/sdk-go-apis-efi` | Isolated entirely in `provider/efi/` |

**Dependency rule:** `domain ← application ← {infrastructure, api}`. Domain imports nothing outward. EFí SDK types never escape the adapter package.

---

## 3. Provider abstraction

The application talks only to ports. EFí is one implementation.

```go
// internal/provider — port
type PixProvider interface {
    CreateImmediateCharge(ctx context.Context, in *ImmediateChargeInput) (*Charge, error) // Cob (PUT /v2/cob/:txid)
    CreateDueDateCharge(ctx context.Context, in *DueDateChargeInput) (*Charge, error)      // CobV (PUT /v2/cobv/:txid)
    GetCharge(ctx context.Context, txid string) (*Charge, error)
    GenerateQRCode(ctx context.Context, locationID string) (*QRCode, error)
    Refund(ctx context.Context, in *RefundInput) (*Refund, error)                          // PUT /v2/pix/:e2eId/devolucao/:id
    GetRefund(ctx context.Context, e2eID, devolucaoID string) (*Refund, error)
}

// internal/provider/efi — adapter, SDK isolated here
type EfiProvider struct {
    clients ClientPool         // one efipay/sdk-go-apis-efi client per payment_provider_id (see below)
    secrets SecretProvider     // resolves PEM cert (CA+Key) + clientID/secret per provider
}
```

The adapter maps SDK structs to domain `Charge` / `Refund` / `QRCode`. **No SDK model is referenced outside `internal/provider/efi/`.** A future provider implements the same port with zero changes to application or domain code.

**SDK facts (verified against the repo + EFí docs — see §16):** the SDK loads the certificate as **PEM (`CA` + `Key`), per client instance** (not per call), and **manages the OAuth token internally**. Therefore: (a) **P12 certificates are converted to PEM** on ingest into the SecretProvider; (b) `EfiProvider` keeps a **client pool keyed by `payment_provider_id`** — one cached SDK client per EFí account — since cert/credentials are bound at client construction.

**Provider resolution:** each request resolves a tenant, then a `PaymentProvider` — explicit in the request or the tenant's `is_default`. The chosen `payment_provider_id` is recorded on the charge.

---

## 4. Folder structure

```Text
cmd/
  server/main.go              # composition root: config, DI wiring, graceful shutdown
internal/
  platform/                   # shared kernel (cross-cutting, no business logic)
    config/                   # env + Vault/AWS SM loader
    logging/ tracing/ metrics/
    db/                       # pgx pool, tx helpers, RLS session setter (SET LOCAL app.tenant_id)
    cache/                    # redis client
    mq/                       # rabbitmq publisher/consumer, DLQ
    idempotency/              # reserve/result store (redis fast-path + db unique authority)
    outbox/                   # transactional outbox store + relay
    events/                   # event types + producer (outbox append) + consumer (rabbit) helpers
    retry/ circuitbreaker/
    secrets/                  # SecretProvider abstraction (keyed by payment_provider_id / subscription id)
    tenant/                   # tenant + provider resolution middleware
    errors/                   # typed domain/app errors -> HTTP mapping
    httpmiddleware/           # auth, ratelimit, requestID, recovery
  charge/                     # the Charge aggregate: Cob + CobV + payments + refunds + events
    domain/                   # Charge root, DueDateTerms VO, Payer VO, Payment/Refund entities, state machine, domain events
    app/                      # use cases (create, get, list, refund), port interfaces
    infra/                    # ChargeRepository (pgx/sqlc), provider adapter calls
    api/                      # gin handlers, DTOs, validation
  scheduler/                  # time-derived events: DueSoon, Overdue, Expired transition + cleanup of stuck CREATED + reconciliation
  webhook/                    # ingestion (batch pix[]), signature, request-level audit, per-e2eId dispatch
    domain/ app/ infra/ api/
  notification/               # two consumers, distinct audiences:
    customer/                 #   email/sms/whatsapp to the payer
    forwarding/               #   HMAC-signed event egress to WebhookSubscriptions
    domain/ app/ infra/ api/
  reporting/                  # financial/collection/operational + exporters (csv/xlsx/json)
    app/ infra/ api/
  provider/                   # PixProvider port
    efi/                      # EfiProvider adapter (SDK isolated)
db/
  migrations/                 # goose .sql, ordered
  queries/                    # sqlc source .sql
api/
  openapi.yaml                # OpenAPI 3.1
deploy/
  docker/Dockerfile
  compose/docker-compose.yml
  k8s/                        # manifests
  helm/                       # chart
  env/                        # .env templates
.github/workflows/            # CI/CD pipelines
```

Each bounded-context module repeats the Clean Arch layers: `domain/` (entities, VOs, events, rules — zero external deps), `app/` (use cases + port interfaces), `infra/` (pgx/sqlc repos, adapters), `api/` (Gin handlers, DTOs, validation).

---

## 5. Core interfaces (design contracts)

```go
// charge/app — repository persists the whole aggregate (charge + payments + refunds + events) atomically
type ChargeRepository interface {
    Create(ctx context.Context, c *domain.Charge) error            // tx A (CREATED)
    Save(ctx context.Context, c *domain.Charge) error              // optimistic lock on version; persists aggregate changes + outbox in one tx
    FindByTxID(ctx context.Context, tenantID, txid string) (*domain.Charge, error)
    FindByID(ctx context.Context, tenantID, id string) (*domain.Charge, error)
    List(ctx context.Context, tenantID string, f ChargeFilter) ([]*domain.Charge, error)
}

// platform/outbox — events written in the same tx as aggregate changes
type OutboxStore interface {
    Enqueue(ctx context.Context, tx pgx.Tx, e OutboxEvent) error
    FetchUnsent(ctx context.Context, limit int) ([]OutboxEvent, error)
    MarkSent(ctx context.Context, ids []string) error
}

// platform/idempotency — required on POST /charges and /refunds
type IdempotencyStore interface {
    Reserve(ctx context.Context, tenantID, key, fingerprint string) (Reservation, error) // new | replay | conflict | in-flight
    SaveResult(ctx context.Context, tenantID, key string, status int, body []byte) error
}

// platform/secrets — keyed by payment_provider_id or subscription id; never the DB
type SecretProvider interface {
    ProviderCredentials(ctx context.Context, paymentProviderID string) (*ProviderCreds, error) // clientID/secret/cert
    SigningSecret(ctx context.Context, subscriptionID string) ([]byte, error)
}

// platform/tenant
type TenantResolver interface {
    Resolve(ctx context.Context) (*Tenant, error)                  // from API key
}
type ProviderResolver interface {
    Resolve(ctx context.Context, t *Tenant, explicitID string) (*PaymentProvider, error) // explicit or default
}

// notification — customer channels; forwarding is a separate consumer, not a Notifier
type Notifier interface {
    Channel() string                                               // email | sms | whatsapp
    Send(ctx context.Context, n *domain.Notification) error
}
```

EFí models never cross out of `provider/efi/`. Consumers are **idempotent** (dedup on `(event_id, channel)` for notifications, `(event_id, subscription_id)` for forwarding).

---

## 6. Data model

Core tables carry `tenant_id` (FK → `tenants.id`) with a Postgres **RLS policy** keyed off session-local `app.tenant_id`. `version int NOT NULL DEFAULT 0` for optimistic locking on the Charge aggregate root. `deleted_at` soft delete where appropriate. `created_at`/`updated_at` everywhere.

### ERD

```mermaid
erDiagram
    tenants ||--o{ payment_providers : has
    tenants ||--o{ webhook_subscriptions : has
    tenants ||--o{ unmatched_payments : has
    tenants ||--o{ notifications : has
    tenants ||--o{ audit_logs : has
    payment_providers ||--o{ pix_keys : registers
    payment_providers ||--o{ charges : issues
    charges ||--o{ payments : settled_by
    charges ||--o{ payment_events : records
    payments ||--o{ refunds : refunded_by
    webhook_subscriptions ||--o{ webhook_delivery_logs : delivers
    notifications ||--o{ notification_logs : attempts

    tenants { uuid id PK; text name; text status; timestamptz deleted_at }
    payment_providers { uuid id PK; uuid tenant_id FK; text provider; text account_label; text status; bool is_default; jsonb webhook_config }
    pix_keys { uuid id PK; uuid tenant_id FK; uuid payment_provider_id FK; text key; text key_type }
    charges { uuid id PK; uuid tenant_id FK; uuid payment_provider_id FK; text txid; text kind; text status; numeric amount; int expiration_seconds; date due_date; int validity_after_days; numeric fine_percent; text interest_mode; numeric interest_percent; text discount_mode; numeric discount_value; numeric abatement_value; text pix_key; text location_id; text payer_doc; text payer_doc_type; text payer_name; text payer_email; text payer_phone; text external_reference; int version; timestamptz deleted_at }
    payments { uuid id PK; uuid tenant_id FK; uuid charge_id FK; text e2e_id; numeric amount; timestamptz paid_at }
    refunds { uuid id PK; uuid tenant_id FK; uuid payment_id FK; text devolucao_id; text status; numeric amount; timestamptz completed_at }
    payment_events { uuid id PK; uuid tenant_id FK; uuid charge_id FK; text event_type; jsonb payload; timestamptz occurred_at }
    unmatched_payments { uuid id PK; uuid tenant_id FK; text e2e_id; text txid; numeric amount; jsonb raw; timestamptz received_at; timestamptz resolved_at }
    webhook_logs { uuid id PK; uuid tenant_id FK; text request_hash; jsonb raw_payload; text signature; timestamptz received_at; timestamptz processed_at }
    webhook_subscriptions { uuid id PK; uuid tenant_id FK; text target_url; text[] event_types; text status; int version }
    webhook_delivery_logs { uuid id PK; uuid tenant_id FK; uuid subscription_id FK; text event_id; int http_status; text response_snippet; int attempt; text outcome; timestamptz attempted_at }
    notifications { uuid id PK; uuid tenant_id FK; uuid charge_id FK; text channel; text event_type; text recipient; text status; timestamptz deleted_at }
    notification_logs { uuid id PK; uuid tenant_id FK; uuid notification_id FK; text attempt_status; text error; timestamptz attempted_at }
    audit_logs { uuid id PK; uuid tenant_id FK; text actor; text action; jsonb before; jsonb after; timestamptz at }
```

Plus two **platform tables** (tenant-scoped but not domain entities): `outbox` (event_id, aggregate_id, type, payload, sent_at) and `idempotency_keys` (tenant_id, key, fingerprint, txid/resource_id, status, response_snapshot, created_at).

### Indexing / constraints (highlights)

- `charges`: unique `(tenant_id, txid)`; index `(tenant_id, status)`, `(tenant_id, due_date) WHERE kind='cobv'`; optional unique `(tenant_id, external_reference)`.
- `charges` CHECK: `kind='cob'` ⇒ due_date/fine/interest/discount NULL; `kind='cobv'` ⇒ expiration_seconds NULL.
- `payments`: **unique `(tenant_id, e2e_id)`** — incoming-payment dedup authority; index `(charge_id)`.
- `refunds`: unique `(tenant_id, devolucao_id)`; aggregate invariant `Σ amount ≤ payment.amount` enforced by the Charge.
- `payment_events`: append-only (no UPDATE/DELETE), index `(charge_id, occurred_at)`.
- `webhook_logs`: index `(tenant_id, request_hash)` (audit; not the dedup authority).
- `idempotency_keys`: unique `(tenant_id, key)`.
- `outbox`: index on `sent_at IS NULL`.
- FKs `ON DELETE RESTRICT` (soft delete instead of cascade). RLS policies on all `tenant_id` tables.

---

## 7. Key flows

### 7.1 Create charge (Cob or CobV) — client-defined txid, persist-first ([ADR-0002](../../adr/0002-client-defined-txid-persist-first.md))

```mermaid
sequenceDiagram
    participant C as Client app
    participant API as Gin handler
    participant UC as CreateCharge use case
    participant P as PixProvider (EfiProvider)
    participant DB as Postgres

    C->>API: POST /api/v1/charges (Idempotency-Key, API key)
    API->>API: authn + resolve tenant + provider + validate DTO
    API->>UC: command
    UC->>UC: idempotency.Reserve(tenant,key,fingerprint) -> new | replay | conflict
    UC->>UC: mint client-defined txid
    UC->>DB: tx A: insert Charge=CREATED (+payment_event created)
    UC->>P: PUT /v2/cob(/v2/cobv)/:txid  (idempotent by txid)
    alt provider ok
        P-->>UC: Charge (QR, payload, ATIVA)
        UC->>DB: tx B: status=ACTIVE, store QR+payload, outbox ChargeCreated, save idempotency result
        API-->>C: 201 {txid, chargeId, qrCode, pixPayload, status:ACTIVE}
    else provider fails
        UC->>DB: tx B: status=FAILED (+payment_event failed)
        API-->>C: 502 {error}; charge recorded as FAILED
    end
```

Stuck `CREATED` (tx B never ran) is resolved by the scheduler's cleanup pass.

### 7.2 Webhook ingestion — batch, per-e2eId dedup (authoritative payment source)

```mermaid
sequenceDiagram
    participant E as EFí
    participant WH as Webhook handler
    participant DB as Postgres
    participant MQ as RabbitMQ

    E->>WH: POST (proxy-terminated mTLS)  body { pix: [ {endToEndId, txid, valor, horario, devolucoes?}, ... ] }
    WH->>WH: validate hmac query + source IP 34.193.116.226 (ADR-0004)
    WH->>DB: insert webhook_logs(request_hash, raw) -- audit only
    loop each pix item (classify)
        alt item has devolucoes[]  (refund notification)
            WH->>DB: tx: update refund(devolucao_id) -> COMPLETED/FAILED, outbox RefundCompleted; if Σ==paid -> charge REFUNDED
        else has tipo+status (sent pix)
            WH->>DB: ignore / log (not our concern)
        else received pix
            WH->>DB: tx: insert payment(endToEndId) [UNIQUE(tenant,e2e_id)]
            alt duplicate
                DB-->>WH: conflict -> skip
            else matched by txid
                WH->>DB: update charge->PAID, payment_events, outbox ChargePaid
            else no/unknown txid
                WH->>DB: insert unmatched_payments + metric
            end
        end
    end
    WH-->>E: 200 within 60s (durably accepted)
    DB-->>MQ: relay publishes events -> notification + forwarding consumers
```

**Item classification (EFí):** a `pix[]` item with a `devolucoes[]` array is a **refund** notification (`status` ∈ `DEVOLVIDO`|`NAO_REALIZADO`, `motivo` on failure); with `tipo`+`status` it is a sent pix (ignored); otherwise a received payment (no status). Dedup authority is `UNIQUE(tenant_id, e2e_id)` on payments, not the request hash. EFí's 60s callback timeout ⇒ ack fast after durable store. Polling is **not** primary; reconciliation is a backstop.

### 7.3 Refund (devolução) — two-phase, async completion

`POST /api/v1/refunds` → validate `Σ ≤ paid` → persist Refund=REQUESTED (+event) → mint devolução id → `PUT /v2/pix/:e2eId/devolucao/:id` → PROCESSING + outbox `RefundRequested` → **completion arrives over the same webhook** (a `pix` item with `devolucoes[]`, `status` `DEVOLVIDO`|`NAO_REALIZADO`; reconciliation `GetRefund` is the backstop) → COMPLETED/FAILED + outbox `RefundCompleted`; if `Σ refunds == paid` → Charge → REFUNDED. (Verified: EFí pushes devolução status over webhook — §16.)

### 7.4 Scheduler (time-derived)

Periodic job over active charges: emit `ChargeDueSoon` / `ChargeOverdue` (once per cadence), perform `ACTIVE→EXPIRED` transition (+`ChargeExpired`), clean up stuck `CREATED`, and reconcile (see §15.3). All events go through the outbox.

---

## 8. Cross-cutting concerns (built in Phase 0, woven throughout)

| Concern | Approach |
| --- | --- |
| Idempotency | **Required** `Idempotency-Key` on POST /charges, /refunds; scope `(tenant,key)`; fingerprint match → replay / 422 mismatch / 409 in-flight; bound 1:1:1 to txid; 24–48h retention (Redis fast-path + DB unique authority) |
| Reliable events | Transactional **Outbox** (event in same tx as state change) → relay → RabbitMQ topic exchange; **at-least-once** |
| Idempotent consumers | dedup on `(event_id, channel)` / `(event_id, subscription_id)` |
| Retry | Exponential backoff + jitter on EFí calls, notifications, queue ops |
| Circuit breaker | Around EFí provider calls; open on sustained failures |
| Dead letter queue | Poison events after max retries → DLQ; admin reprocessing (§15.4) |
| Logging | Structured JSON; correlation + request IDs via context; **PII masked** |
| Tracing | OpenTelemetry spans API → use case → provider → DB |
| Metrics | Prometheus: `charges_created_total`, `charges_paid_total`, `webhook_failures_total`, `notification_failures_total`, `forwarding_failures_total`, latency histograms |
| Health | `/health` (overall), `/ready` (deps), `/live` (process) |
| Domain events | `ChargeCreated, ChargePaid, ChargeExpired, ChargeDueSoon, ChargeOverdue, RefundRequested, RefundCompleted, NotificationSent, NotificationFailed` |

---

## 9. Security

- TLS everywhere. **Inbound EFí webhook mTLS is terminated at the ingress proxy** (nginx, EFí `efipay/mtls-webhook` pattern); the app registers with `x-skip-mtls-checking: true` and additionally validates the **hmac query secret** + **source IP `34.193.116.226`** on every callback (TLS ≥ 1.2) — [ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md).
- **Outbound to EFí** uses the SDK's per-account client cert (PEM, converted from P12).
- Secrets from env / Vault / AWS Secrets Manager via `SecretProvider`, keyed by `payment_provider_id` (creds/cert/webhook-hmac) and `subscription_id` (forwarding signing secret). **Never in the DB, never hardcoded.** Rotation via TTL / re-fetch.
- **PII / LGPD:** CPF/CNPJ masked in all logs; minimized in forwarded events; access recorded in `audit_logs`. Column-level encryption of `payer_doc` is a Phase 6 decision (disk encryption + RLS in the interim).
- **RBAC** for internal/admin ops; **API-key authentication** for client apps; per-(tenant,API-key) **rate limiting** (Redis token bucket).
- IP allow lists where appropriate (EFí webhook source, admin).
- Postgres **RLS** enforces tenant isolation at the DB layer (defense-in-depth atop repo filtering) — [ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md).
- Outbound forwarding signed **HMAC-SHA256** with per-subscription secret.
- Follow OWASP (input validation, output encoding, least privilege, dependency scanning in CI).

---

## 10. API surface (v1)

```Text
POST   /api/v1/charges                 # create Cob or CobV (Idempotency-Key REQUIRED; optional provider id)
GET    /api/v1/charges/{id}
GET    /api/v1/charges                 # list + filter (status, kind, due_date, overdue, paging)
POST   /api/v1/refunds                 # Idempotency-Key REQUIRED
GET    /api/v1/refunds/{id}
POST   /api/v1/webhooks/register       # register/configure a WebhookSubscription (forwarding)
POST   /api/v1/webhooks/efi            # EFí inbound (mTLS + signature) — not public-listed
GET    /api/v1/reports/revenue
GET    /api/v1/reports/overdue
GET    /api/v1/reports/payments
GET    /health  /ready  /live
GET    /metrics                        # Prometheus
```

Versioned prefix `/api/v1`. Full contract in `api/openapi.yaml` (grows per phase, complete Phase 6).

---

## 11. Phased roadmap (task-level, with exit criteria)

> Adds **Phase 0** foundation and weaves cross-cutting concerns from the start, then the prompt's functional phases.

### Phase 0 — Foundation

- Init Go module, folder skeleton, `cmd/server/main.go` composition root.
- Config loader (env + SecretProvider abstraction).
- pgx pool + tx helpers + **RLS session setter** (`SET LOCAL app.tenant_id`); goose migrations; sqlc.
- Base structured logging (PII masking), OTel tracing, Prometheus metrics, `/health /ready /live`.
- Platform primitives: idempotency store, transactional outbox + relay, events producer/consumer helpers, retry, circuit breaker, RabbitMQ publisher/consumer + DLQ.
- HTTP middleware: auth scaffold, request ID, recovery, rate limit.
- Dockerfile + docker-compose (app, Postgres, Redis, RabbitMQ, **nginx mTLS webhook proxy**); CI (lint, test, security scan, build, publish image).
- **Exit:** service boots; `/health` green with deps up; CI green; modules compile; outbox relay round-trips a test event; RLS denies cross-tenant reads in a test.

### Phase 1 — Tenants + provider accounts + EFí auth + immediate charge

- Tenant + `payment_providers` + `pix_keys` models; `TenantResolver` + `ProviderResolver` middleware.
- `SecretProvider` impls (env first; Vault/AWS SM interface ready), keyed by `payment_provider_id`.
- `PixProvider` port + `EfiProvider`: **per-account SDK client pool** (cert PEM, P12→PEM ingest; SDK manages OAuth), credential validation.
- Charge aggregate (Cob): client-defined txid, **persist-first two-phase** create, QR + payload, status CREATED→ACTIVE/FAILED, Payer VO, payment_events.
- `POST /api/v1/charges` (immediate), `GET /api/v1/charges/{id}`, required Idempotency-Key.
- **Exit:** create + fetch immediate charge end-to-end against EFí homologation; failed provider call leaves FAILED + audit; ≥80% coverage on charge domain/app; SDK capability review documented.

### Phase 2 — Due-date charges (CobV)

- CobV via typed columns; `DueDateTerms` VO + rules for multa/juros/desconto/abatimento.
- Extend `POST /api/v1/charges` for due-date + rules; CHECK constraints.
- **Exit:** CobV created with fine/interest/discount applied; rule calculations unit-tested across edge cases.

### Phase 3 — Webhooks + refunds + reconciliation

- Secure ingestion: mTLS + signature, **batch `pix[]`**, request-level audit (`webhook_logs`), per-e2eId dedup (`UNIQUE(tenant,e2e_id)`), unmatched_payments.
- Payment lifecycle state machine on the Charge; record payments + payment_events; emit ChargePaid/ChargeExpired.
- Refund flow (`POST /api/v1/refunds`): two-phase, partial+multiple, `Σ≤paid`, async completion; emit RefundRequested/RefundCompleted; derived REFUNDED.
- Scheduler: DueSoon/Overdue/Expired + stuck-CREATED cleanup + reconciliation backstop (§15.3).
- **Exit:** batched webhook marks charges PAID idempotently; replay rejected; unmatched recorded; partial refund processes and completes via webhook-or-recon; reconciliation recovers a simulated missed event. **Verify EFí delivers devolução status over webhook (open item).**

### Phase 4 — Notifications + forwarding

- Customer notifiers (email/sms/whatsapp) — platform-global providers, per-tenant sender identity (§15.1).
- Webhook **forwarding**: `webhook_subscriptions`, HMAC-signed egress consumer, `webhook_delivery_logs`, retry → DLQ.
- Event-driven via outbox consumers on ChargeCreated/DueSoon/Overdue/Paid/Refunded; idempotent.
- **Exit:** paid event → customer notification sent + logged AND forwarded to a subscriber with valid HMAC; induced failures land in DLQ and retry per policy.

### Phase 5 — Reporting

- Financial (daily/monthly/yearly revenue), collection (overdue, unpaid, payment aging), operational (webhook failures, notification/forwarding failures, processing times).
- Export CSV / XLSX / JSON; report endpoints; tenant-scoped.
- **Exit:** revenue + overdue + payments reports export correctly in all three formats with tenant scoping.

### Phase 6 — Production hardening

- RBAC, rate limiting + keying (§15.4), IP allow lists, secret rotation, `payer_doc` encryption decision.
- Load tests; complete `api/openapi.yaml`; contract + webhook + integration suites.
- Security/OWASP review + risk assessment; K8s manifests + Helm chart (incl. **nginx mTLS ingress** for webhooks); Grafana dashboards.
- **Exit:** load-test target met; security review signed off; Helm deploy to a cluster succeeds; coverage ≥80% overall.

---

## 12. Testing strategy

- **Unit:** domain rules (fine/interest/discount), Charge state machine, mappers, refund invariant. No I/O.
- **Integration:** repos against ephemeral Postgres (testcontainers) with RLS on, outbox relay, RabbitMQ consumers.
- **Contract:** EFí adapter against recorded fixtures / homologation; provider port conformance.
- **Webhook:** batch `pix[]`, signature, replay, per-e2eId dedup, unmatched, malformed payloads.
- **Load:** charge creation + webhook ingestion throughput.
- **Target:** ≥80% coverage overall, enforced in CI.

---

## 13. Deliverables mapping (from prompt)

Architecture doc (this spec) · glossary ([CONTEXT.md](../../../CONTEXT.md)) · decisions ([ADR-0001](../../adr/0001-charge-lifecycle-status-model.md)/[0002](../../adr/0002-client-defined-txid-persist-first.md)/[0003](../../adr/0003-multi-tenant-shared-db-provider-account.md)) · component/sequence diagrams (§4, §7) · DB ERD (§6) · OpenAPI (`api/openapi.yaml`, complete Phase 6) · folder structure (§4) · infra + Docker + K8s/Helm (§4 `deploy/`, Phases 0 & 6) · security review + risk assessment (Phase 6) · implementation roadmap (§11).

---

## 14. Risks & open items

Resolved during the grilling/research session (see §16 + ADRs):

- ~~Devolução status delivery~~ → **webhook** (`devolucoes[]`), poll backstop.
- ~~Webhook signature scheme~~ → **proxy mTLS + hmac + IP** ([ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md)); no JSON signature.
- ~~mTLS cert per provider~~ → **per-account SDK client pool**, PEM (P12→PEM).
- ~~Idempotency-Key ownership~~ → required on mutations, documented contract (§8).

Remaining:

- **EFí SDK edge gaps:** confirm exact method names + error semantics against the SDK during Phase 1; fall back to EFí REST behind the adapter if a feature is missing.
- **PII encryption:** `payer_doc` at-rest encryption deferred to a Phase 6 decision.
- **Homologation validation:** exercise cob/cobv/devolução/webhook end-to-end against EFí homologation in Phases 1 & 3 (standard verification, not a design gap).

---

## 15. Resolved deferred branches

### 15.1 Notification provider model

Provider **integrations are platform-global** (one SMTP / SMS gateway / WhatsApp BSP configured at platform level, creds in SecretProvider). **Per-tenant** config covers sender identity (from-name, from-email, reply-to, WhatsApp sender) and per-channel enable/disable. Avoids per-tenant gateway onboarding in v1 while allowing branding. Revisit per-tenant gateways only if a product needs its own sender infrastructure.

### 15.2 RLS mechanics

Single pgx pool. Every request handler opens a transaction and runs `SET LOCAL app.tenant_id = $1` before any query; repositories operate only within such a tx. RLS policies use `USING (tenant_id = current_setting('app.tenant_id')::uuid)`. Forgetting to set it is **fail-closed** (no rows). A privileged migration/admin role bypasses RLS for maintenance and the scheduler runs per-tenant or with an elevated role scoped by explicit `tenant_id` filters.

### 15.3 Reconciliation

Scheduler job (default every 5 min) selects charges `ACTIVE` with stale `last_reconciled_at` and refunds in `PROCESSING`, calls `GetCharge` / `GetRefund`, reconciles provider vs local status, and emits events on drift. A daily sweep covers older actives. Bounded by `last_reconciled_at` to limit provider calls. Backstop only — webhooks remain authoritative.

### 15.4 Rate limiting & DLQ ops

Rate limit = Redis token bucket keyed primarily by API key, tenant as fallback; a stricter bucket guards charge creation; the EFí webhook endpoint is exempt from client rate limiting but IP-allowlisted. DLQ: messages exceeding max redelivery land in a per-consumer DLQ; an admin endpoint + CLI replays them after a fix; poison messages are inspectable with original headers/correlation IDs.

---

## 16. EFí integration specifics (verified against docs + SDK)

Facts established by reviewing the official EFí Go SDK and Pix API docs; these constrain the adapter and webhook design.

**SDK (`efipay/sdk-go-apis-efi`)**

- Covers: immediate cob, due cobv, charge detail, **devolução**, webhook config + list, QR code / copia-e-cola, EVP key creation.
- Certificate is loaded as **PEM** via config fields `CA` (public cert) + `Key` (private key), plus `client_id`, `client_secret`, `sandbox`, `timeout`. **P12 must be converted to PEM** before use.
- Certificate + credentials bind **per client instance** (not per call) ⇒ keep **one cached SDK client per `payment_provider_id`** (a client pool).
- The SDK **manages the OAuth token internally**; our own token caching is unnecessary for the SDK path (Redis token cache only if we ever bypass the SDK).

**Webhook ingress** ([ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md))

- Register per Pix key: `PUT /v2/webhook/:chave` with `{ "webhookUrl": "https://…/webhook?hmac=<secret>&ignorar=" }`. The `ignorar=` suffix prevents EFí appending `/pix` to the route.
- Security: **mTLS** (BACEN-mandated, terminated at proxy) + `x-skip-mtls-checking: true` + **hmac** query secret + **IP allowlist `34.193.116.226`**. No signature field in the JSON body. TLS ≥ 1.2. **60s** callback timeout.
- `payment_providers.webhook_config` stores the registered keys + a reference to the hmac secret (secret itself in SecretProvider).

**Webhook payload (`{ "pix": [ … ] }`)**

- Received payment item: `endToEndId`, `txid`, `chave`, `valor`, `horario`, `infoPagador`, optional `gnExtras`. No `status`.
- Refund item: same base + `devolucoes[]` with `id`, `rtrId`, `valor`, `natureza`, `horario.solicitacao`, `status` ∈ `DEVOLVIDO` | `NAO_REALIZADO` (+ `motivo` when failed).
- Sent pix item: carries `tipo` + `status` (not relevant to this platform — ignored).

**Sources:** [SDK repo](https://github.com/efipay/sdk-go-apis-efi) · [Pix webhooks doc](https://dev.efipay.com.br/en/docs/api-pix/webhooks/) · [efipay/mtls-webhook (nginx)](https://github.com/efipay/mtls-webhook)
