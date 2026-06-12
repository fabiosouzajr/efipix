package domain

type Tenant struct {
	ID     string
	Name   string
	Status string
}

type PaymentProvider struct {
	ID            string
	TenantID      string
	Provider      string
	AccountLabel  string
	Status        string
	IsDefault     bool
	WebhookConfig []byte
}

type PixKey struct {
	ID                string
	TenantID          string
	PaymentProviderID string
	Key               string
	KeyType           string
}
