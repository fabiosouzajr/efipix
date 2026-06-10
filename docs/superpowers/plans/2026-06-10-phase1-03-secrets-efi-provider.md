# Phase 1 · File 03 — Secrets, PixProvider Port & EFí Adapter

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-10-phase1-00-overview.md) first. Depends on [01-foundation](2026-06-10-phase1-01-foundation.md) and [02-tenant-provider](2026-06-10-phase1-02-tenant-provider.md).

**Goal:** Resolve per-account EFí credentials from a `SecretProvider`; define the provider-agnostic `PixProvider` port; implement `EfiProvider` with a **per-`payment_provider_id` SDK client pool** that creates and fetches immediate charges and maps EFí structs to internal DTOs. The SDK is isolated entirely in `internal/provider/efi`.

**Design seam:** `EfiProvider` depends on a small internal `efiClient` interface, not on the SDK directly. The mapping/pooling logic is unit-tested with a fake `efiClient`; the real SDK binding is a separate, clearly-marked task whose exact method names are confirmed during the SDK capability review (spec §3 / §16).

---

### Task 1: SecretProvider (env-JSON implementation)

**Files:**
- Create: `internal/platform/secrets/secrets.go`
- Create: `internal/platform/secrets/env.go`
- Test: `internal/platform/secrets/env_test.go`

> Env layout: a single env var `EFI_CREDENTIALS` holds a JSON object keyed by `payment_provider_id`. Each entry has `client_id`, `client_secret`, `cert_pem_path`, `key_pem_path`, `sandbox`. The env impl loads PEM bytes from the referenced files. Vault/AWS-SM impls (same interface) are Phase 6.

- [ ] **Step 1: Define the interface + type** (`internal/platform/secrets/secrets.go`)

```go
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
```

- [ ] **Step 2: Write the failing test** (`internal/platform/secrets/env_test.go`)

