package tenantctx

import "context"

type Resolved struct {
	TenantID   string
	ProviderID string
	PixKey     string
}

type ctxKey struct{}

func With(ctx context.Context, r *Resolved) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

func From(ctx context.Context) (*Resolved, bool) {
	r, ok := ctx.Value(ctxKey{}).(*Resolved)
	return r, ok
}
