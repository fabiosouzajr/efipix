package app

import "context"

type Repository interface {
	// TenantByAPIKeyHash resolves an active tenant id from an API key hash (auth path; no tenant ctx).
	TenantByAPIKeyHash(ctx context.Context, keyHash string) (tenantID string, err error)
	// ResolveAccount resolves the acting payment provider and its pix key for a
	// tenant in one round trip. If explicitProviderID == "", it resolves the
	// tenant's active default provider; otherwise it validates that
	// explicitProviderID belongs to the tenant and is active. Returns
	// KindValidation if no provider resolves, or if the resolved provider has
	// no pix key.
	ResolveAccount(ctx context.Context, tenantID, explicitProviderID string) (ResolvedAccount, error)
}

// ResolvedAccount is the join result of account resolution: the acting
// payment provider and the pix key new Charges should address.
type ResolvedAccount struct {
	ProviderID string
	PixKey     string
}