```go
package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvProviderCredentials(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("CERTDATA"), 0o600))
	require.NoError(t, os.WriteFile(keyPath, []byte("KEYDATA"), 0o600))

	js := `{"prov-1":{"client_id":"cid","client_secret":"sec","cert_pem_path":"` +
		certPath + `","key_pem_path":"` + keyPath + `","sandbox":true}}`
	t.Setenv("EFI_CREDENTIALS", js)

	sp, err := NewEnv()
	require.NoError(t, err)
	c, err := sp.ProviderCredentials(context.Background(), "prov-1")
	require.NoError(t, err)
	require.Equal(t, "cid", c.ClientID)
	require.Equal(t, "sec", c.ClientSecret)
	require.Equal(t, []byte("CERTDATA"), c.CertPEM)
	require.Equal(t, []byte("KEYDATA"), c.KeyPEM)
	require.True(t, c.Sandbox)

	_, err = sp.ProviderCredentials(context.Background(), "missing")
	require.Error(t, err)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/platform/secrets/`
Expected: FAIL (NewEnv undefined).

- [ ] **Step 4: Implement the env provider** (`internal/platform/secrets/env.go`)

```go
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/platform/secrets/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/secrets/
git commit -m "feat(secrets): SecretProvider interface and env-json implementation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: P12 → PEM conversion

**Files:**
- Create: `internal/platform/secrets/p12.go`
- Test: `internal/platform/secrets/p12_test.go`

- [ ] **Step 1: Add dependency**

```bash
go get software.sslmate.com/src/go-pkcs12@latest
go mod tidy
```

- [ ] **Step 2: Write the failing test** (round-trips: build a self-signed key+cert → encode P12 → convert back to PEM → parse)

```go
package secrets

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

func TestP12ToPEM(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pfx, err := pkcs12.Modern.Encode(key, cert, nil, "pw")
	require.NoError(t, err)

	certPEM, keyPEM, err := P12ToPEM(pfx, "pw")
	require.NoError(t, err)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	require.Equal(t, "CERTIFICATE", block.Type)
	kblock, _ := pem.Decode(keyPEM)
	require.NotNil(t, kblock)

	_, err = P12ToPEM([]byte("not-a-p12"), "pw")
	require.Error(t, err)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/platform/secrets/ -run TestP12ToPEM`
Expected: FAIL (P12ToPEM undefined).

- [ ] **Step 4: Implement** (`internal/platform/secrets/p12.go`)

```go
package secrets

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// P12ToPEM decodes a PKCS#12 bundle into PEM-encoded certificate and private key.
func P12ToPEM(p12 []byte, password string) (certPEM, keyPEM []byte, err error) {
	key, cert, err := pkcs12.Decode(p12, password)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: decode p12: %w", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("secrets: marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	return certPEM, keyPEM, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/platform/secrets/ -run TestP12ToPEM`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/secrets/p12.go internal/platform/secrets/p12_test.go go.mod go.sum
git commit -m "feat(secrets): pkcs12 to pem conversion

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: PixProvider port + DTOs

**Files:**
- Create: `internal/provider/provider.go`

> Provider-agnostic contract (per locked signatures). No behaviour → no unit test; exercised by Task 4's `EfiProvider` tests.

- [ ] **Step 1: Implement**

```go
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
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./internal/provider/`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/provider/provider.go
git commit -m "feat(provider): PixProvider port and DTOs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: EfiProvider — client pool + mapping (unit-tested)

**Files:**
- Create: `internal/provider/efi/client.go` (internal `efiClient` seam + types)
- Create: `internal/provider/efi/efi.go` (`EfiProvider`)
- Test: `internal/provider/efi/efi_test.go`

- [ ] **Step 1: Define the internal seam** (`internal/provider/efi/client.go`)

```go
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
```

- [ ] **Step 2: Write the failing test** (fake client + fake secrets; asserts pooling + mapping)

```go
package efi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/platform/secrets"
	"github.com/efipix/pix/internal/provider"
)

type fakeClient struct {
	created cobInput
	calls   int
}

func (f *fakeClient) CreateCob(_ context.Context, in cobInput) (cobOutput, error) {
	f.created = in
	f.calls++
	return cobOutput{Txid: in.Txid, Status: "ATIVA", LocationID: "loc1",
		QRCodeImage: "base64png", PixPayload: "000201..."}, nil
}
func (f *fakeClient) GetCob(_ context.Context, txid string) (cobOutput, error) {
	return cobOutput{Txid: txid, Status: "CONCLUIDA"}, nil
}

type fakeSecrets struct{ calls int }

func (f *fakeSecrets) ProviderCredentials(_ context.Context, _ string) (*secrets.ProviderCreds, error) {
	f.calls++
	return &secrets.ProviderCreds{ClientID: "c", ClientSecret: "s", CertPEM: []byte("x"), KeyPEM: []byte("y")}, nil
}

func newProvider(t *testing.T, fc *fakeClient) (*EfiProvider, *fakeSecrets) {
	sp := &fakeSecrets{}
	p := New(sp, func(*secrets.ProviderCreds) (efiClient, error) { return fc, nil })
	return p, sp
}

func TestCreateImmediateChargeMapsFields(t *testing.T) {
	fc := &fakeClient{}
	p, _ := newProvider(t, fc)
	res, err := p.CreateImmediateCharge(context.Background(), &provider.ImmediateChargeInput{
		Txid: "tx1", PaymentProviderID: "prov-1", Amount: money.Centavos(1050),
		PixKey: "k@e.com", Description: "d", ExpirationSeconds: 3600,
	})
	require.NoError(t, err)
	require.Equal(t, "10.50", fc.created.Amount)
	require.Equal(t, "tx1", res.Txid)
	require.Equal(t, "ATIVA", res.Status)
	require.Equal(t, "loc1", res.LocationID)
	require.Equal(t, "base64png", res.QRCodeImage)
}

func TestClientPoolCachesPerProvider(t *testing.T) {
	fc := &fakeClient{}
	p, sp := newProvider(t, fc)
	in := &provider.ImmediateChargeInput{Txid: "a", PaymentProviderID: "prov-1", Amount: 100}
	_, _ = p.CreateImmediateCharge(context.Background(), in)
	_, _ = p.CreateImmediateCharge(context.Background(), in)
	require.Equal(t, 1, sp.calls, "credentials fetched once and client cached")
}

func TestGetCharge(t *testing.T) {
	fc := &fakeClient{}
	p, _ := newProvider(t, fc)
	res, err := p.GetCharge(context.Background(), "prov-1", "tx9")
	require.NoError(t, err)
	require.Equal(t, "tx9", res.Txid)
	require.Equal(t, "CONCLUIDA", res.Status)
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/provider/efi/`
Expected: FAIL (New / EfiProvider undefined).

- [ ] **Step 4: Implement `EfiProvider`** (`internal/provider/efi/efi.go`)

```go
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
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/provider/efi/`
Expected: PASS (3 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/provider/efi/client.go internal/provider/efi/efi.go internal/provider/efi/efi_test.go
git commit -m "feat(provider/efi): EfiProvider with per-account client pool and mapping

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Real SDK-backed efiClient + SDK review note

**Files:**
- Create: `internal/provider/efi/sdkclient.go`
- Create: `internal/provider/efi/sdkclient_homolog_test.go` (tagged `homolog`)
- Create: `docs/efi-sdk-review.md`
- Modify: `go.mod` (add the SDK)

> The exact SDK method names/signatures must be confirmed during this task (it IS the spec §3 capability review). The code below follows the documented `efipay/sdk-go-apis-efi` usage (credentials map + `pix.NewEfiPay`, method `PixCreateImmediateCharge`, `PixDetailCharge`); adjust to the installed version and record findings in `docs/efi-sdk-review.md`. The unit-tested seam (Task 4) means only this glue is provider-coupled.

- [ ] **Step 1: Add the SDK dependency**

```bash
go get github.com/efipay/sdk-go-apis-efi@latest
go mod tidy
```

- [ ] **Step 2: Implement the SDK client + factory** (`internal/provider/efi/sdkclient.go`)

```go
package efi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	efipix "github.com/efipay/sdk-go-apis-efi/src/efipay/pix"

	"github.com/efipix/pix/internal/platform/secrets"
)

// SDKFactory builds a real efiClient from credentials. Pass this to efi.New in production wiring.
func SDKFactory(creds *secrets.ProviderCreds) (efiClient, error) {
	// The SDK loads the cert from a file path; write the PEM (cert+key concatenated) to a temp file.
	f, err := os.CreateTemp("", "efi-*.pem")
	if err != nil {
		return nil, fmt.Errorf("efi: temp cert: %w", err)
	}
	if _, err := f.Write(append(append([]byte{}, creds.CertPEM...), creds.KeyPEM...)); err != nil {
		return nil, fmt.Errorf("efi: write cert: %w", err)
	}
	_ = f.Close()
	cfg := map[string]interface{}{
		"client_id":     creds.ClientID,
		"client_secret": creds.ClientSecret,
		"sandbox":       creds.Sandbox,
		"certificate":   f.Name(),
		"timeout":       30,
	}
	return &sdkClient{efi: efipix.NewEfiPay(cfg), certPath: f.Name()}, nil
}

type sdkClient struct {
	efi      *efipix.Efipay
	certPath string
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
	// Client-defined txid → PUT /v2/cob/:txid (idempotent). Confirm SDK method name.
	raw, err := c.efi.PixCreateCharge(in.Txid, body)
	if err != nil {
		return cobOutput{}, fmt.Errorf("efi: PixCreateCharge: %w", err)
	}
	return parseCob(raw, in.Txid)
}

func (c *sdkClient) GetCob(_ context.Context, txid string) (cobOutput, error) {
	raw, err := c.efi.PixDetailCharge(txid)
	if err != nil {
		return cobOutput{}, fmt.Errorf("efi: PixDetailCharge: %w", err)
	}
	return parseCob(raw, txid)
}

// parseCob normalises the SDK's JSON response (string) into cobOutput.
// Confirm field paths against the installed SDK + EFí docs during the review.
func parseCob(raw string, txid string) (cobOutput, error) {
	var r struct {
		Txid     string `json:"txid"`
		Status   string `json:"status"`
		Loc      struct {
			ID int `json:"id"`
		} `json:"loc"`
		PixCopiaECola string `json:"pixCopiaECola"`
		QRCode        string `json:"qrcode"`
		ImagemQrcode  string `json:"imagemQrcode"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return cobOutput{}, fmt.Errorf("efi: parse cob: %w", err)
	}
	out := cobOutput{
		Txid: firstNonEmpty(r.Txid, txid), Status: r.Status,
		LocationID: fmt.Sprint(r.Loc.ID), PixPayload: firstNonEmpty(r.PixCopiaECola, r.QRCode),
		QRCodeImage: r.ImagemQrcode,
	}
	return out, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
```

- [ ] **Step 3: Write the homologation test** (`internal/provider/efi/sdkclient_homolog_test.go`) — skipped unless real creds present

```go
//go:build homolog

package efi

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/platform/secrets"
	"github.com/efipix/pix/internal/provider"
)

// Run with: EFI_CREDENTIALS=... go test -tags=homolog ./internal/provider/efi/
func TestHomologCreateAndGet(t *testing.T) {
	if os.Getenv("EFI_CREDENTIALS") == "" {
		t.Skip("no EFI_CREDENTIALS")
	}
	sp, err := secrets.NewEnv()
	require.NoError(t, err)
	p := New(sp, SDKFactory)

	providerID := os.Getenv("EFI_TEST_PROVIDER_ID")
	pixKey := os.Getenv("EFI_TEST_PIX_KEY")
	txid := "PIX" + uuid.NewString()
	txid = txid[:35]

	res, err := p.CreateImmediateCharge(context.Background(), &provider.ImmediateChargeInput{
		Txid: txid, PaymentProviderID: providerID, Amount: money.Centavos(100),
		PixKey: pixKey, ExpirationSeconds: 3600,
	})
	require.NoError(t, err)
	require.Equal(t, "ATIVA", res.Status)
	require.NotEmpty(t, res.PixPayload)

	got, err := p.GetCharge(context.Background(), providerID, txid)
	require.NoError(t, err)
	require.Equal(t, txid, got.Txid)
}
```

- [ ] **Step 4: Verify compilation (unit scope stays green; homolog tag compiles)**

Run:
```bash
go build ./internal/provider/efi/
go vet -tags=homolog ./internal/provider/efi/
go test ./internal/provider/efi/   # unit tests still pass
```
Expected: builds; unit tests PASS. **If the SDK import path or method names differ from the installed version, fix them now and record the actual API in `docs/efi-sdk-review.md`.**

- [ ] **Step 5: Write the SDK review note** (`docs/efi-sdk-review.md`)

```markdown
# EFí Go SDK Capability Review (Phase 1)

SDK: `github.com/efipay/sdk-go-apis-efi`

## Confirmed for Phase 1 (immediate charge)
- Create immediate charge (client-defined txid): method `<confirm: PixCreateCharge>` → maps to `PUT /v2/cob/:txid`.
- Detail charge: method `<confirm: PixDetailCharge>` → `GET /v2/cob/:txid`.
- Credentials: map with `client_id`, `client_secret`, `sandbox`, `certificate` (PEM file path), `timeout`.
- Certificate is bound at client construction (per-instance) → we pool one client per payment_provider_id.
- Response shape fields observed: `txid`, `status`, `loc.id`, `pixCopiaECola`, `imagemQrcode`.

## Gaps / notes
- <record any method that returns a different type or requires a different body shape>
- <record cobv / devolução / webhook-config method names for Phases 2–3>
```

Fill the `<confirm:…>` placeholders with the actual installed method names while doing Step 4 (this is the deliverable, not a leftover TODO).

- [ ] **Step 6: Commit**

```bash
git add internal/provider/efi/sdkclient.go internal/provider/efi/sdkclient_homolog_test.go docs/efi-sdk-review.md go.mod go.sum
git commit -m "feat(provider/efi): real SDK-backed client + homolog test + capability review

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## File 03 exit criteria

- [ ] `go test ./internal/platform/secrets/ ./internal/provider/...` green (unit).
- [ ] `EfiProvider` caches one client per `payment_provider_id` and maps centavos→"10.50".
- [ ] P12→PEM round-trips.
- [ ] `docs/efi-sdk-review.md` records the confirmed SDK method names (no remaining `<confirm:…>`).
- [ ] Homolog test passes when `EFI_CREDENTIALS` + test env are set (manual / nightly), skipped otherwise.

Proceed to [04-charge-aggregate](2026-06-10-phase1-04-charge-aggregate.md).
