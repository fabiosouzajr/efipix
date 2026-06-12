package app

import (
	"context"

	"github.com/efipix/pix/internal/platform/tenantctx"
)

type Resolver struct{ repo Repository }

func NewResolver(repo Repository) *Resolver { return &Resolver{repo: repo} }

// Resolve maps a raw API key (+ optional explicit provider id) to the acting
// tenant/provider/pix-key context.
func (r *Resolver) Resolve(ctx context.Context, rawAPIKey, explicitProviderID string) (*tenantctx.Resolved, error) {
	tenantID, err := r.repo.TenantByAPIKeyHash(ctx, HashAPIKey(rawAPIKey))
	if err != nil {
		return nil, err
	}
	acct, err := r.repo.ResolveAccount(ctx, tenantID, explicitProviderID)
	if err != nil {
		return nil, err
	}
	return &tenantctx.Resolved{TenantID: tenantID, ProviderID: acct.ProviderID, PixKey: acct.PixKey}, nil
}
