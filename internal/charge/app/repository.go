package app

import (
	"context"

	"github.com/efipix/pix/internal/charge/domain"
)

type OutboxEvent struct {
	ID          string
	TenantID    string
	AggregateID string
	Type        string
	Payload     []byte
}

type ChargeRepository interface {
	// Create = tx A: insert CREATED charge + its pending audit events.
	Create(ctx context.Context, c *domain.Charge) error
	// Save = tx B: optimistic-lock update by (id, version)->version+1, insert the charge's new
	// audit events, and insert the given outbox events — all in one tenant-scoped tx.
	Save(ctx context.Context, c *domain.Charge, out ...OutboxEvent) error
	FindByID(ctx context.Context, tenantID, id string) (*domain.Charge, error)
	FindByTxID(ctx context.Context, tenantID, txid string) (*domain.Charge, error)
}
