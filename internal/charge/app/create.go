package app

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/efipix/pix/internal/charge/domain"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/provider"
)

type CreateImmediateChargeCmd struct {
	TenantID          string
	PaymentProviderID string
	PixKey            string
	Amount            money.Centavos
	Description       string
	ExpirationSeconds int
	Payer             domain.Payer
	ExternalReference string
}

type CreateImmediateCharge struct {
	repo ChargeRepository
	prov provider.PixProvider
}

func NewCreateImmediateCharge(repo ChargeRepository, prov provider.PixProvider) *CreateImmediateCharge {
	return &CreateImmediateCharge{repo: repo, prov: prov}
}

func (uc *CreateImmediateCharge) Execute(ctx context.Context, cmd CreateImmediateChargeCmd) (*domain.Charge, error) {
	c, err := domain.NewImmediate(domain.NewImmediateParams{
		TenantID: cmd.TenantID, PaymentProviderID: cmd.PaymentProviderID, PixKey: cmd.PixKey,
		Amount: cmd.Amount, Description: cmd.Description, ExpirationSeconds: cmd.ExpirationSeconds,
		Payer: cmd.Payer, ExternalReference: cmd.ExternalReference,
	})
	if err != nil {
		return nil, err
	}

	// tx A: record intent as CREATED before calling the provider.
	if err := uc.repo.Create(ctx, c); err != nil {
		return nil, err
	}

	res, perr := uc.prov.CreateImmediateCharge(ctx, &provider.ImmediateChargeInput{
		Txid: c.Txid, PaymentProviderID: c.PaymentProviderID, Amount: c.Amount, PixKey: c.PixKey,
		Description: c.Description, ExpirationSeconds: c.ExpirationSeconds,
		PayerDoc: c.Payer.Doc, PayerDocType: c.Payer.DocType, PayerName: c.Payer.Name,
	})
	if perr != nil {
		_ = c.MarkFailed(perr.Error())
		if serr := uc.repo.Save(ctx, c); serr != nil {
			return nil, serr
		}
		// Classify as a provider failure so the API maps it to 502 regardless of the
		// underlying error's kind (the real EfiProvider already wraps as KindProvider).
		return nil, apperrs.Wrap(apperrs.KindProvider, "provider charge creation failed", perr)
	}

	if err := c.MarkActive(res.LocationID, res.QRCodeImage, res.PixPayload); err != nil {
		return nil, err
	}
	evt := OutboxEvent{
		ID: uuid.NewString(), TenantID: c.TenantID, AggregateID: c.ID,
		Type: "ChargeCreated", Payload: chargeCreatedPayload(c),
	}
	if err := uc.repo.Save(ctx, c, evt); err != nil {
		return nil, err
	}
	return c, nil
}

func chargeCreatedPayload(c *domain.Charge) []byte {
	b, _ := json.Marshal(map[string]any{
		"charge_id": c.ID, "txid": c.Txid, "status": string(c.Status), "amount": int64(c.Amount),
	})
	return b
}
