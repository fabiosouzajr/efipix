package efi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/platform/secrets"
	"github.com/efipix/pix/internal/provider"
)

type fakeClient struct {
	created cobInput
	calls   int
}

func (f *fakeClient) CreateCob(_ context.Context, in cobInput) (cobOutput, error) {
	f.created = in
	f.calls++
	return cobOutput{Txid: in.Txid, Status: "ATIVA", LocationID: "loc1",
		QRCodeImage: "base64png", PixPayload: "000201..."}, nil
}
func (f *fakeClient) GetCob(_ context.Context, txid string) (cobOutput, error) {
	return cobOutput{Txid: txid, Status: "CONCLUIDA"}, nil
}

type fakeSecrets struct{ calls int }

func (f *fakeSecrets) ProviderCredentials(_ context.Context, _ string) (*secrets.ProviderCreds, error) {
	f.calls++
	return &secrets.ProviderCreds{ClientID: "c", ClientSecret: "s", CertPEM: []byte("x"), KeyPEM: []byte("y")}, nil
}

func newProvider(t *testing.T, fc *fakeClient) (*EfiProvider, *fakeSecrets) {
	sp := &fakeSecrets{}
	p := New(sp, func(*secrets.ProviderCreds) (efiClient, error) { return fc, nil })
	return p, sp
}

func TestCreateImmediateChargeMapsFields(t *testing.T) {
	fc := &fakeClient{}
	p, _ := newProvider(t, fc)
	res, err := p.CreateImmediateCharge(context.Background(), &provider.ImmediateChargeInput{
		Txid: "tx1", PaymentProviderID: "prov-1", Amount: money.Centavos(1050),
		PixKey: "k@e.com", Description: "d", ExpirationSeconds: 3600,
	})
	require.NoError(t, err)
	require.Equal(t, "10.50", fc.created.Amount)
	require.Equal(t, "tx1", res.Txid)
	require.Equal(t, "ATIVA", res.Status)
	require.Equal(t, "loc1", res.LocationID)
	require.Equal(t, "base64png", res.QRCodeImage)
}

func TestClientPoolCachesPerProvider(t *testing.T) {
	fc := &fakeClient{}
	p, sp := newProvider(t, fc)
	in := &provider.ImmediateChargeInput{Txid: "a", PaymentProviderID: "prov-1", Amount: 100}
	_, _ = p.CreateImmediateCharge(context.Background(), in)
	_, _ = p.CreateImmediateCharge(context.Background(), in)
	require.Equal(t, 1, sp.calls, "credentials fetched once and client cached")
}

func TestGetCharge(t *testing.T) {
	fc := &fakeClient{}
	p, _ := newProvider(t, fc)
	res, err := p.GetCharge(context.Background(), "prov-1", "tx9")
	require.NoError(t, err)
	require.Equal(t, "tx9", res.Txid)
	require.Equal(t, "CONCLUIDA", res.Status)
}
