package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apperrs "github.com/efipix/pix/internal/platform/errors"
)

type fakeRepo struct {
	tenantID string
	accounts map[string]ResolvedAccount // keyed by explicit providerID; "" = default
}

func (f *fakeRepo) TenantByAPIKeyHash(_ context.Context, h string) (string, error) {
	if h == HashAPIKey("good") {
		return f.tenantID, nil
	}
	return "", apperrs.New(apperrs.KindUnauthorized, "invalid api key")
}
func (f *fakeRepo) ResolveAccount(_ context.Context, _, providerID string) (ResolvedAccount, error) {
	if a, ok := f.accounts[providerID]; ok {
		return a, nil
	}
	return ResolvedAccount{}, apperrs.New(apperrs.KindValidation, "unknown or inactive provider")
}

func newFake() *fakeRepo {
	return &fakeRepo{
		tenantID: "t1",
		accounts: map[string]ResolvedAccount{
			"":   {ProviderID: "pdef", PixKey: "k-def"},
			"pX": {ProviderID: "pX", PixKey: "k-x"},
		},
	}
}

func TestResolveDefaultProvider(t *testing.T) {
	r := NewResolver(newFake())
	res, err := r.Resolve(context.Background(), "good", "")
	require.NoError(t, err)
	require.Equal(t, "t1", res.TenantID)
	require.Equal(t, "pdef", res.ProviderID)
	require.Equal(t, "k-def", res.PixKey)
}

func TestResolveExplicitProvider(t *testing.T) {
	r := NewResolver(newFake())
	res, err := r.Resolve(context.Background(), "good", "pX")
	require.NoError(t, err)
	require.Equal(t, "pX", res.ProviderID)
	require.Equal(t, "k-x", res.PixKey)
}

func TestResolveBadKey(t *testing.T) {
	r := NewResolver(newFake())
	_, err := r.Resolve(context.Background(), "bad", "")
	require.Equal(t, apperrs.KindUnauthorized, apperrs.KindOf(err))
}

func TestResolveUnknownExplicitProvider(t *testing.T) {
	r := NewResolver(newFake())
	_, err := r.Resolve(context.Background(), "good", "nope")
	require.Equal(t, apperrs.KindValidation, apperrs.KindOf(err))
}
