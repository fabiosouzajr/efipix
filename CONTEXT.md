# Context — Pix Payment Platform

Ubiquitous language for the Pix payment platform. Glossary only — no implementation details.

## Terms

### Charge

The **aggregate root** of the payment domain. A request for payment created against a Pix provider. Two kinds:

- **Immediate Charge (Cob)** — pay-now, has an expiration.
- **Due-Date Charge (CobV)** — has a due date and may carry fine, interest, discount, and abatement rules.

A Charge owns its Payments, Refunds, and PaymentEvents. Nothing inside the aggregate is loaded or mutated except through the Charge. One optimistic-lock version guards the whole aggregate.

### Payment

A settled Pix transfer against a Charge, identified by an **e2eId**. An entity inside the Charge aggregate, never standalone. A Charge may receive more than one Payment.

### Refund (Devolução)

A return of funds against a Payment. An entity inside the Charge aggregate. Partial and multiple refunds per Payment are allowed; the sum of a Payment's refunds may not exceed the Payment amount.

### PaymentEvent

An append-only record of a state change in the Charge aggregate (audit history).

### Unmatched payment

A Pix transfer received via webhook that cannot be tied to a Charge (no txid, or txid not found). Recorded separately for later reconciliation; never discarded.

### Tenant

An isolated customer of the platform. Every request executes in the context of exactly one Tenant. Data is isolated by `tenant_id` row-scoping plus Postgres RLS. See [ADR-0003](docs/adr/0003-multi-tenant-shared-db-provider-account.md).

### PaymentProvider

One external Pix account belonging to a Tenant (an EFí account today). Holds non-secret account config and webhook configuration; a Tenant may have several and designates a default. Credentials and certificate are **not** stored in the database — they live in the SecretProvider keyed by the PaymentProvider id. Account identity (Pix keys, certificate, credentials) hangs off the PaymentProvider, not the Tenant.

### PixProvider (port)

The abstraction the application talks to (`CreateImmediateCharge`, `CreateDueDateCharge`, `GetCharge`, `GenerateQRCode`, `Refund`). EFí is one implementation; provider-specific models never leave the adapter.

### PixKey

A Pix addressing key (registered with a PSP). Belongs to a PaymentProvider — the account that registered it.

### WebhookSubscription

A partner system's registration to receive forwarded platform events: target URL, the event types it wants, and a signing secret (in the SecretProvider). Drives outbound, HMAC-signed event forwarding. Distinct from customer notifications.

### Payer (Devedor)

The party that owes a Charge: document (CPF/CNPJ), name, and optional contact (email/phone). An embedded value object on the Charge — not a managed customer entity. Document is PII (LGPD): masked in logs, minimized in forwarded events.

### Charge status

Platform-internal lifecycle state of a Charge: CREATED, ACTIVE, PENDING, PAID, EXPIRED, CANCELLED, REFUNDED, FAILED. Provider-agnostic; EFí statuses map onto it. EXPIRED and REFUNDED are **platform-derived** (not reported by EFí). See [ADR-0001](docs/adr/0001-charge-lifecycle-status-model.md).

### Refund status

Lifecycle of a Refund, tracked separately from Charge status: REQUESTED, PROCESSING, COMPLETED, FAILED (EFí: `EM_PROCESSAMENTO`, `DEVOLVIDO`, `NAO_REALIZADO`). A Charge becomes REFUNDED only when refunded total equals paid total.

### Overdue / Due soon

**Derived predicates** over CobV charges, not stored statuses. Overdue = past due date, still ACTIVE and payable (fine/interest apply). Due soon = within N days of due date. Both are emitted as events by the scheduler. See [ADR-0001](docs/adr/0001-charge-lifecycle-status-model.md).

### txid

The transaction identifier of a Charge at the Pix provider. **Client-defined** (we mint it) so provider calls are idempotent. See [ADR-0002](docs/adr/0002-client-defined-txid-persist-first.md).

### e2eId

End-to-end identifier of a settled Pix Payment, assigned by the Pix arrangement. The deduplication authority for incoming payments.
