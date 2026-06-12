package app

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/charge/domain"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/provider"
)

type fakeRepo struct {
	created  *domain.Charge
	saved    *domain.Charge
	savedOut []OutboxEvent
	store    map[string]*domain.Charge
}

func newFakeRepo() *fakeRepo { return &fakeRepo{store: map[string]*domain.Charge{}} }

func (f *fakeRepo) Create(_ context.Context, c *domain.Charge) error {
	f.created = c
	cp := *c
	f.store[c.ID] = &cp
	return nil
}
func (f *fakeRepo) Save(_ context.Context, c *domain.Charge, out ...OutboxEvent) error {
	f.saved = c
	f.savedOut = out
	cp := *c
	f.store[c.ID] = &cp
	return nil
}
func (f *fakeRepo) FindByID(_ context.Context, _, id string) (*domain.Charge, error) {
	if c, ok := f.store[id]; ok {
		return c, nil
	}
	return nil, apperrs.New(apperrs.KindNotFound, "nf")
}
func (f *fakeRepo) FindByTxID(_ context.Context, _, _ string) (*domain.Charge, error) {
	return nil, apperrs.New(apperrs.KindNotFound, "nf")
}

type fakeProv struct {
	fail bool
}

func (f *fakeProv) CreateImmediateCharge(_ context.Context, in *provider.ImmediateChargeInput) (*provider.ChargeResult, error) {
	if f.fail {
		return nil, errors.New("efi down")
	}
	return &provider.ChargeResult{Txid: in.Txid, Status: "ATIVA", LocationID: "loc1",
		QRCodeImage: "img", PixPayload: "000201..."}, nil
}
func (f *fakeProv) GetCharge(_ context.Context, _, _ string) (*provider.ChargeResult, error) {
	return nil, nil
}

func cmd() CreateImmediateChargeCmd {
	return CreateImmediateChargeCmd{
		TenantID: "t1", PaymentProviderID: "p1", PixKey: "k@e.com",
		Amount: money.Centavos(1050), ExpirationSeconds: 3600,
	}
}

func TestCreateSuccess(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateImmediateCharge(repo, &fakeProv{})
	c, err := uc.Execute(context.Background(), cmd())
	require.NoError(t, err)
	require.Equal(t, domain.StatusActive, c.Status)
	require.Equal(t, "000201...", c.PixPayload)
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.saved)
	require.Len(t, repo.savedOut, 1)
	require.Equal(t, "ChargeCreated", repo.savedOut[0].Type)
}

func TestCreateProviderFailureMarksFailed(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateImmediateCharge(repo, &fakeProv{fail: true})
	_, err := uc.Execute(context.Background(), cmd())
	require.Error(t, err)
	require.Equal(t, domain.StatusFailed, repo.saved.Status)
	require.Empty(t, repo.savedOut, "no outbox event on failure")
}
