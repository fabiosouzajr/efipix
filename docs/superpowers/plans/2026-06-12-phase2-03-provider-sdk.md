# Phase 2 · File 03 — Provider Port & EFí CobV Adapter

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-12-phase2-00-overview.md) first (modalidade mapping table + percent representation). Depends on File 01.

**Scope:** Add `CreateDueDateCharge` to the `PixProvider` port, implement it on `EfiProvider`, and add the `CreateCobV` adapter that builds the EFí CobV body and calls the SDK's `CreateDueCharge(txid, body)` → `PUT /v2/cobv/:txid`. Also record the confirmed EFí CobV body shape in `docs/efi-sdk-review.md`.

**Files:**
- Modify: `internal/provider/provider.go` (DTOs + `CreateDueDateCharge` on `PixProvider`)
- Modify: `internal/provider/efi/client.go` (`cobvInput`, `efiClient.CreateCobV`)
- Modify: `internal/provider/efi/efi.go` (`EfiProvider.CreateDueDateCharge`)
- Modify: `internal/provider/efi/sdkclient.go` (`efiSDKClient.CreateDueCharge`, `sdkClient.CreateCobV`)
- Modify: `internal/provider/efi/efi_test.go` (fake must implement `CreateCobV`; map-fields test)
- Modify: `docs/efi-sdk-review.md` (record CobV body shape + modalidade codes)

---

### Task 1: Port DTOs + `EfiProvider.CreateDueDateCharge` (adapter layer, fake client)

**Files:**
- Modify: `internal/provider/provider.go`
- Modify: `internal/provider/efi/client.go`
- Modify: `internal/provider/efi/efi.go`
- Test: `internal/provider/efi/efi_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/provider/efi/efi_test.go`, extend the existing `fakeClient` with a `CreateCobV` method and a captured input, then add a mapping test. Add these to the file:

```go
// add field to fakeClient:  createdV cobvInput
func (f *fakeClient) CreateCobV(_ context.Context, in cobvInput) (cobOutput, error) {
	f.createdV = in
	f.calls++
	return cobOutput{Txid: in.Txid, Status: "ATIVA", LocationID: "loc9",
		QRCodeImage: "base64png", PixPayload: "000201..."}, nil
}

func TestCreateDueDateChargeMapsFields(t *testing.T) {
	fc := &fakeClient{}
	p, _ := newProvider(t, fc)
	res, err := p.CreateDueDateCharge(context.Background(), &provider.DueDateChargeInput{
		Txid: "txv1", PaymentProviderID: "prov-1", Amount: money.Centavos(10000),
		PixKey: "k@e.com", Description: "d", DueDate: "2026-12-31", ValidityAfterDays: 30,
		Fine:      &provider.FineInput{Mode: "percent", Value: "2.00"},
		Interest:  &provider.InterestInput{Mode: "monthly_percent", Value: "1.00"},
		Discount:  &provider.DiscountInput{Mode: "fixed", Entries: []provider.DiscountEntryInput{{Date: "2026-12-20", Value: "5.00"}}},
		Abatement: "1.50",
	})
	require.NoError(t, err)
	require.Equal(t, "txv1", res.Txid)
	require.Equal(t, "ATIVA", res.Status)
	require.Equal(t, "loc9", res.LocationID)
	require.Equal(t, "10.00", fc.createdV.Amount)
	require.Equal(t, "2026-12-31", fc.createdV.DueDate)
	require.Equal(t, "percent", fc.createdV.Fine.Mode)
	require.Equal(t, "monthly_percent", fc.createdV.Interest.Mode)
	require.Equal(t, "1.50", fc.createdV.Abatement)
	require.Len(t, fc.createdV.Discount.Entries, 1)
	require.Equal(t, "5.00", fc.createdV.Discount.Entries[0].Value)
}
```

Update the existing `fakeClient` struct literal: add the `createdV cobvInput` field declaration to the struct.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/...`
Expected: FAIL — `provider.DueDateChargeInput`/`FineInput`/etc., `cobvInput`, `CreateCobV`, and `EfiProvider.CreateDueDateCharge` are undefined; the fake doesn't satisfy `efiClient`.

- [ ] **Step 3: Write minimal implementation**

In `internal/provider/provider.go`, add the DTOs and extend the interface:

```go
type FineInput struct {
	Mode  string // "fixed" | "percent"
	Value string // decimal "10.00" (fixed) or "2.00" (percent)
}

type InterestInput struct {
	Mode  string // "daily_percent" | "monthly_percent"
	Value string // decimal percent "2.00"
}

type DiscountEntryInput struct {
	Date  string // "2006-01-02"
	Value string // decimal "5.00" (fixed) or "2.00" (percent)
}

