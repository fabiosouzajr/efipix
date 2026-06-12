package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

type envEntry struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	CertPEMPath  string `json:"cert_pem_path"`
	KeyPEMPath   string `json:"key_pem_path"`
	Sandbox      bool   `json:"sandbox"`
}

type EnvProvider struct{ entries map[string]envEntry }

func NewEnv() (*EnvProvider, error) {
	raw := os.Getenv("EFI_CREDENTIALS")
	if raw == "" {
		return &EnvProvider{entries: map[string]envEntry{}}, nil
	}
	var m map[string]envEntry
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("secrets: parse EFI_CREDENTIALS: %w", err)
	}
	return &EnvProvider{entries: m}, nil
}

func (p *EnvProvider) ProviderCredentials(_ context.Context, id string) (*ProviderCreds, error) {
	e, ok := p.entries[id]
	if !ok {
		return nil, fmt.Errorf("secrets: no credentials for provider %s", id)
	}
	cert, err := os.ReadFile(e.CertPEMPath)
	if err != nil {
		return nil, fmt.Errorf("secrets: read cert: %w", err)
	}
	key, err := os.ReadFile(e.KeyPEMPath)
	if err != nil {
		return nil, fmt.Errorf("secrets: read key: %w", err)
	}
	return &ProviderCreds{
		ClientID: e.ClientID, ClientSecret: e.ClientSecret,
		CertPEM: cert, KeyPEM: key, Sandbox: e.Sandbox,
	}, nil
}
