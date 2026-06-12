package infra

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	chargeapp "github.com/efipix/pix/internal/charge/app"
	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/db"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
)

type Repository struct{ pool *db.Pool }

func New(pool *db.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, c *domain.Charge) error {
	return r.pool.WithTenantTx(ctx, c.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO charges
			 (id, tenant_id, payment_provider_id, txid, kind, status, amount, pix_key,
			  description, expiration_seconds, payer_doc, payer_doc_type, payer_name,
			  payer_email, payer_phone, external_reference, version)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			c.ID, c.TenantID, c.PaymentProviderID, c.Txid, c.Kind, c.Status, int64(c.Amount), c.PixKey,
			c.Description, c.ExpirationSeconds, c.Payer.Doc, c.Payer.DocType, c.Payer.Name,
			c.Payer.Email, c.Payer.Phone, c.ExternalReference, c.Version)
		if err != nil {
			return apperrs.Wrap(apperrs.KindConflict, "insert charge", err)
		}
		return insertEvents(ctx, tx, c)
	})
}

func (r *Repository) Save(ctx context.Context, c *domain.Charge, out ...chargeapp.OutboxEvent) error {
	return r.pool.WithTenantTx(ctx, c.TenantID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx,
			`UPDATE charges SET status=$1, location_id=$2, qr_code_image=$3, pix_payload=$4,
			   version=version+1, updated_at=now()
			 WHERE id=$5 AND tenant_id=$6 AND version=$7`,
			c.Status, c.LocationID, c.QRCodeImage, c.PixPayload, c.ID, c.TenantID, c.Version)
		if err != nil {
			return apperrs.Wrap(apperrs.KindUnknown, "update charge", err)
		}
		if ct.RowsAffected() != 1 {
			return apperrs.New(apperrs.KindConflict, "optimistic lock conflict")
		}
		c.Version++
		if err := insertEvents(ctx, tx, c); err != nil {
			return err
		}
		for _, e := range out {
			if _, err := tx.Exec(ctx,
				`INSERT INTO outbox (id, tenant_id, aggregate_id, type, payload)
				 VALUES ($1,$2,$3,$4,$5)`,
				e.ID, e.TenantID, e.AggregateID, e.Type, e.Payload); err != nil {
				return apperrs.Wrap(apperrs.KindUnknown, "insert outbox", err)
			}
		}
		return nil
	})
}

// insertEvents writes the charge's pending audit events and clears them.
func insertEvents(ctx context.Context, tx pgx.Tx, c *domain.Charge) error {
	for _, e := range c.Events {
		if _, err := tx.Exec(ctx,
			`INSERT INTO payment_events (id, tenant_id, charge_id, event_type, payload, occurred_at)
			 VALUES ($1,$2,$3,$4,$5,$6)`,
			e.ID, c.TenantID, c.ID, e.EventType, e.Payload, e.OccurredAt); err != nil {
			return apperrs.Wrap(apperrs.KindUnknown, "insert event", err)
		}
	}
	c.Events = nil
	return nil
}

func (r *Repository) FindByID(ctx context.Context, tenantID, id string) (*domain.Charge, error) {
	return r.findBy(ctx, tenantID, "id", id)
}

func (r *Repository) FindByTxID(ctx context.Context, tenantID, txid string) (*domain.Charge, error) {
	return r.findBy(ctx, tenantID, "txid", txid)
}

func (r *Repository) findBy(ctx context.Context, tenantID, col, val string) (*domain.Charge, error) {
	var c domain.Charge
	var amount int64
	err := r.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id, tenant_id, payment_provider_id, txid, kind, status, amount, pix_key,
			        description, COALESCE(expiration_seconds,0), location_id, qr_code_image, pix_payload,
			        payer_doc, payer_doc_type, payer_name, payer_email, payer_phone,
			        external_reference, version
			 FROM charges WHERE `+col+`=$1 AND tenant_id=$2 AND deleted_at IS NULL`, val, tenantID,
		).Scan(&c.ID, &c.TenantID, &c.PaymentProviderID, &c.Txid, &c.Kind, &c.Status, &amount, &c.PixKey,
			&c.Description, &c.ExpirationSeconds, &c.LocationID, &c.QRCodeImage, &c.PixPayload,
			&c.Payer.Doc, &c.Payer.DocType, &c.Payer.Name, &c.Payer.Email, &c.Payer.Phone,
			&c.ExternalReference, &c.Version)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperrs.New(apperrs.KindNotFound, "charge not found")
	}
	if err != nil {
		return nil, err
	}
	c.Amount = money.Centavos(amount)
	return &c, nil
}
