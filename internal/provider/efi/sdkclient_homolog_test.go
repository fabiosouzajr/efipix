//go:build homolog

package efi

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/platform/secrets"
	"github.com/efipix/pix/internal/provider"
)

// Run with: EFI_CREDENTIALS=... EFI_TEST_PROVIDER_ID=... EFI_TEST_PIX_KEY=... go test -tags=homolog ./internal/provider/efi/
func TestHomologCreateAndGet(t *testing.T) {
	if os.Getenv("EFI_CREDENTIALS") == "" {
		t.Skip("no EFI_CREDENTIALS")
	}
	sp, err := secrets.NewEnv()
	require.NoError(t, err)
	p := New(sp, SDKFactory)

	providerID := os.Getenv("EFI_TEST_PROVIDER_ID")
	pixKey := os.Getenv("EFI_TEST_PIX_KEY")
	txid := "PIX" + uuid.NewString()
	txid = txid[:35]

	res, err := p.CreateImmediateCharge(context.Background(), &provider.ImmediateChargeInput{
		Txid: txid, PaymentProviderID: providerID, Amount: money.Centavos(100),
		PixKey: pixKey, ExpirationSeconds: 3600,
	})
	require.NoError(t, err)
	require.Equal(t, "ATIVA", res.Status)
	require.NotEmpty(t, res.PixPayload)

	got, err := p.GetCharge(context.Background(), providerID, txid)
	require.NoError(t, err)
	require.Equal(t, txid, got.Txid)
}
