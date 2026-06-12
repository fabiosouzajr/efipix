package efi

import (
	"context"
	"sync"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/secrets"
	"github.com/efipix/pix/internal/provider"
)

// clientFactory builds an efiClient from provider credentials (real impl in sdkclient.go).
type clientFactory func(*secrets.ProviderCreds) (efiClient, error)

type EfiProvider struct {
	secrets secrets.SecretProvider
	factory clientFactory
	mu      sync.Mutex
	pool    map[string]efiClient
}

func New(sp secrets.SecretProvider, factory clientFactory) *EfiProvider {
	return &EfiProvider{secrets: sp, factory: factory, pool: map[string]efiClient{}}
}

func (p *EfiProvider) clientFor(ctx context.Context, providerID string) (efiClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.pool[providerID]; ok {
		return c, nil
	}
	creds, err := p.secrets.ProviderCredentials(ctx, providerID)
	if err != nil {
		return nil, apperrs.Wrap(apperrs.KindProvider, "load credentials", err)
	}
	c, err := p.factory(creds)
	if err != nil {
		return nil, apperrs.Wrap(apperrs.KindProvider, "build efi client", err)
	}
	p.pool[providerID] = c
	return c, nil
}

func (p *EfiProvider) CreateImmediateCharge(ctx context.Context, in *provider.ImmediateChargeInput) (*provider.ChargeResult, error) {
	c, err := p.clientFor(ctx, in.PaymentProviderID)
	if err != nil {
		return nil, err
	}
	out, err := c.CreateCob(ctx, cobInput{
		Txid: in.Txid, PixKey: in.PixKey, Amount: in.Amount.String(),
		Description: in.Description, ExpirationSeconds: in.ExpirationSeconds,
		PayerDoc: in.PayerDoc, PayerDocType: in.PayerDocType, PayerName: in.PayerName,
	})
	if err != nil {
		return nil, apperrs.Wrap(apperrs.KindProvider, "create cob", err)
	}
	return toResult(out), nil
}

func (p *EfiProvider) GetCharge(ctx context.Context, providerID, txid string) (*provider.ChargeResult, error) {
	c, err := p.clientFor(ctx, providerID)
	if err != nil {
		return nil, err
	}
	out, err := c.GetCob(ctx, txid)
	if err != nil {
		return nil, apperrs.Wrap(apperrs.KindProvider, "get cob", err)
	}
	return toResult(out), nil
}

func toResult(o cobOutput) *provider.ChargeResult {
	return &provider.ChargeResult{
		Txid: o.Txid, Status: o.Status, LocationID: o.LocationID,
		QRCodeImage: o.QRCodeImage, PixPayload: o.PixPayload,
	}
}