type DiscountInput struct {
	Mode    string // "fixed" | "percent"
	Entries []DiscountEntryInput
}

type DueDateChargeInput struct {
	Txid              string
	PaymentProviderID string
	Amount            money.Centavos
	PixKey            string
	Description       string
	PayerDoc          string
	PayerDocType      string
	PayerName         string
	DueDate           string // "2006-01-02"
	ValidityAfterDays int
	Fine              *FineInput
	Interest          *InterestInput
	Discount          *DiscountInput
	Abatement         string // decimal "10.00", "" if none
}
```

And add the method to the `PixProvider` interface:

```go
type PixProvider interface {
	CreateImmediateCharge(ctx context.Context, in *ImmediateChargeInput) (*ChargeResult, error)
	CreateDueDateCharge(ctx context.Context, in *DueDateChargeInput) (*ChargeResult, error)
	GetCharge(ctx context.Context, paymentProviderID, txid string) (*ChargeResult, error)
}
```

In `internal/provider/efi/client.go`, add the adapter-internal `cobvInput` shape and the `CreateCobV` method on `efiClient`:

```go
type cobvFine struct{ Mode, Value string }
type cobvInterest struct{ Mode, Value string }
type cobvDiscountEntry struct{ Date, Value string }
type cobvDiscount struct {
	Mode    string
	Entries []cobvDiscountEntry
}

