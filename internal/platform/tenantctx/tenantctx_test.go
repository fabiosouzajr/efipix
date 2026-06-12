package tenantctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	ctx := With(context.Background(), &Resolved{TenantID: "t1", ProviderID: "p1", PixKey: "k1"})
	got, ok := From(ctx)
	require.True(t, ok)
	require.Equal(t, "t1", got.TenantID)
	require.Equal(t, "p1", got.ProviderID)
	require.Equal(t, "k1", got.PixKey)

	_, ok = From(context.Background())
	require.False(t, ok)
}
