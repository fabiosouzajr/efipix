# anatomy.md

> Auto-maintained by OpenWolf. Last scanned: 2026-06-12T15:52:39.700Z
> Files: 102 tracked | Anatomy hits: 0 | Misses: 0

## ../../../../tmp/

- `architecture-review-20260610-113434.html` — Architecture review — efipix (Pix payment platform) (~7119 tok)

## ./

- `.gitignore` — Git ignore rules (~13 tok)
- `.golangci.yml` (~41 tok)
- `CLAUDE.md` — OpenWolf (~57 tok)
- `CONTEXT.md` — Context — Pix Payment Platform (~1024 tok)
- `go.mod` — Go module definition (~11 tok)
- `Makefile` (~158 tok)
- `sqlc.yaml` (~74 tok)

## .claude/

- `settings.json` (~441 tok)

## .claude/rules/

- `openwolf.md` (~313 tok)

## .github/workflows/

- `ci.yml` — CI: ci (~148 tok)

## cmd/server/

- `main.go` (~583 tok)

## db/migrations/

- `00001_bootstrap.sql` — +goose Up (~108 tok)
- `00002_tenants.sql` — +goose Up (~870 tok)
- `00003_charges.sql` — +goose Up (~1284 tok)

## db/seed/

- `dev.sql` (~218 tok)

## deploy/compose/

- `docker-compose.yml` — Docker Compose services (~264 tok)
- `nginx-webhook.conf` (~116 tok)

## deploy/compose/initdb/

- `00-roles.sql` — Owner/migrator role is the compose superuser "pix" (POSTGRES_USER). (~112 tok)

## deploy/docker/

- `Dockerfile` — Docker container definition (~71 tok)

## docs/

- `efi-sdk-review.md` — EFí Go SDK Capability Review (Phase 1) (~967 tok)

## docs/adr/

- `0001-charge-lifecycle-status-model.md` — 1. Charge lifecycle status model (~984 tok)
- `0001-charge-lifecycle-status-model.md` — 8-status enum, derived EXPIRED/REFUNDED, OVERDUE/DueSoon predicates + scheduler (~700 tok)
- `0002-client-defined-txid-persist-first.md` — 2. Client-defined txid and persist-first charge creation (~608 tok)
- `0002-client-defined-txid-persist-first.md` — mint txid, persist CREATED before EFi call, two-phase (~600 tok)
- `0003-multi-tenant-shared-db-provider-account.md` — 3. Multi-tenancy via shared DB + RLS, with account identity on payment_providers (~816 tok)
- `0003-multi-tenant-shared-db-provider-account.md` — shared-DB+RLS, account identity on payment_providers, secrets keyed by provider_id (~700 tok)
- `0004-webhook-ingress-mtls-termination.md` — 4. Inbound webhook mTLS terminated at the proxy, with app-level hmac + IP allowlist (~754 tok)
- `0005-cobv-due-date-rule-schema.md` — 5. CobV due-date rule schema: charge_discounts table and generic fine/interest value columns (~619 tok)
- `0006-post-charges-cobv-only.md` — 6. POST /api/v1/charges issues CobV only (Cob stays dormant in the domain) (~456 tok)

## docs/superpowers/plans/

- `2026-06-10-phase1-00-overview.md` — Phase 1 Implementation Plan — Overview & Conventions (~2706 tok)
- `2026-06-10-phase1-01-foundation.md` — Phase 1 · File 01 — Foundation (~6555 tok)
- `2026-06-10-phase1-02-tenant-provider.md` — Phase 1 · File 02 — Tenant, Provider Accounts, Resolution & API-Key Auth (~6383 tok)
- `2026-06-10-phase1-03-secrets-efi-provider.md` — Phase 1 · File 03 — Secrets, PixProvider Port & EFí Adapter (~5769 tok)
- `2026-06-10-phase1-04-charge-aggregate.md` — Phase 1 · File 04 — Charge Aggregate: Domain, Schema, Repository, Idempotency (~8987 tok)
- `2026-06-10-phase1-05-create-charge-api.md` — Phase 1 · File 05 — Create-Charge Use Case, API, Wiring & End-to-End (~6064 tok)
- `2026-06-12-phase2-00-overview.md` — Phase 2 Implementation Plan — Overview & Conventions (~3720 tok)
- `2026-06-12-phase2-01-domain-rules.md` — Phase 2 · File 01 — Domain Rules: `brdate`, `DueDateTerms`, `EffectiveAmount`, `NewDueDate` (~5975 tok)
- `2026-06-12-phase2-02-schema-repository.md` — Phase 2 · File 02 — Schema Migration & Repository Persistence (~3803 tok)
- `2026-06-12-phase2-03-provider-sdk.md` — Phase 2 · File 03 — Provider Port & EFí CobV Adapter (~4086 tok)
- `2026-06-12-phase2-04-usecase-api.md` — Phase 2 · File 04 — Use Case, CobV API, Wiring & End-to-End (~7218 tok)

## docs/superpowers/specs/

