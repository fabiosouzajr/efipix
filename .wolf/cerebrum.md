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

## Do-Not-Repeat

<!-- Mistakes made and corrected. Each entry prevents the same mistake recurring. -->
<!-- Format: [YYYY-MM-DD] Description of what went wrong and what to do instead. -->

- [2026-06-10] Host port **5432 is already bound by a pre-existing Postgres** (not ours; rejects pix/pix auth). Throwaway/dev containers must publish to an alternate host port (used 55432); a literal `make up` (compose publishes 5432) will fail to bind. Don't kill the existing DB.
- [2026-06-10] `postgres:16-alpine` **false-readies on first boot**: the init-script phase starts a temp server (pg_isready passes), then restarts. Wait for **occurrence ≥2** of "database system is ready to accept connections" in `docker logs` (this is why testcontainers uses `WithOccurrence(2)`). `pg_isready` alone gives "system is shutting down" / connection-reset.
- [2026-06-10] Plan File 01 Task 5 `MaskDoc` had a **test/impl mismatch** (test expected CNPJ `…/4567-…`, impl sliced `doc[8:12]`=`0001`). Resolved to `0001` (impl was internally consistent with the `/NNNN` slot). Auto-logged as bug-005.

## Decision Log

<!-- Significant technical decisions with rationale. Why X was chosen over Y. -->

- [2026-06-10] **Go 1.25 across go.mod / Dockerfile / CI**, overriding plan File 01's 1.24. Forced: latest testcontainers/gin require ≥1.25. Chose to align everything up rather than pin older deps, since the plan's own Task 1 used `go get ...@latest`.
- [2026-06-10] **git init + `phase1-foundation` feature branch**; implemented **File 01 only** this session, then checkpoint (user choices via executing-plans skill).
