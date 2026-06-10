# anatomy.md

> Auto-maintained by OpenWolf. Last scanned: 2026-06-10T13:39:21.639Z
> Files: 25 tracked | Anatomy hits: 0 | Misses: 0

## ./

- `.gitignore` — Git ignore rules (~13 tok)
- `.golangci.yml` (~41 tok)
- `CLAUDE.md` — OpenWolf (~57 tok)
- `CONTEXT.md` — Context — Pix Payment Platform (~1024 tok)
- `go.mod` — Go module definition (~11 tok)
- `Makefile` (~140 tok)
- `sqlc.yaml` (~74 tok)

## .claude/

- `settings.json` (~441 tok)

## .claude/rules/

- `openwolf.md` (~313 tok)

## docs/


## docs/adr/

- `0001-charge-lifecycle-status-model.md` — 1. Charge lifecycle status model (~984 tok)
- `0001-charge-lifecycle-status-model.md` — 8-status enum, derived EXPIRED/REFUNDED, OVERDUE/DueSoon predicates + scheduler (~700 tok)
- `0002-client-defined-txid-persist-first.md` — 2. Client-defined txid and persist-first charge creation (~608 tok)
- `0002-client-defined-txid-persist-first.md` — mint txid, persist CREATED before EFi call, two-phase (~600 tok)
- `0003-multi-tenant-shared-db-provider-account.md` — 3. Multi-tenancy via shared DB + RLS, with account identity on payment_providers (~816 tok)
- `0003-multi-tenant-shared-db-provider-account.md` — shared-DB+RLS, account identity on payment_providers, secrets keyed by provider_id (~700 tok)
- `0004-webhook-ingress-mtls-termination.md` — 4. Inbound webhook mTLS terminated at the proxy, with app-level hmac + IP allowlist (~754 tok)

## docs/superpowers/plans/

- `2026-06-10-phase1-00-overview.md` — Phase 1 Implementation Plan — Overview & Conventions (~2695 tok)
- `2026-06-10-phase1-01-foundation.md` — Phase 1 · File 01 — Foundation (~6555 tok)
- `2026-06-10-phase1-02-tenant-provider.md` — Phase 1 · File 02 — Tenant, Provider Accounts, Resolution & API-Key Auth (~6317 tok)
- `2026-06-10-phase1-03-secrets-efi-provider.md` — Phase 1 · File 03 — Secrets, PixProvider Port & EFí Adapter (~5769 tok)
- `2026-06-10-phase1-04-charge-aggregate.md` — Phase 1 · File 04 — Charge Aggregate: Domain, Schema, Repository, Idempotency (~7182 tok)
- `2026-06-10-phase1-05-create-charge-api.md` — Phase 1 · File 05 — Create-Charge Use Case, API, Wiring & End-to-End (~6234 tok)

## docs/superpowers/specs/

- `2026-06-09-pix-payment-platform-design.md` — Design Spec — Enterprise Pix Payment Platform (EFí) (~8261 tok)

## root (added)

- `0004-webhook-ingress-mtls-termination.md` — proxy-terminated mTLS + app hmac + IP allowlist 34.193.116.226 (~600 tok)
- `CONTEXT.md` — ubiquitous-language glossary for the Pix domain (~900 tok)
