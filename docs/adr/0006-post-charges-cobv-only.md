# 6. POST /api/v1/charges issues CobV only (Cob stays dormant in the domain)

Date: 2026-06-12
Status: Accepted

## Context

Phase 1 built the Cob (immediate charge) path end-to-end: `domain.NewImmediate`, `MarkActive`/`MarkFailed`, and `POST /api/v1/charges` create a `kind=cob` charge. Phase 2 adds CobV (due-date charges) to the same aggregate and endpoint. The master design and Phase 2 spec implied both kinds would be selectable on the same endpoint (e.g. via a `kind` field), with a validation matrix (cob fields rejected for cobv and vice versa, `due_date` required only for cobv, etc.).

During Phase 2 planning we chose to skip that selection/validation matrix for now.

## Decision

`POST /api/v1/charges` now always creates a `kind=cobv` charge: `due_date` is required, `expiration_seconds` drops out of the request contract, and `fine`/`interest`/`discount`/`abatement` are optional rule blocks. `domain.NewImmediate` and its transitions remain in `internal/charge/domain` with their existing unit tests (`charge_test.go`, `transitions_test.go`), but are not reachable from the API. Phase 1's `internal/charge/api/e2e_test.go` (which exercised Cob via the API) is rewritten for CobV.

This is a **simplification for Phase 2 delivery, not a permanent product decision** — Cob may return behind an explicit `kind` selector in a later phase if a use case needs instant charges.

## Consequences

- Cob is effectively dormant: present in the domain, covered by domain-level unit tests, but untested at the API/integration level.
- Reintroducing Cob via the API later requires adding a `kind` field to `createChargeRequest` plus the validation matrix this ADR avoided — not a schema change, since `charges` and its CHECK constraints (per [ADR-0005](0005-cobv-due-date-rule-schema.md)) already support both kinds.
