package efi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	efipix "github.com/efipay/sdk-go-apis-efi/src/efipay/pix"

	"github.com/efipix/pix/internal/platform/secrets"
)

// efiSDKClient is the subset of the methods efipix.NewEfiPay's return value
// exposes that sdkClient needs. NewEfiPay returns a pointer to an unexported
// type, so it can only be referenced through an interface like this one.
type efiSDKClient interface {
	CreateCharge(txid string, body map[string]interface{}) (string, error)
	DetailCharge(txid string) (string, error)
	PixGenerateQRCode(id string) (string, error)
}

// SDKFactory builds a real efiClient from provider credentials. Pass this to
// efi.New in production wiring.
func SDKFactory(creds *secrets.ProviderCreds) (efiClient, error) {
	certFile, err := writeTempPEM("efi-cert-*.pem", creds.CertPEM)
	if err != nil {
		return nil, err
	}
	keyFile, err := writeTempPEM("efi-key-*.pem", creds.KeyPEM)
	if err != nil {
		return nil, err
	}
	cfg := map[string]interface{}{
		"client_id":     creds.ClientID,
		"client_secret": creds.ClientSecret,
		"CA":            certFile,
		"Key":           keyFile,
		"sandbox":       creds.Sandbox,
		"timeout":       30,
	}
	return &sdkClient{efi: efipix.NewEfiPay(cfg)}, nil
}

func writeTempPEM(pattern string, pem []byte) (string, error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("efi: create temp file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(pem); err != nil {
		return "", fmt.Errorf("efi: write temp file: %w", err)
	}
	return f.Name(), nil
}

type sdkClient struct {
	efi efiSDKClient
}

func (c *sdkClient) CreateCob(_ context.Context, in cobInput) (cobOutput, error) {
	body := map[string]interface{}{
		"calendario": map[string]interface{}{"expiracao": in.ExpirationSeconds},
		"valor":      map[string]interface{}{"original": in.Amount},
		"chave":      in.PixKey,
	}
	if in.Description != "" {
		body["solicitacaoPagador"] = in.Description
	}
	if in.PayerDoc != "" {
		dev := map[string]interface{}{"nome": in.PayerName}
		dev[in.PayerDocType] = in.PayerDoc // "cpf" or "cnpj"
		body["devedor"] = dev
	}
	// Client-defined txid -> PUT /v2/cob/:txid (idempotent).
	raw, err := c.efi.CreateCharge(in.Txid, body)
	if err != nil {
		return cobOutput{}, fmt.Errorf("efi: CreateCharge: %w", err)
	}
	out, err := parseCob(raw, in.Txid)
	if err != nil {
		return cobOutput{}, err
	}
	// The QR code image is only available via /v2/loc/:id/qrcode, not on the
	// cob resource itself. Best-effort: don't fail charge creation if it's
	// unavailable.
	if out.LocationID != "" && out.LocationID != "0" {
		if qrRaw, qrErr := c.efi.PixGenerateQRCode(out.LocationID); qrErr == nil {
			out.QRCodeImage = parseQRCodeImage(qrRaw)
		}
	}
	return out, nil
}

func (c *sdkClient) GetCob(_ context.Context, txid string) (cobOutput, error) {
	raw, err := c.efi.DetailCharge(txid)
	if err != nil {
		return cobOutput{}, fmt.Errorf("efi: DetailCharge: %w", err)
	}
	return parseCob(raw, txid)
}

// parseCob normalises the cob resource (POST/PUT/GET /v2/cob) JSON into cobOutput.
func parseCob(raw string, txid string) (cobOutput, error) {
	var r struct {
		Txid   string `json:"txid"`
		Status string `json:"status"`
		Loc    struct {
			ID int `json:"id"`
		} `json:"loc"`
		PixCopiaECola string `json:"pixCopiaECola"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return cobOutput{}, fmt.Errorf("efi: parse cob response: %w", err)
	}
	return cobOutput{
		Txid:       firstNonEmpty(r.Txid, txid),
		Status:     r.Status,
		LocationID: fmt.Sprint(r.Loc.ID),
		PixPayload: r.PixCopiaECola,
	}, nil
}

// parseQRCodeImage extracts the base64 PNG from a /v2/loc/:id/qrcode response.
func parseQRCodeImage(raw string) string {
	var r struct {
		ImagemQrcode string `json:"imagemQrcode"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return ""
	}
	return r.ImagemQrcode
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
