# Project: Enterprise Pix Payment Platform Using EFí API and Official Go SDK

You are a senior software architect, Go backend engineer, DevOps engineer, security engineer, database architect, and technical writer.

Your task is to design and implement a production-grade Pix Payment Platform using EFí's Pix API and the official EFí Go SDK.

The system must be implemented as a standalone microservice that can serve multiple applications and business units through APIs, events, and webhooks.

The resulting platform must become the centralized payment infrastructure for all company products.

---

## Primary Goals

Build a reusable payment service capable of:

* Creating Pix charges
* Creating Pix charges with due dates
* Supporting fines and interest for overdue payments
* Generating QR Codes
* Generating Pix Copy-and-Paste payloads
* Tracking payment status
* Processing EFí webhooks
* Delivering payment notifications
* Generating operational and financial reports
* Supporting multiple EFí accounts (multi-tenant)
* Running as a containerized service

---

## Official Integration Requirements

Use the official EFí Go SDK as the preferred integration mechanism.

Repository:

    https://github.com/efipay/sdk-go-apis-efi

Use EFí official API documentation as the source of truth.

Requirements:

* Use the SDK whenever possible.
* Review SDK capabilities and limitations before implementation.
* Never expose SDK models outside the Infrastructure layer.
* Create an adapter layer that converts SDK models into internal domain models.
* All business logic must remain independent of EFí.
* The service must be able to replace EFí with another payment provider in the future without major refactoring.

---

## Architecture Requirements

Use Clean Architecture.

Layers:

### Domain Layer

Contains:

* entities
* value objects
* business rules
* domain events

Must have zero EFí dependencies.

---

### Application Layer

Contains:

* use cases
* orchestration
* commands
* queries

Must depend only on interfaces.

---

### Infrastructure Layer

Contains:

* EFí SDK adapter
* database implementation
* queues
* notification providers
* storage providers

---

### API Layer

Contains:

* REST API
* OpenAPI specification
* authentication
* request validation

---

## Provider Abstraction

Design the system to support future providers.

Create interfaces such as:

```go
type PixProvider interface {
    CreateImmediateCharge(...)
    CreateDueDateCharge(...)
    GetCharge(...)
    Refund(...)
}
```

Implement:

```go
type EfiProvider struct {
}
```

All application services must communicate exclusively through interfaces.

The application layer must never directly call EFí SDK code.

---

## Multi-Tenant Support

The service must support multiple independent EFí accounts.

Each tenant must have:

* Client ID
* Client Secret
* Certificate
* Pix Keys
* Webhook configuration

Examples:

Tenant A

Tenant B

Tenant C

Implement a tenant resolution mechanism.

All requests must execute in the context of a tenant.

---

## Core Functional Modules

### Authentication Module

Responsibilities:

* OAuth2 authentication
* token caching
* token renewal
* credential validation

Support:

* P12 certificates
* PEM certificates

Secrets must come from:

* environment variables
* Vault
* AWS Secrets Manager
* equivalent secret providers

Never hardcode credentials.

---

### Pix Charge Module

#### Immediate Charge (Cob)

Input:

* amount
* description
* expiration
* customer data
* metadata
* external reference

Output:

* txid
* charge id
* QR code
* Pix payload
* status

---

#### Due-Date Charge (CobV)

Input:

* amount
* due date
* customer information
* fine rules
* interest rules
* discount rules

Output:

* txid
* charge identifier
* QR code
* Pix payload

Support:

* multa
* juros
* desconto
* abatimento

---

## Payment Lifecycle Module

Track:

* CREATED
* ACTIVE
* PENDING
* PAID
* EXPIRED
* CANCELLED
* REFUNDED
* FAILED

Store:

* txid
* e2eId
* timestamps
* provider identifiers
* audit history

---

## Webhook Processing Module

Implement secure webhook ingestion.

Requirements:

* signature validation
* idempotency
* replay protection
* retry handling

Store all incoming payloads.

Supported events:

* payment received
* charge paid
* charge expired
* refund requested
* refund completed

Webhook events become the authoritative payment source.

Do not rely primarily on polling.

---

