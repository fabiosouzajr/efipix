# Phase 5 Spec — Reporting & Exports

**Date:** 2026-06-10
**Status:** Approved — pending implementation plan
**Master design:** [pix-payment-platform-design](2026-06-09-pix-payment-platform-design.md) · **Glossary:** [CONTEXT.md](../../../CONTEXT.md)
**Decisions:** master §6 (data model), [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md) (overdue = derived predicate)
**Depends on:** Phases 1–4 (charges, payments, refunds, notifications, forwarding data)

---

## 1. Goal

Expose **financial**, **collection**, and **operational** reports over the accumulated tenant data, exportable as CSV / XLSX / JSON, all tenant-scoped.

## 2. Scope

**In scope**
- Financial: daily / monthly / yearly revenue (settled payments net of refunds).
- Collection: overdue charges, unpaid charges, payment aging buckets.
- Operational: webhook failures, notification failures, forwarding failures, processing times.
- Export to CSV, XLSX, JSON for each report.
- Read-optimized query layer (`reporting/` module: `app` + `infra` + `api`, no domain entity).

**Out of scope**
- Real-time dashboards / Grafana (Phase 6 observability).
- Materialized views / warehouse offload (note as future; Phase 5 uses indexed SQL, add a materialized view only if a report is too slow).

## 3. Functional requirements

- Revenue reports aggregate `payments.amount` minus completed `refunds.amount` grouped by day/month/year, filterable by date range and (optionally) `payment_provider_id`.
- Overdue report lists CobV charges where `due_date < today AND status = ACTIVE` (derived predicate, [ADR-0001](../../adr/0001-charge-lifecycle-status-model.md)); unpaid lists ACTIVE charges past expectation; aging buckets (0–30 / 31–60 / 61–90 / 90+ days).
- Operational reports read `webhook_logs`, `notification_logs`, `webhook_delivery_logs`, `payment_events` for failure counts and timing percentiles.
- Each report endpoint accepts `?format=csv|xlsx|json` (default json) and streams the file with correct content-type + filename; large results stream rather than buffer.
- All reports are strictly tenant-scoped (RLS + explicit tenant filter); no cross-tenant aggregation.

## 4. Domain changes

None — reporting is a read-model/query concern, not a domain aggregate. Define report DTOs + query params only.

## 5. Data model changes

No new tables expected. Add covering/composite indexes to support aggregation (e.g. `payments(tenant_id, paid_at)`, `charges(tenant_id, kind, due_date, status)`, `refunds(tenant_id, status, completed_at)`). Consider a materialized view for revenue if needed (decide in plan); refresh strategy then documented.

## 6. API

```Text
GET /api/v1/reports/revenue     # ?granularity=daily|monthly|yearly&from&to&format
GET /api/v1/reports/overdue     # ?as_of&format
GET /api/v1/reports/payments    # ?from&to&format  (payments + aging)
# operational reports may be admin/RBAC-gated (finalized with Phase 6 RBAC)
```

## 7. Key flow

Request → validate params + tenant scope → run aggregation query (within a tenant tx) → stream to the requested exporter (CSV/XLSX/JSON) → respond. Exporters share a common row-stream interface so a query feeds any format.

## 8. Provider / SDK

None. Pure data + export libraries (e.g. `encoding/csv`, an XLSX writer such as `excelize`).

## 9. Cross-cutting

Reuses RLS tenant scoping; introduces a streaming-export utility (`reporting/infra/export`) shared by all reports. Read-only; no writes/events.

## 10. Dependencies

Phases 1–4 to have charges/payments/refunds/notification/forwarding data to report on.

## 11. Risks / open items

- Query performance on large tenants — index first; materialized views only if measured slow.
- XLSX library memory for large exports — stream/paginate; cap or async-generate very large reports (decide threshold).
- Timezone handling for daily/monthly buckets (tenant tz vs UTC) — define explicitly.
- Money rounding/representation in exports (centavos → decimal string consistently).

## 12. Exit criteria

- Revenue, overdue, and payments reports return correct, tenant-scoped results.
- Each exports correctly in CSV, XLSX, and JSON.
- A large-result export streams without unbounded memory growth.
- Aggregations verified against seeded fixtures with known totals.

## 13. Testing focus

Aggregation correctness against fixtures (revenue net of refunds, aging buckets); tenant isolation of reports; exporter format validity (parse CSV/XLSX back, JSON schema); streaming/pagination for large sets; timezone boundary cases.
