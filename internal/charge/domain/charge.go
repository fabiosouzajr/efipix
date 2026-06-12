package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
)

type ChargeKind string

const (
	KindCob  ChargeKind = "cob"
	KindCobV ChargeKind = "cobv"
)

type ChargeStatus string

const (
	StatusCreated   ChargeStatus = "CREATED"
	StatusActive    ChargeStatus = "ACTIVE"
	StatusPending   ChargeStatus = "PENDING"
	StatusPaid      ChargeStatus = "PAID"
	StatusExpired   ChargeStatus = "EXPIRED"
	StatusCancelled ChargeStatus = "CANCELLED"
	StatusRefunded  ChargeStatus = "REFUNDED"
	StatusFailed    ChargeStatus = "FAILED"
)

type Payer struct {
	Doc     string
	DocType string
	Name    string
	Email   string
	Phone   string
}

type PaymentEvent struct {
	ID         string
	ChargeID   string
	EventType  string
	Payload    []byte
	OccurredAt time.Time
}

type Charge struct {
	ID                string
	TenantID          string
	PaymentProviderID string
	Txid              string
	Kind              ChargeKind
	Status            ChargeStatus
	Amount            money.Centavos
	PixKey            string
	Description       string
	ExpirationSeconds int
	LocationID        string
	QRCodeImage       string
	PixPayload        string
	Payer             Payer
	ExternalReference string
	Version           int
	Events            []PaymentEvent // pending audit events to persist
}

type NewImmediateParams struct {
	TenantID          string
	PaymentProviderID string
	PixKey            string
	Amount            money.Centavos
	Description       string
	ExpirationSeconds int
	Payer             Payer
	ExternalReference string
}

// NewTxid returns a client-defined txid: 32 lowercase-hex chars (within EFí's 26-35 alnum range).
func NewTxid() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func NewImmediate(p NewImmediateParams) (*Charge, error) {
	if p.Amount <= 0 {
		return nil, apperrs.New(apperrs.KindValidation, "amount must be positive")
	}
	if p.PixKey == "" {
		return nil, apperrs.New(apperrs.KindValidation, "pix key required")
	}
	if p.ExpirationSeconds <= 0 {
		p.ExpirationSeconds = 3600
	}
	id := uuid.NewString()
	c := &Charge{
		ID: id, TenantID: p.TenantID, PaymentProviderID: p.PaymentProviderID,
		Txid: NewTxid(), Kind: KindCob, Status: StatusCreated, Amount: p.Amount,
		PixKey: p.PixKey, Description: p.Description, ExpirationSeconds: p.ExpirationSeconds,
		Payer: p.Payer, ExternalReference: p.ExternalReference, Version: 0,
	}
	c.appendEvent("created")
	return c, nil
}

func (c *Charge) appendEvent(t string) {
	c.Events = append(c.Events, PaymentEvent{
		ID: uuid.NewString(), ChargeID: c.ID, EventType: t,
		Payload: []byte("{}"), OccurredAt: time.Now().UTC(),
	})
}

// MarkActive transitions CREATED -> ACTIVE, storing provider location/QR/payload.
func (c *Charge) MarkActive(locationID, qrImage, pixPayload string) error {
	if c.Status != StatusCreated {
		return apperrs.New(apperrs.KindConflict, "charge not in CREATED state")
	}
	c.Status = StatusActive
	c.LocationID = locationID
	c.QRCodeImage = qrImage
	c.PixPayload = pixPayload
	c.appendEvent("activated")
	return nil
}

// MarkFailed transitions CREATED -> FAILED (provider create failed).
func (c *Charge) MarkFailed(reason string) error {
	if c.Status != StatusCreated {
		return apperrs.New(apperrs.KindConflict, "charge not in CREATED state")
	}
	c.Status = StatusFailed
	c.appendEvent("failed")
	return nil
}