## Notification Module

Support:

### Email

Provider abstraction.

### SMS

Provider abstraction.

### WhatsApp

Provider abstraction.

### Webhook Forwarding

Allow external systems to subscribe to events.

Events:

* charge created
* due soon
* overdue
* paid
* refunded

Implement event-driven notifications.

---

## Reporting Module

Generate:

### Financial Reports

* daily revenue
* monthly revenue
* yearly revenue

### Collection Reports

* overdue charges
* unpaid charges
* payment aging

### Operational Reports

* webhook failures
* notification failures
* processing times

Exports:

* CSV
* XLSX
* JSON

---

## Database Design

Create complete schema and ERD.

Required tables:

* tenants
* payment_providers
* pix_keys
* charges
* payments
* refunds
* payment_events
* webhook_logs
* notifications
* notification_logs
* audit_logs

Requirements:

* migrations
* indexes
* foreign keys
* optimistic locking
* soft deletes where appropriate

---

## Event-Driven Architecture

Implement domain events.

Examples:

ChargeCreated

ChargePaid

ChargeExpired

ChargeOverdue

RefundRequested

RefundCompleted

NotificationSent

NotificationFailed

Use an internal event bus abstraction.

---

## Reliability Requirements

Implement:

### Idempotency

Prevent duplicate charge creation.

Prevent duplicate webhook processing.

---

### Retry Policies

For:

* API calls
* notifications
* queue operations

---

### Circuit Breakers

Protect external integrations.

---

### Dead Letter Queues

Capture failed event processing.

---

### Outbox Pattern

Guarantee reliable event delivery.

---

## Observability

Implement:

### Logging

Structured logs.

Correlation IDs.

Request IDs.

---

### Metrics

Prometheus compatible.

Examples:

* charges created
* charges paid
* webhook failures
* notification failures

---

### Tracing

OpenTelemetry support.

---

### Health Checks

Endpoints:

/health

/ready

/live

---

## Security Requirements

Implement:

* TLS everywhere
* mTLS where required by EFí
* secret rotation support
* audit logging
* RBAC
* API key authentication for client applications
* rate limiting
* IP allow lists where appropriate

Follow OWASP recommendations.

---

## API Design

REST API.

Versioned endpoints.

Examples:

POST /api/v1/charges

GET /api/v1/charges/{id}

GET /api/v1/charges

POST /api/v1/refunds

POST /api/v1/webhooks/register

GET /api/v1/reports/revenue

GET /api/v1/reports/overdue

GET /api/v1/reports/payments

---

## Deployment Requirements

Provide:

* Dockerfile
* docker-compose
* Kubernetes manifests
* Helm chart
* environment templates

The service must be stateless.

---

## CI/CD

Generate:

GitHub Actions pipelines.

Stages:

* lint
* test
* security scan
* build
* publish image
* deploy

---

## Testing Requirements

Create:

Unit Tests

Integration Tests

Contract Tests

Webhook Tests

Load Tests

Target:

Minimum 80% coverage.

---

## Deliverables

Produce:

1. Architecture document
2. Component diagrams
3. Sequence diagrams
4. Database ERD
5. OpenAPI specification
6. Folder structure
7. Infrastructure architecture
8. Docker configuration
9. Kubernetes deployment
10. Security review
11. Risk assessment
12. Implementation roadmap

---

## Preferred Technology Stack

Language:

Go 1.24+

Framework:

Gin or Fiber

Database:

PostgreSQL

Cache:

Redis

Queue:

RabbitMQ

ORM:

GORM or SQLC

Observability:

OpenTelemetry

Metrics:

Prometheus

Containerization:

Docker

Deployment:

Kubernetes

---

## Implementation Phases

Phase 1

* tenant management
* EFí integration
* authentication
* immediate Pix charges

Phase 2

* due-date charges
* fines
* interest
* discounts

Phase 3

* webhook processing
* payment reconciliation

Phase 4

* notifications

Phase 5

* reporting

Phase 6

* production hardening
* observability
* security review

Generate implementation-ready output suitable for a senior engineering team. Avoid placeholder code, toy examples, and generic architecture.
