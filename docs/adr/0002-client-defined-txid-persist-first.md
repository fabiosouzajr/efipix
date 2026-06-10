# 2. Client-defined txid and persist-first charge creation

Date: 2026-06-09
Status: Accepted

## Context

Creating a charge involves two systems that can fail independently: our database and the EFí Pix API. EFí supports two ways to create a cob/cobv:

- **PSP-defined txid**: `POST /v2/cob` — EFí mints the txid and returns it.
- **Client-defined txid**: `PUT /v2/cob/:txid` — we choose the txid (26–35 char alphanumeric). Re-issuing the same `PUT` is idempotent: it returns the existing charge rather than creating a duplicate.

The requirements mandate a `CREATED` status and full audit history for every charge. A naive "call EFí, then persist the row it returns" approach has two defects:

1. A failed or timed-out EFí call leaves **no local record** — no CREATED, no FAILED, no audit of the attempt.
2. A client retry after a timeout risks creating a **duplicate charge** at EFí, because we have no stable key to deduplicate against.

## Decision

We mint the txid ourselves and persist the charge **before** calling EFí (persist-first, two-phase):

1. Mint a client-defined txid (hex of a UUID, within EFí's 26–35 char alphanumeric constraint).
2. **Tx A** — insert the Charge as `CREATED` with the txid + a `PaymentEvent(created)`.
3. Call `PUT /v2/cob/:txid` (or cobv). Idempotent by txid, so retries are safe.
4. **Tx B** — on success: transition to `ACTIVE`, store QR code + Pix payload, append outbox `ChargeCreated`. On failure: transition to `FAILED` + `PaymentEvent(failed)`.

The client `Idempotency-Key` (see spec §8) binds 1:1 to the minted txid, so Idempotency-Key, txid, and Charge form a 1:1:1 relationship. A cleanup/retry job handles charges stuck in `CREATED` (provider call never confirmed).

## Consequences

- Every charge attempt is recorded, satisfying the audit requirement even when EFí fails.
- The provider call is naturally retry-safe — the txid is the deduplication anchor at EFí; replays return the same cob.
- Two transactions per creation instead of one, and charges may sit in `CREATED` or `FAILED` without reaching `ACTIVE`. A reconciliation/cleanup job must resolve stuck `CREATED` rows.
- We forgo EFí's PSP-defined txid convenience; we own txid format and generation.
- A future provider must accept a client-supplied transaction id (or the adapter maps our txid to the provider's scheme). Most Pix PSPs support client-defined txid; if one does not, the adapter absorbs the difference.
