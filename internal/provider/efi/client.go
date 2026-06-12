package efi

import "context"

// cobInput/cobOutput are the EFí-adapter-internal shapes. They never leave this package.
type cobInput struct {
	Txid              string
	PixKey            string
	Amount            string // decimal "10.50"
	Description       string
	ExpirationSeconds int
	PayerDoc          string
	PayerDocType      string
	PayerName         string
}

type cobOutput struct {
	Txid        string
	Status      string
	LocationID  string
	QRCodeImage string
	PixPayload  string
}

// efiClient is the minimal EFí surface EfiProvider needs. The real impl (sdkclient.go)
// wraps github.com/efipay/sdk-go-apis-efi; tests use a fake.
type efiClient interface {
	CreateCob(ctx context.Context, in cobInput) (cobOutput, error)
	GetCob(ctx context.Context, txid string) (cobOutput, error)
}