- `2026-06-09-pix-payment-platform-design.md` — Design Spec — Enterprise Pix Payment Platform (EFí) (~8453 tok)
- `2026-06-10-phase1-tenants-auth-immediate-charge-spec.md` — Phase 1 Spec — Tenants, EFí Auth & Immediate Charges (Cob) (~1676 tok)
- `2026-06-10-phase2-due-date-charges-spec.md` — Phase 2 Spec — Due-Date Charges (CobV) with Fine, Interest, Discount, Abatement (~2439 tok)
- `2026-06-10-phase3-webhooks-lifecycle-refunds-spec.md` — Phase 3 Spec — Webhooks, Payment Lifecycle, Refunds & Reconciliation (~1952 tok)
- `2026-06-10-phase4-notifications-forwarding-spec.md` — Phase 4 Spec — Notifications & Webhook Forwarding (~1433 tok)
- `2026-06-10-phase5-reporting-spec.md` — Phase 5 Spec — Reporting & Exports (~1127 tok)
- `2026-06-10-phase6-production-hardening-spec.md` — Phase 6 Spec — Production Hardening: Security, Observability, Resilience, Deploy (~1608 tok)

## docs/superpowers/specs/ (per-phase)


## internal/charge/api/

- `dto.go` — Struct: createChargeRequest (~287 tok)
- `e2e_test.go` — go:build integration (~1292 tok)
- `handler.go` — Struct: Handler (~764 tok)

## internal/charge/app/

- `create_test.go` — Struct: fakeRepo (~1008 tok)
- `create.go` — Struct: CreateImmediateChargeCmd (~705 tok)
- `repository.go` — Interface: ChargeRepository (~207 tok)

## internal/charge/domain/

- `charge_test.go` — TestNewTxidFormat, TestNewImmediateCreatesCreated, TestNewImmediateValidates (~287 tok)
- `charge.go` — Struct: Payer (~918 tok)
- `transitions_test.go` — TestMarkActiveFromCreated, TestMarkActiveIllegalFromFailed, TestMarkFailedFromCreated (~260 tok)

## internal/charge/infra/

- `repository_test.go` — go:build integration (~831 tok)
- `repository.go` — Struct: Repository (~1146 tok)

## internal/platform/config/

- `config_test.go` — TestLoadDefaultsAndRequired, TestLoadMissingRequired (~142 tok)
- `config.go` — Struct: Config (~217 tok)

## internal/platform/db/

- `db_test.go` — go:build integration (~510 tok)
- `db.go` — Struct: Pool (~513 tok)

## internal/platform/errors/

- `errors_test.go` — TestKindOf (~125 tok)
- `errors.go` — Struct: Error (~187 tok)

## internal/platform/health/

- `health_test.go` — TestEndpoints, TestReadyFailsWhenDepDown (~231 tok)
- `health.go` — Register (~179 tok)

## internal/platform/httpx/

- `errors_test.go` — TestStatusForKind (~204 tok)
- `errors.go` — StatusFor (~140 tok)

## internal/platform/idempotency/

- `idempotency_test.go` — go:build integration (~572 tok)
- `idempotency.go` — Interface: Store (~535 tok)
- `middleware_test.go` — Struct: fakeStore (~729 tok)
- `middleware.go` — Struct: bufferingWriter (~821 tok)

## internal/platform/logging/

- `logging_test.go` — TestMaskDoc, TestNewReturnsLogger (~124 tok)
- `logging.go` — New, MaskDoc (~192 tok)

## internal/platform/money/

- `money_test.go` — TestCentavosString, TestParseString (~147 tok)
- `money.go` — ParseString (~239 tok)

## internal/platform/secrets/

- `env_test.go` — TestEnvProviderCredentials (~273 tok)
- `env.go` — Struct: envEntry (~343 tok)
- `p12_test.go` — TestP12ToPEM (~301 tok)
- `p12.go` — P12ToPEM (~193 tok)
- `secrets.go` — Interface: SecretProvider (~79 tok)

## internal/platform/tenantctx/

- `tenantctx_test.go` — TestRoundTrip (~118 tok)
- `tenantctx.go` — Struct: Resolved (~96 tok)

## internal/provider/

- `provider.go` — Interface: PixProvider (~207 tok)

## internal/provider/efi/

- `client.go` — Interface: efiClient (~214 tok)
- `efi_test.go` — Struct: fakeClient (~630 tok)
- `efi.go` — Struct: EfiProvider (~613 tok)
- `sdkclient_homolog_test.go` — go:build homolog (~319 tok)
- `sdkclient.go` — Interface: efiSDKClient (~1067 tok)

## internal/tenant/api/

- `middleware_test.go` — Struct: fakeRepo (~558 tok)
- `middleware.go` — Middleware (~326 tok)

## internal/tenant/app/

- `apikey_test.go` — TestHashAPIKey (~79 tok)
- `apikey.go` — HashAPIKey (~67 tok)
- `repository.go` — Interface: Repository (~249 tok)
- `resolver_test.go` — Struct: fakeRepo (~501 tok)
- `resolver.go` — Struct: Resolver (~204 tok)

## internal/tenant/domain/

- `domain.go` — Struct: Tenant (~113 tok)

## internal/tenant/infra/

- `repository_test.go` — go:build integration (~639 tok)
- `repository.go` — Struct: Repository (~766 tok)

## root (added)

- `0004-webhook-ingress-mtls-termination.md` — proxy-terminated mTLS + app hmac + IP allowlist 34.193.116.226 (~600 tok)
- `CONTEXT.md` — ubiquitous-language glossary for the Pix domain (~900 tok)
