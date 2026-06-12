# Cerebrum

> OpenWolf's learning memory. Updated automatically as the AI learns from interactions.
> Do not edit manually unless correcting an error.
> Last updated: 2026-06-09

## User Preferences

<!-- How the user likes things done. Code style, tools, patterns, communication. -->

- Implements via superpowers `executing-plans`: strict TDD per task (failing test → red → minimal impl → green → commit), Conventional Commits, one commit per task, co-author trailer `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- Prefers checkpoints between plan files (asked for "File 01 only, then checkpoint" rather than running all 5).
- Version control: `git init` + feature branch (`phase1-foundation`), not committing on the default branch.

## Key Learnings

- **Project:** efipix
- **Go version is 1.25, not the plan's 1.24.** Latest `testcontainers-go`/`gin` (and their deps) require go ≥1.25, so `go mod tidy` bumped the go.mod directive to `1.25.0`. Consequence: Dockerfile uses `golang:1.25-alpine` and CI `setup-go` uses `1.25` (both deviate from plan File 01's literal 1.24, which would fail the toolchain check).
- **`go mod tidy` strips requires when no .go source imports them.** After scaffolding (Task 1) go.mod had zero requires; each later task's `go test`/`go get` + `go mod tidy` re-adds only what its code imports. So commit go.mod/go.sum alongside the task that introduces a dependency.
- **golangci-lint must be built with go ≥ the module's targeted version.** A prebuilt release (v1.62.2, built with go1.23) refuses a go1.25 module ("language version used to build golangci-lint is lower than targeted"). Fix: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8` so it builds with the local toolchain.
- Integration tests are tagged `//go:build integration` and excluded from `go test ./...`; run with `-tags=integration` (needs Docker). `db` package thus shows "no test files" in the unit run — expected.
- `~/go/bin` (goose, golangci-lint) is not on PATH by default in this shell — prefix commands with `export PATH="$PATH:/home/fj/go/bin"`.

## Do-Not-Repeat

<!-- Mistakes made and corrected. Each entry prevents the same mistake recurring. -->
<!-- Format: [YYYY-MM-DD] Description of what went wrong and what to do instead. -->

- [2026-06-10] Host port **5432 is already bound by a pre-existing Postgres** (not ours; rejects pix/pix auth). Throwaway/dev containers must publish to an alternate host port (used 55432); a literal `make up` (compose publishes 5432) will fail to bind. Don't kill the existing DB.
- [2026-06-10] `postgres:16-alpine` **false-readies on first boot**: the init-script phase starts a temp server (pg_isready passes), then restarts. Wait for **occurrence ≥2** of "database system is ready to accept connections" in `docker logs` (this is why testcontainers uses `WithOccurrence(2)`). `pg_isready` alone gives "system is shutting down" / connection-reset.
- [2026-06-10] Plan File 01 Task 5 `MaskDoc` had a **test/impl mismatch** (test expected CNPJ `…/4567-…`, impl sliced `doc[8:12]`=`0001`). Resolved to `0001` (impl was internally consistent with the `/NNNN` slot). Auto-logged as bug-005.
- [2026-06-12] Migration `GRANT ... TO pix_app` fails under testcontainers — `deploy/compose/initdb/00-roles.sql` (which creates `pix_app`) is compose-only and not mounted there. Any migration granting to `pix_app` must `CREATE ROLE IF NOT EXISTS` itself first. Auto-logged as bug-011.

## Decision Log

<!-- Significant technical decisions with rationale. Why X was chosen over Y. -->

- [2026-06-10] **Go 1.25 across go.mod / Dockerfile / CI**, overriding plan File 01's 1.24. Forced: latest testcontainers/gin require ≥1.25. Chose to align everything up rather than pin older deps, since the plan's own Task 1 used `go get ...@latest`.
- [2026-06-10] **git init + `phase1-foundation` feature branch**; implemented **File 01 only** this session, then checkpoint (user choices via executing-plans skill).
- [2026-06-10] **Idempotent-replay protocol extracted to `idempotency.Middleware`** (gin.HandlerFunc, `internal/platform/idempotency/middleware.go`, File 04 Task 6) instead of inlined in `charge/api/handler.create()` (File 05 Task 3). From architecture-review Candidate 1 — chose the zero-config-middleware design over (a) a `Guard.Run` higher-order function that owns the whole HTTP response, and (b) a policy-object middleware with 5 option funcs for hypothetical Refund/CobV needs. Rationale: matches Gin's existing cross-cutting-middleware idiom (same split as `tenantctx.Middleware`), passes the deletion test (delete it → the gate/fingerprint/branch/persist protocol reappears in every future write handler), and doesn't pay for flexibility no caller needs yet. Extension path if Phase 2 needs a different fingerprint/response mapping: additive sibling constructors (`MiddlewareWithConfig`), non-breaking.
- [2026-06-10] **Platform packages may depend on `gin`** when providing HTTP-protocol glue — precedent is `internal/platform/health.Register(r *gin.Engine, ...)`. So `idempotency.Middleware` lives in the same package as `idempotency.Store`/`PgStore`, not a separate `replay` sub-package — one import (`internal/platform/idempotency`) gives callers both the store and the middleware.
- [2026-06-10] `RegisterRoutes(rg, h, idem)` applies `idempotency.Middleware(idem)` **only to `POST /charges`**, not `GET /charges/:id` — replay protocol is for at-most-once writes; reads don't need it. `Handler` struct/`NewHandler` no longer hold an `idem` field — the handler is now fully idempotency-agnostic (`process()` unchanged).
- [2026-06-10] **`tenantctx.Resolved` gains `PixKey string`** (additive to the locked signature, not an ADR-0003 conflict) and `tenant/app.Repository` collapses from 3 methods (`DefaultProvider`/`Provider`/`PixKeyFor`) to 2 (`TenantByAPIKeyHash`/`ResolveAccount`). From architecture-review Candidate 2 — chose a hybrid: Design 3's single-query seam (`internal/tenant/infra` resolves default-or-explicit provider + its PixKey in one joined query, `($2='' AND is_default) OR id::text=$2`, zero Go-level branching) **+** Design 1's error-disambiguation fallback (on zero rows, a same-tx existence check distinguishes "no default provider configured" vs "unknown provider id vs tenant's other providers", preserving 3 distinct `KindValidation` messages). Carrier is a lean `tenant/app.ResolvedAccount{ProviderID, PixKey string}` — not Design 2's full `domain.PaymentProvider` (rejected: `AccountLabel`/`WebhookConfig`/`IsDefault` have zero Phase-1 callers outside the resolver itself, fails the deletion test). Net effect: `resolver.Resolve` drops from 3 calls/transactions to 2 (`TenantByAPIKeyHash` + `ResolveAccount`), and `charge/api.Handler` drops its `tenants tenantapp.Repository` dependency entirely — `process()` reads `res.PixKey` straight from `tenantctx.Resolved` (no 3rd query). Implemented across plan Files 00/02/05 (pre-build, plan-doc edits only — File 01 is the only built code and is unaffected).
- [2026-06-12] **Implemented File 02 (tenant-provider) per the Candidate-2-revised plan** — all 6 tasks built as written (schema+RLS+seed, domain types, tenantctx+HashAPIKey, pgx repository w/ ResolveAccount join, Resolver, gin Middleware), one commit per task, TDD red→green. Only deviation from the plan text: migration 00002 needed an idempotent `CREATE ROLE pix_app IF NOT EXISTS` before its `GRANT` (bug-011) — found via the integration test, fixed in the same commit as Task 4. `go build/vet/test -race -cover`, `golangci-lint run`, and `go test -tags=integration ./...` all green.
