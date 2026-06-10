# 3. Multi-tenancy via shared DB + RLS, with account identity on payment_providers

Date: 2026-06-09
Status: Accepted

## Context

The platform is the centralized payment infrastructure for all company products and must serve multiple independent EFí accounts. Two coupled decisions are needed: how to isolate tenant data, and where account identity (credentials, certificate, Pix keys, webhook config) lives.

The source requirements stated "each tenant must have: Client ID, Client Secret, Certificate, Pix Keys, Webhook configuration" — implying credentials sit 1:1 on the tenant. But a Pix key and certificate belong to a specific PSP account, and the stated goal of provider-swappability and serving many products argues for a tenant being able to hold more than one account.

Isolation options considered: shared DB with `tenant_id` row-scoping, schema-per-tenant, database-per-tenant.

## Decision

**Isolation — shared database, `tenant_id` row-scoping + Postgres Row-Level Security.**
Every business table carries `tenant_id`. RLS policies restrict rows to the current tenant: each request runs inside a transaction that sets a session-local `app.tenant_id`, and policies use `USING (tenant_id = current_setting('app.tenant_id')::uuid)`. The application repository layer also filters by tenant; RLS is defense-in-depth, not the only guard. A privileged migration/admin role bypasses RLS for maintenance.

**Account identity — on `payment_providers`, not `tenants`.**
A tenant has one or more `payment_providers` rows, each representing one EFí (or future provider) account: `(id, tenant_id, provider, account_label, status, is_default, webhook_config)`. Consequences:

- Secrets (clientID/secret/certificate) are **not** stored in the database. They are resolved from the `SecretProvider` (env/Vault/AWS Secrets Manager) keyed by `payment_provider_id`.
- `pix_keys` reference `payment_provider_id` — a key belongs to the account that registered it.
- `charges.payment_provider_id` records which account a charge was created under (required for correct reconciliation and reporting).
- Request resolution is two steps: `TenantResolver` resolves the tenant (from API key); the provider is then either explicit in the request or the tenant's `is_default` provider.

## Consequences

- Lowest operational cost: one migration set, one connection pool, stateless service — fits horizontal scaling.
- Tenant isolation is enforced at two layers (repo filter + RLS). A bug in one is backstopped by the other.
- Supports multiple accounts per tenant and future non-EFí providers without schema change — only a new adapter and provider rows.
- Diverges from the literal requirement (creds 1:1 on tenant). This is intentional; the requirement's "11 tables" and "creds on tenant" were non-exhaustive guidance.
- A noisy tenant can affect shared resources; mitigated by per-tenant rate limiting. If a tenant later needs hard physical isolation (regulatory), they can be migrated to a dedicated database — the `tenant_id` model does not preclude it but does not provide it out of the box.
- RLS requires every data path to run within a tenant-scoped transaction; code that forgets to set `app.tenant_id` sees no rows (fail-closed), which is the desired failure mode.
