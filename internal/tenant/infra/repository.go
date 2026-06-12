package infra

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/efipix/pix/internal/platform/db"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/tenant/app"
)

type Repository struct{ pool *db.Pool }

func New(pool *db.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) TenantByAPIKeyHash(ctx context.Context, keyHash string) (string, error) {
	var tenantID string
	// Auth path: admin pool (no tenant GUC yet).
	err := r.pool.WithAdminTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT k.tenant_id FROM api_keys k
			 JOIN tenants t ON t.id = k.tenant_id
			 WHERE k.key_hash = $1 AND k.status = 'active'
			   AND t.status = 'active' AND t.deleted_at IS NULL`, keyHash,
		).Scan(&tenantID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperrs.New(apperrs.KindUnauthorized, "invalid api key")
	}
	if err != nil {
		return "", err
	}
	return tenantID, nil
}

// ResolveAccount joins payment_providers to pix_keys in one query: when
// explicitProviderID == "", it matches the tenant's active default provider;
// otherwise it matches that provider id, validating it belongs to the tenant
// and is active. On no match, a same-tx existence check disambiguates "no
// such provider" from "provider has no pix key".
func (r *Repository) ResolveAccount(ctx context.Context, tenantID, explicitProviderID string) (app.ResolvedAccount, error) {
	var acct app.ResolvedAccount
	err := r.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(ctx,
			`SELECT pp.id, pk.key
			 FROM payment_providers pp
			 JOIN pix_keys pk ON pk.payment_provider_id = pp.id AND pk.tenant_id = pp.tenant_id
			 WHERE pp.tenant_id = $1 AND pp.status = 'active'
			   AND (($2 = '' AND pp.is_default) OR pp.id::text = $2)
			 ORDER BY pk.created_at ASC, pk.id ASC
			 LIMIT 1`,
			tenantID, explicitProviderID,
		).Scan(&acct.ProviderID, &acct.PixKey)
		if scanErr == nil {
			return nil
		}
		if !errors.Is(scanErr, pgx.ErrNoRows) {
			return scanErr
		}

		// No joined row: disambiguate "no such provider" from "provider has no pix key".
		var providerID string
		existsErr := tx.QueryRow(ctx,
			`SELECT id FROM payment_providers
			 WHERE tenant_id = $1 AND status = 'active'
			   AND (($2 = '' AND is_default) OR id::text = $2)`,
			tenantID, explicitProviderID,
		).Scan(&providerID)
		switch {
		case errors.Is(existsErr, pgx.ErrNoRows):
			if explicitProviderID == "" {
				return apperrs.New(apperrs.KindValidation, "no default provider for tenant")
			}
			return apperrs.New(apperrs.KindValidation, "unknown or inactive provider")
		case existsErr != nil:
			return existsErr
		default:
			return apperrs.New(apperrs.KindValidation, "no pix key for provider")
		}
	})
	if err != nil {
		return app.ResolvedAccount{}, err
	}
	return acct, nil
}
