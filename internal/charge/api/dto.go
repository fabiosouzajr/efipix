package api

import (
	"github.com/efipix/pix/internal/charge/domain"
)

type createChargeRequest struct {
	Amount            string `json:"amount" binding:"required"` // decimal "10.50"
	Description       string `json:"description"`
	ExpirationSeconds int    `json:"expiration_seconds"`
	Payer             struct {
		Doc     string `json:"doc"`
		DocType string `json:"doc_type"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Phone   string `json:"phone"`
	} `json:"payer"`
	ExternalReference string `json:"external_reference"`
}

type chargeResponse struct {
	ID         string `json:"id"`
	Txid       string `json:"txid"`
	Status     string `json:"status"`
	Amount     string `json:"amount"`
	QRCode     string `json:"qr_code_image"`
	PixPayload string `json:"pix_payload"`
	Location   string `json:"location_id"`
}

func toResponse(c *domain.Charge) chargeResponse {
	return chargeResponse{
		ID: c.ID, Txid: c.Txid, Status: string(c.Status), Amount: c.Amount.String(),
		QRCode: c.QRCodeImage, PixPayload: c.PixPayload, Location: c.LocationID,
	}
}
