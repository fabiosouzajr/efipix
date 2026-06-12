# Phase 6 Spec — Production Hardening: Security, Observability, Resilience, Deploy

**Date:** 2026-06-10
**Status:** Approved — pending implementation plan
**Master design:** [pix-payment-platform-design](2026-06-09-pix-payment-platform-design.md) · **Glossary:** [CONTEXT.md](../../../CONTEXT.md)
**Decisions:** [ADR-0003](../../adr/0003-multi-tenant-shared-db-provider-account.md) (RLS/tenancy), [ADR-0004](../../adr/0004-webhook-ingress-mtls-termination.md) (webhook mTLS), master §8/§9/§15
**Depends on:** Phases 1–5 (the full functional surface to harden)

---

## 1. Goal

Make the platform production-ready: complete security controls, full observability, resilience around external integrations, deployment artifacts (K8s + Helm), load validation, a complete OpenAPI contract, and a signed-off security/risk review.

## 2. Scope

**In scope**
- **Security:** RBAC for internal/admin operations; per-(tenant, API-key) rate limiting; IP allow lists (admin + EFí webhook source); secret rotation support; `payer_doc` at-rest encryption decision + implementation; OWASP review; risk assessment.
- **Observability:** OpenTelemetry tracing across API → use case → provider → DB → consumers; Prometheus metrics (`charges_created_total`, `charges_paid_total`, `webhook_failures_total`, `notification_failures_total`, `forwarding_failures_total`, latency histograms); Grafana dashboards.
- **Resilience:** retry (exp backoff + jitter) standardized on EFí calls / notifications / queue ops; circuit breaker around EFí provider; DLQ operations (admin replay endpoint + CLI, poison-message inspection).
- **Secret backends:** Vault + AWS Secrets Manager `SecretProvider` implementations (interface from Phase 1).
- **Deploy:** production Dockerfile; Kubernetes manifests; Helm chart (incl. nginx mTLS ingress for webhooks, config/secret wiring, HPA); environment templates.
- **Contract/quality:** complete `api/openapi.yaml`; contract + webhook + integration + load test suites; coverage `≥80%` overall enforced in CI; CI security scan + image publish + deploy stages.

**Out of scope**
- New functional features (this phase hardens existing behavior).

## 3. Functional requirements

- **RBAC:** roles for client (API-key) vs internal/admin operations (DLQ replay, reports config, webhook registration); enforced in middleware; least privilege.
- **Rate limiting:** Redis token bucket keyed by API key (tenant fallback); stricter bucket for charge creation; EFí webhook endpoint exempt from client rate limiting but IP-allowlisted.
- **Secret rotation:** credentials re-fetched on TTL / rotation signal without redeploy; the EfiProvider client pool invalidates and rebuilds the affected account's client on rotation.
- **PII at rest:** `payer_doc` encrypted (pgcrypto/app-layer) or accepted under disk-encryption+RLS — decide and implement; document in the security review.
- **Tracing/metrics:** every request carries correlation + request IDs; spans exported via OTLP; metrics scraped by Prometheus; dashboards cover the golden signals + business counters.
- **Resilience:** EFí calls wrapped in retry + circuit breaker; breaker-open returns a typed provider error (502) fast; DLQ messages are replayable after a fix with original headers/correlation IDs.
- **Deploy:** `helm install` brings up the service + nginx mTLS ingress in a cluster; the service is stateless and horizontally scalable; HPA on CPU/latency.
- **Load:** defined throughput/latency targets for charge creation + webhook ingestion are met under load test.

## 4. Domain changes

None. Adds an RBAC role/permission model at the platform/auth layer (not a payment-domain aggregate) and a typed circuit-breaker-open provider error.

## 5. Data model changes

- RBAC: `roles` / `role_bindings` (or extend `api_keys` with a scope/role column) — decide in plan.
- `payer_doc` encryption may change column type/storage (migration + backfill).
- Audit logging populated for sensitive mutations (`audit_logs` table from master §6).
- No other functional schema changes.

## 6. API

```Text
# Hardening of existing routes (auth/rate-limit/RBAC), plus:
POST /api/v1/admin/dlq/replay        # RBAC-gated DLQ reprocessing
GET  /metrics                        # Prometheus
# /health /ready /live remain; OpenAPI completed for ALL routes
```

## 7. Key flows

- Secret rotation: rotation signal → SecretProvider re-fetch → EfiProvider evicts the account's pooled client → next call rebuilds with new cert/creds.
- Circuit breaker: sustained EFí failures open the breaker → fast 502 + metric → half-open probe → close on recovery.
- DLQ replay: admin lists DLQ → fixes cause → replays → message re-enters its consumer.

## 8. Provider / SDK

No new EFí endpoints. Wraps existing provider calls with retry + circuit breaker; per-account client eviction on rotation.

## 9. Cross-cutting

This phase *is* cross-cutting: completes observability, resilience, and security that earlier phases deferred. Standardizes the retry/circuit-breaker/DLQ utilities introduced piecemeal in Phases 3–4.

## 10. Dependencies

Phases 1–5 (full functional surface). Builds on the outbox/RabbitMQ + scheduler (Phase 3), notification/forwarding consumers (Phase 4), and reports (Phase 5).

## 11. Risks / open items

- RBAC model scope (how granular) — keep minimal: client vs admin + a few permissions.
- Encryption choice for `payer_doc` (pgcrypto vs app-layer KMS) — trade off searchability vs protection.
- Circuit-breaker thresholds + retry budgets must avoid amplifying EFí incidents.
- Load-test environment fidelity vs production; targets must be agreed before sign-off.
- Helm/K8s secret + mTLS cert wiring per environment.

## 12. Exit criteria

- Load-test throughput/latency targets met.
- Security/OWASP review signed off; risk assessment documented; PII-at-rest decision implemented.
- `helm install` deploys service + nginx mTLS ingress successfully to a cluster.
- Tracing + metrics visible in dashboards; RBAC + rate limiting + IP allow lists enforced.
- Circuit breaker + retry + DLQ replay demonstrated.
- `api/openapi.yaml` complete; overall coverage `≥80%`; CI lint/test/security-scan/build/publish/deploy green.

## 13. Testing focus

Load tests (charge create + webhook ingestion); chaos/failure injection for breaker + retry + DLQ; RBAC + rate-limit + IP-allowlist enforcement; secret-rotation client eviction; OpenAPI contract validation; security scan (deps + SAST) in CI.