type cobvInput struct {
	Txid              string
	PixKey            string
	Amount            string // decimal "10.50"
	Description       string
	PayerDoc          string
	PayerDocType      string
	PayerName         string
	DueDate           string
	ValidityAfterDays int
	Fine              *cobvFine
	Interest          *cobvInterest
	Discount          *cobvDiscount
	Abatement         string
}
```

And add `CreateCobV` to the `efiClient` interface (alongside `CreateCob`/`GetCob`):

```go
type efiClient interface {
	CreateCob(ctx context.Context, in cobInput) (cobOutput, error)
	CreateCobV(ctx context.Context, in cobvInput) (cobOutput, error)
	GetCob(ctx context.Context, txid string) (cobOutput, error)
}
```

In `internal/provider/efi/efi.go`, add the provider method (maps the public DTO to `cobvInput`):

```go
func (p *EfiProvider) CreateDueDateCharge(ctx context.Context, in *provider.DueDateChargeInput) (*provider.ChargeResult, error) {
	c, err := p.clientFor(ctx, in.PaymentProviderID)
	if err != nil {
		return nil, err
	}
	cin := cobvInput{
		Txid: in.Txid, PixKey: in.PixKey, Amount: in.Amount.String(),
		Description: in.Description, PayerDoc: in.PayerDoc, PayerDocType: in.PayerDocType,
		PayerName: in.PayerName, DueDate: in.DueDate, ValidityAfterDays: in.ValidityAfterDays,
		Abatement: in.Abatement,
	}
	if in.Fine != nil {
		cin.Fine = &cobvFine{Mode: in.Fine.Mode, Value: in.Fine.Value}
	}
	if in.Interest != nil {
		cin.Interest = &cobvInterest{Mode: in.Interest.Mode, Value: in.Interest.Value}
	}
	if in.Discount != nil {
		d := &cobvDiscount{Mode: in.Discount.Mode}
		for _, e := range in.Discount.Entries {
			d.Entries = append(d.Entries, cobvDiscountEntry{Date: e.Date, Value: e.Value})
		}
		cin.Discount = d
	}
	out, err := c.CreateCobV(ctx, cin)
	if err != nil {
		return nil, apperrs.Wrap(apperrs.KindProvider, "create cobv", err)
	}
	return toResult(out), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/...`
Expected: PASS (new mapping test + existing `TestCreateImmediateChargeMapsFields` etc.). Note `sdkClient` does not yet implement `CreateCobV`, so the **real** SDK client won't compile against `efiClient` — Task 2 adds it. If the build complains here, it's the `sdkClient` type; complete Task 2 in the same session before the package builds fully. To keep this task green in isolation, implement Task 2's `sdkClient.CreateCobV` immediately after Step 3 here, or run the test with only the efi package's `_test.go` (the fake) which does compile. **Recommended:** treat Tasks 1 & 2 as one commit if your runner builds the whole package per step.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/provider.go internal/provider/efi/client.go internal/provider/efi/efi.go internal/provider/efi/efi_test.go
git commit -m "feat(provider): CreateDueDateCharge port and EfiProvider mapping"
```

---

### Task 2: `sdkClient.CreateCobV` — build the EFí CobV body, call `CreateDueCharge`

**Files:**
- Modify: `internal/provider/efi/sdkclient.go`
- Test: `internal/provider/efi/sdkclient_test.go` (new — fake `efiSDKClient` capturing the body)
- Modify: `docs/efi-sdk-review.md`

- [ ] **Step 1: Confirm the EFí CobV body shape against the SDK source, record it**

Inspect the vendored SDK (as in Phase 1) to confirm the CobV endpoint + body field names:

```bash
SDK="$(go env GOPATH)/pkg/mod/github.com/efipay/sdk-go-apis-efi@v1.4.0"
grep -rn "CreateDueCharge\|cobv" "$SDK/src/efipay/pix/endpoints_pix.go"
ls "$SDK"/examples 2>/dev/null && grep -rln "cobv\|dataDeVencimento\|descontoDataFixa\|valorPerc" "$SDK" | head
```

Confirm: method `CreateDueCharge(txid string, body map[string]interface{}) (string, error)` → `PUT /v2/cobv/:txid`; body nests `calendario.dataDeVencimento`, `calendario.validadeAposVencimento`, `valor.original`, and optional `valor.multa`/`valor.juros`/`valor.desconto`/`valor.abatimento` each as `{modalidade, valorPerc}` (desconto additionally carries `descontoDataFixa: [{data, valorPerc}]`). Record the confirmed shape and the modalidade codes (from 00-overview's table) in `docs/efi-sdk-review.md` under a new `## Confirmed for Phase 2 (CobV / due charge)` section. **If the SDK examples disagree with the overview's `valorPerc`/modalidade assumptions, update `sdkclient.go` AND the overview table to match the source, and note the discrepancy in the SDK review.**

- [ ] **Step 2: Write the failing test**

Create `internal/provider/efi/sdkclient_test.go`:

```go
package efi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeSDK struct {
	body map[string]interface{}
	txid string
}

func (f *fakeSDK) CreateCharge(txid string, body map[string]interface{}) (string, error) {
	return "", nil
}
func (f *fakeSDK) DetailCharge(string) (string, error)      { return "", nil }
func (f *fakeSDK) PixGenerateQRCode(string) (string, error) { return `{"imagemQrcode":""}`, nil }
func (f *fakeSDK) CreateDueCharge(txid string, body map[string]interface{}) (string, error) {
	f.txid, f.body = txid, body
	return `{"txid":"` + txid + `","status":"ATIVA","loc":{"id":7},"pixCopiaECola":"000201..."}`, nil
}

func TestCreateCobVBuildsBody(t *testing.T) {
	f := &fakeSDK{}
	c := &sdkClient{efi: f}
	out, err := c.CreateCobV(context.Background(), cobvInput{
		Txid: "txv1", PixKey: "k@e.com", Amount: "100.00", DueDate: "2026-12-31",
		ValidityAfterDays: 30,
		Fine:      &cobvFine{Mode: "percent", Value: "2.00"},
		Interest:  &cobvInterest{Mode: "monthly_percent", Value: "1.00"},
		Discount:  &cobvDiscount{Mode: "fixed", Entries: []cobvDiscountEntry{{Date: "2026-12-20", Value: "5.00"}}},
		Abatement: "1.50",
		PayerDoc:  "12345678909", PayerDocType: "cpf", PayerName: "Ana",
	})
	require.NoError(t, err)
	require.Equal(t, "txv1", out.Txid)
	require.Equal(t, "ATIVA", out.Status)
	require.Equal(t, "7", out.LocationID)

	cal := f.body["calendario"].(map[string]interface{})
	require.Equal(t, "2026-12-31", cal["dataDeVencimento"])
	require.Equal(t, 30, cal["validadeAposVencimento"])

	valor := f.body["valor"].(map[string]interface{})
	require.Equal(t, "100.00", valor["original"])
	require.Equal(t, map[string]interface{}{"modalidade": 2, "valorPerc": "2.00"}, valor["multa"])
	require.Equal(t, map[string]interface{}{"modalidade": 5, "valorPerc": "1.00"}, valor["juros"])
	require.Equal(t, map[string]interface{}{"modalidade": 1, "valorPerc": "1.50"}, valor["abatimento"])

	desc := valor["desconto"].(map[string]interface{})
	require.Equal(t, 1, desc["modalidade"])
	entries := desc["descontoDataFixa"].([]map[string]interface{})
	require.Equal(t, "2026-12-20", entries[0]["data"])
	require.Equal(t, "5.00", entries[0]["valorPerc"])

	dev := f.body["devedor"].(map[string]interface{})
	require.Equal(t, "Ana", dev["nome"])
	require.Equal(t, "12345678909", dev["cpf"])
	require.Equal(t, "k@e.com", f.body["chave"])
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/provider/efi/ -run TestCreateCobVBuildsBody -v`
Expected: FAIL — `sdkClient.CreateCobV` and `efiSDKClient.CreateDueCharge` are undefined.

- [ ] **Step 4: Write minimal implementation**

In `internal/provider/efi/sdkclient.go`, add `CreateDueCharge` to the `efiSDKClient` interface:

```go
type efiSDKClient interface {
	CreateCharge(txid string, body map[string]interface{}) (string, error)
	CreateDueCharge(txid string, body map[string]interface{}) (string, error)
	DetailCharge(txid string) (string, error)
	PixGenerateQRCode(id string) (string, error)
}
```

The real return value of `efipix.NewEfiPay(cfg)` already exposes `CreateDueCharge` (confirmed in Step 1 / cerebrum `endpoints_pix.go`), so no wiring change is needed in `SDKFactory`.

Then add the `CreateCobV` method + modalidade mapping helpers:

```go
func (c *sdkClient) CreateCobV(_ context.Context, in cobvInput) (cobOutput, error) {
	valor := map[string]interface{}{"original": in.Amount}
	if in.Fine != nil {
		valor["multa"] = map[string]interface{}{"modalidade": fineModalidade(in.Fine.Mode), "valorPerc": in.Fine.Value}
	}
	if in.Interest != nil {
		valor["juros"] = map[string]interface{}{"modalidade": interestModalidade(in.Interest.Mode), "valorPerc": in.Interest.Value}
	}
	if in.Discount != nil && len(in.Discount.Entries) > 0 {
		entries := make([]map[string]interface{}, 0, len(in.Discount.Entries))
		for _, e := range in.Discount.Entries {
			entries = append(entries, map[string]interface{}{"data": e.Date, "valorPerc": e.Value})
		}
		valor["desconto"] = map[string]interface{}{
			"modalidade":       discountModalidade(in.Discount.Mode),
			"descontoDataFixa": entries,
		}
	}
	if in.Abatement != "" {
		valor["abatimento"] = map[string]interface{}{"modalidade": 1, "valorPerc": in.Abatement}
	}

	body := map[string]interface{}{
		"calendario": map[string]interface{}{
			"dataDeVencimento":       in.DueDate,
			"validadeAposVencimento": in.ValidityAfterDays,
		},
		"valor": valor,
		"chave": in.PixKey,
	}
	if in.Description != "" {
		body["solicitacaoPagador"] = in.Description
	}
	if in.PayerDoc != "" {
		dev := map[string]interface{}{"nome": in.PayerName}
		dev[in.PayerDocType] = in.PayerDoc // "cpf" or "cnpj"
		body["devedor"] = dev
	}

	// Client-defined txid -> PUT /v2/cobv/:txid (idempotent).
	raw, err := c.efi.CreateDueCharge(in.Txid, body)
	if err != nil {
		return cobOutput{}, fmt.Errorf("efi: CreateDueCharge: %w", err)
	}
	out, err := parseCob(raw, in.Txid) // cobv response has the same txid/status/loc/pixCopiaECola shape
	if err != nil {
		return cobOutput{}, err
	}
	if out.LocationID != "" && out.LocationID != "0" {
		if qrRaw, qrErr := c.efi.PixGenerateQRCode(out.LocationID); qrErr == nil {
			out.QRCodeImage = parseQRCodeImage(qrRaw)
		}
	}
	return out, nil
}

// EFí modalidade codes — see 00-overview's mapping table / docs/efi-sdk-review.md.
func fineModalidade(mode string) int {
	if mode == "fixed" {
		return 1
	}
	return 2 // percent
}

func interestModalidade(mode string) int {
	if mode == "daily_percent" {
		return 2
	}
	return 5 // monthly_percent (percentual ao mês, dias corridos)
}

func discountModalidade(mode string) int {
	if mode == "fixed" {
		return 1
	}
	return 2 // percent
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/provider/...`
Expected: PASS (new `TestCreateCobVBuildsBody`, the Task-1 mapping test, and all existing provider tests).

- [ ] **Step 6: Commit**

```bash
git add internal/provider/efi/sdkclient.go internal/provider/efi/sdkclient_test.go docs/efi-sdk-review.md
git commit -m "feat(provider): EFí CobV body builder via CreateDueCharge and SDK review notes"
```

Append to `.wolf/memory.md`; add `sdkclient_test.go` to `.wolf/anatomy.md`; update the `docs/efi-sdk-review.md` anatomy description to mention Phase 2 CobV.

---

## File 03 done — checkpoint

```bash
export PATH="$PATH:/home/fj/go/bin"
go vet ./internal/provider/...
go test -race ./internal/provider/...
golangci-lint run ./internal/provider/...
```

Expected: all green. Proceed to [04-usecase-api](2026-06-12-phase2-04-usecase-api.md).
