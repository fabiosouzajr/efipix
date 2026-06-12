package provider

import (
	"context"

	"github.com/efipix/pix/internal/platform/money"
)

type ImmediateChargeInput struct {
	Txid              string
	PaymentProviderID string
	Amount            money.Centavos
	PixKey            string
	Description       string
	ExpirationSeconds int
	PayerDoc          string
	PayerDocType      string
	PayerName         string
}

type ChargeResult struct {
	Txid        string
	Status      string // raw provider status, e.g. "ATIVA"
	LocationID  string
	QRCodeImage string // base64 PNG
	PixPayload  string // copia-e-cola
}

type PixProvider interface {
	CreateImmediateCharge(ctx context.Context, in *ImmediateChargeInput) (*ChargeResult, error)
	GetCharge(ctx context.Context, paymentProviderID, txid string) (*ChargeResult, error)
}
