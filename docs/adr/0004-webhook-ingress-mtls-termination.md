# 4. Inbound webhook mTLS terminated at the proxy, with app-level hmac + IP allowlist

Date: 2026-06-10
Status: Accepted

## Context

EFí delivers Pix notifications (received payments and refund/devolução status) by POSTing to a registered webhook URL. Per Banco Central rules the channel is secured with **mutual TLS**: EFí presents its client certificate and the receiving server must require and validate it (TLS ≥ 1.2). There is **no signature field in the JSON payload** — authenticity rests on the transport plus optional application-level checks:

- `x-skip-mtls-checking: true` (set at webhook registration) tells EFí your server will not itself validate the client cert (used when a proxy terminates mTLS).
- An **hmac query parameter** can be appended to the registered URL (`…/webhook?hmac=<secret>&ignorar=`); the app validates it on each callback. The `ignorar=` suffix stops EFí appending `/pix` to the path.
- EFí sends exclusively from a **fixed source IP** (`34.193.116.226`).
- Callback timeout is **60 seconds**.

The service is designed to be stateless and horizontally scaled behind a load balancer / Kubernetes ingress. Terminating mTLS inside the Go process would couple the certificate/TLS lifecycle to the application and complicate the LB/ingress path.

## Decision

**Terminate EFí's mTLS at the ingress proxy; the application performs defense-in-depth checks.**

- An **nginx ingress** (following EFí's `efipay/mtls-webhook` pattern) requires and verifies EFí's client certificate. The application is registered with `x-skip-mtls-checking: true` because the proxy, not the app, validates the cert.
- The application additionally validates, on every callback:
  - the **hmac** query secret matches the value registered for that `payment_provider` (secret stored in the SecretProvider),
  - the **source IP** is `34.193.116.226` (allowlist),
  - and only then accepts the payload.
- The handler acks within the 60s window after the body is durably stored (raw → `webhook_logs`) and items are enqueued/processed.

Webhook registration is per Pix key: `PUT /v2/webhook/:chave` with `{ "webhookUrl": "…?hmac=<secret>&ignorar=" }`. The registered keys and hmac secret reference live in the `payment_providers.webhook_config`.

## Consequences

- The Go service stays stateless and TLS-agnostic; cert rotation is an ingress concern, not an app deploy.
- Two independent app-level guards (hmac + IP) backstop the proxy, so a misconfigured proxy does not silently accept forged calls.
- Requires an nginx (or equivalent) mTLS sidecar/ingress in every environment — added to `deploy/` (compose + k8s/helm). Local development uses skip-mTLS + hmac + IP only.
- If a future deployment cannot run a mTLS-capable proxy, the fallback is skip-mTLS + hmac + IP alone (weaker — no cert validation); acceptable only behind an otherwise trusted gateway.
- The hmac secret is per `payment_provider` and lives in the SecretProvider, consistent with [ADR-0003](0003-multi-tenant-shared-db-provider-account.md).
