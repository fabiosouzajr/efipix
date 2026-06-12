package secrets

import "context"

type ProviderCreds struct {
	ClientID     string
	ClientSecret string
	CertPEM      []byte
	KeyPEM       []byte
	Sandbox      bool
}

type SecretProvider interface {
	ProviderCredentials(ctx context.Context, paymentProviderID string) (*ProviderCreds, error)
}
