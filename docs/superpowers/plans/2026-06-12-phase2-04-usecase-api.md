# Phase 2 · File 04 — Use Case, CobV API, Wiring & End-to-End

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-12-phase2-00-overview.md) first. Depends on Files 01–03.

**Scope:** The `CreateDueDateCharge` use case (persist-first two-phase, reusing `ChargeRepository`), the CobV request DTO + validation, the `amount_due` breakdown on responses, `cmd/server` wiring, and the rewritten end-to-end test ([ADR-0006](../../adr/0006-post-charges-cobv-only.md): `POST /charges` is CobV-only).

**Files:**
- Create: `internal/charge/app/create_duedate.go`
- Modify: `internal/charge/app/create_test.go` (add `CreateDueDateCharge` to `fakeProv`; new use-case tests)
- Modify: `internal/charge/api/dto.go` (CobV request + `amount_due` response + `toResponse`)
- Modify: `internal/charge/api/handler.go` (handler holds the cobv use case; `buildTerms` validation in `process`)
- Create: `internal/charge/api/dto_test.go` (white-box `buildTerms`/`toResponse` unit tests)
- Modify: `cmd/server/main.go` (wire `NewCreateDueDateCharge`)
- Rewrite: `internal/charge/api/e2e_test.go` (CobV flow)

**Heads-up:** File 03 added `CreateDueDateCharge` to the `provider.PixProvider` interface. Every fake implementing it must gain the method or its package won't compile. This file updates the two remaining fakes (`internal/charge/app/create_test.go` and `internal/charge/api/e2e_test.go`). After this file, `go test ./...` builds module-wide again.

---

### Task 1: `CreateDueDateCharge` use case

**Files:**
- Create: `internal/charge/app/create_duedate.go`
- Test: `internal/charge/app/create_test.go` (extend the existing fake + add tests)

- [ ] **Step 1: Write the failing test**

First make the app test package compile against the new interface: add a `CreateDueDateCharge` method to the existing `fakeProv` in `internal/charge/app/create_test.go`:

```go
func (f *fakeProv) CreateDueDateCharge(_ context.Context, in *provider.DueDateChargeInput) (*provider.ChargeResult, error) {
	if f.fail {
		return nil, errors.New("efi down")
	}
	return &provider.ChargeResult{Txid: in.Txid, Status: "ATIVA", LocationID: "locv",
		QRCodeImage: "img", PixPayload: "000201..."}, nil
}
```

Then append a new test file `internal/charge/app/create_duedate_test.go` (separate file, same `package app`, reuses `fakeRepo`/`fakeProv`):

```go
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/brdate"
	"github.com/efipix/pix/internal/platform/money"
)

func dueCmd() CreateDueDateChargeCmd {
	return CreateDueDateChargeCmd{
		TenantID: "t1", PaymentProviderID: "p1", PixKey: "k@e.com",
		Amount: money.Centavos(10000),
		Terms: domain.DueDateTerms{
			DueDate:           brdate.Date(2026, time.December, 31),
			ValidityAfterDays: 30,
			Fine:              &domain.Fine{Mode: domain.FinePercent, Value: 200},
			Discount: &domain.Discount{Mode: domain.DiscountFixed, Entries: []domain.DiscountEntry{
				{Date: brdate.Date(2026, time.December, 20), Value: 500},
			}},
		},
		Today: brdate.Date(2026, time.June, 1),
	}
}

func TestCreateDueDateSuccess(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateDueDateCharge(repo, &fakeProv{})
	c, err := uc.Execute(context.Background(), dueCmd())
	require.NoError(t, err)
	require.Equal(t, domain.KindCobV, c.Kind)
	require.Equal(t, domain.StatusActive, c.Status)
	require.NotNil(t, repo.created)
	require.NotNil(t, repo.created.Terms)
	require.Len(t, repo.savedOut, 1)
	require.Equal(t, "ChargeCreated", repo.savedOut[0].Type)
}

func TestCreateDueDateInvalidTerms(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateDueDateCharge(repo, &fakeProv{})
	c := dueCmd()
	c.Terms.DueDate = brdate.Date(2026, time.May, 1) // before Today (Jun 1)
	_, err := uc.Execute(context.Background(), c)
	require.Error(t, err)
	require.Nil(t, repo.created, "no persistence when the domain rejects the terms")
}

func TestCreateDueDateProviderFailureMarksFailed(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateDueDateCharge(repo, &fakeProv{fail: true})
	_, err := uc.Execute(context.Background(), dueCmd())
	require.Error(t, err)
	require.Equal(t, domain.StatusFailed, repo.saved.Status)
	require.Empty(t, repo.savedOut, "no outbox event on failure")
}

func TestCreateDueDateRepoSaveErrorOnFailure(t *testing.T) {
	repo := newFakeRepo()
	repo.saveErr = errors.New("db down")
	uc := NewCreateDueDateCharge(repo, &fakeProv{fail: true})
	_, err := uc.Execute(context.Background(), dueCmd())
	require.Error(t, err)
	require.Equal(t, "db down", err.Error())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/charge/app/...`
Expected: FAIL — `CreateDueDateChargeCmd`, `NewCreateDueDateCharge` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/charge/app/create_duedate.go`:

```go
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/efipix/pix/internal/charge/domain"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/money"
	"github.com/efipix/pix/internal/provider"
)

type CreateDueDateChargeCmd struct {
	TenantID          string
	PaymentProviderID string
	PixKey            string
	Amount            money.Centavos
	Description       string
	Payer             domain.Payer
	ExternalReference string
	Terms             domain.DueDateTerms
	Today             time.Time
}

type CreateDueDateCharge struct {
	repo ChargeRepository
	prov provider.PixProvider
}

func NewCreateDueDateCharge(repo ChargeRepository, prov provider.PixProvider) *CreateDueDateCharge {
	return &CreateDueDateCharge{repo: repo, prov: prov}
}

func (uc *CreateDueDateCharge) Execute(ctx context.Context, cmd CreateDueDateChargeCmd) (*domain.Charge, error) {
	c, err := domain.NewDueDate(domain.NewDueDateParams{
		TenantID: cmd.TenantID, PaymentProviderID: cmd.PaymentProviderID, PixKey: cmd.PixKey,
		Amount: cmd.Amount, Description: cmd.Description, Payer: cmd.Payer,
		ExternalReference: cmd.ExternalReference, Terms: cmd.Terms, Today: cmd.Today,
	})
	if err != nil {
		return nil, err
	}

	// tx A: record intent as CREATED before calling the provider.
	if err := uc.repo.Create(ctx, c); err != nil {
		return nil, err
	}

	res, perr := uc.prov.CreateDueDateCharge(ctx, toDueDateInput(c))
	if perr != nil {
		_ = c.MarkFailed(perr.Error())
		if serr := uc.repo.Save(ctx, c); serr != nil {
			return nil, serr
		}
		return nil, apperrs.Wrap(apperrs.KindProvider, "provider charge creation failed", perr)
	}

	if err := c.MarkActive(res.LocationID, res.QRCodeImage, res.PixPayload); err != nil {
		return nil, err
	}
	evt := OutboxEvent{
		ID: uuid.NewString(), TenantID: c.TenantID, AggregateID: c.ID,
		Type: "ChargeCreated", Payload: chargeCreatedPayload(c), // defined in create.go
	}
	if err := uc.repo.Save(ctx, c, evt); err != nil {
		return nil, err
	}
	return c, nil
}

// toDueDateInput maps the persisted domain charge to the provider port DTO,
// rendering centavos/percent values as decimal strings and dates as YYYY-MM-DD.
func toDueDateInput(c *domain.Charge) *provider.DueDateChargeInput {
	t := c.Terms
	in := &provider.DueDateChargeInput{
		Txid: c.Txid, PaymentProviderID: c.PaymentProviderID, Amount: c.Amount, PixKey: c.PixKey,
		Description: c.Description, PayerDoc: c.Payer.Doc, PayerDocType: c.Payer.DocType, PayerName: c.Payer.Name,
		DueDate: t.DueDate.Format("2006-01-02"), ValidityAfterDays: t.ValidityAfterDays,
	}
	if t.Fine != nil {
		in.Fine = &provider.FineInput{Mode: string(t.Fine.Mode), Value: t.Fine.Value.String()}
	}
	if t.Interest != nil {
		in.Interest = &provider.InterestInput{Mode: string(t.Interest.Mode), Value: t.Interest.Value.String()}
	}
	if t.Discount != nil {
		d := &provider.DiscountInput{Mode: string(t.Discount.Mode)}
		for _, e := range t.Discount.Entries {
			d.Entries = append(d.Entries, provider.DiscountEntryInput{Date: e.Date.Format("2006-01-02"), Value: e.Value.String()})
		}
		in.Discount = d
	}
	if t.Abatement > 0 {
		in.Abatement = t.Abatement.String()
	}
	return in
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/charge/app/...`
Expected: PASS (new cobv tests + all existing `CreateImmediateCharge` tests, which still compile — the cob use case is untouched).

- [ ] **Step 5: Commit**

```bash
git add internal/charge/app/create_duedate.go internal/charge/app/create_test.go internal/charge/app/create_duedate_test.go
git commit -m "feat(charge): CreateDueDateCharge use case (persist-first CobV)"
```

Append to `.wolf/memory.md`; add `create_duedate.go`/`create_duedate_test.go` to `.wolf/anatomy.md`.

---

### Task 2: CobV request DTO, validation, and `amount_due` response

**Files:**
- Modify: `internal/charge/api/dto.go`
- Test: `internal/charge/api/dto_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/charge/api/dto_test.go` (white-box `package api`):

```go
package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/brdate"
	"github.com/efipix/pix/internal/platform/money"
)

func TestBuildTermsParsesAll(t *testing.T) {
	req := createChargeRequest{Amount: "100.00", DueDate: "2026-12-31"}
	req.Fine = &ruleBlock{Mode: "percent", Value: "2.50"}
	req.Interest = &ruleBlock{Mode: "monthly_percent", Value: "1.00"}
	req.Discount = &discountBlock{Mode: "fixed", Entries: []discountEntryBlock{{Date: "2026-12-20", Value: "5.00"}}}
	req.Abatement = &abatementBlock{Value: "1.50"}

	terms, err := buildTerms(req)
	require.NoError(t, err)
	require.Equal(t, "2026-12-31", terms.DueDate.Format("2006-01-02"))
	require.Equal(t, 30, terms.ValidityAfterDays) // defaulted
	require.Equal(t, domain.FinePercent, terms.Fine.Mode)
	require.Equal(t, money.Centavos(250), terms.Fine.Value) // 2.50% -> 250
	require.Equal(t, money.Centavos(100), terms.Interest.Value)
	require.Equal(t, money.Centavos(500), terms.Discount.Entries[0].Value) // R$5.00 -> 500
	require.Equal(t, money.Centavos(150), terms.Abatement)
}

func TestBuildTermsRejectsUnknownMode(t *testing.T) {
	req := createChargeRequest{Amount: "100.00", DueDate: "2026-12-31"}
	req.Fine = &ruleBlock{Mode: "weekly_percent", Value: "2.00"}
	_, err := buildTerms(req)
	require.Error(t, err)
}

func TestBuildTermsRejectsBadDate(t *testing.T) {
	req := createChargeRequest{Amount: "100.00", DueDate: "31/12/2026"}
	_, err := buildTerms(req)
	require.Error(t, err)
}

func TestToResponseIncludesAmountDue(t *testing.T) {
	// Far-future due date + a far-future discount entry => "early" branch,
	// discount applies deterministically regardless of the real today.
	c := &domain.Charge{
		ID: "id1", Txid: "tx1", Status: domain.StatusActive, Amount: money.Centavos(10000),
		Kind: domain.KindCobV,
		Terms: &domain.DueDateTerms{
			DueDate: brdate.Date(2099, time.December, 31),
			Discount: &domain.Discount{Mode: domain.DiscountFixed, Entries: []domain.DiscountEntry{
				{Date: brdate.Date(2099, time.December, 20), Value: 500},
			}},
		},
	}
	r := toResponse(c)
	require.Equal(t, "2099-12-31", r.DueDate)
	require.NotNil(t, r.AmountDue)
	require.Equal(t, "100.00", r.AmountDue.Original)
	require.Equal(t, "5.00", r.AmountDue.Discount)
	require.Equal(t, "95.00", r.AmountDue.Total)
}

func TestToResponseNoAmountDueForCob(t *testing.T) {
	c := &domain.Charge{ID: "id2", Txid: "tx2", Status: domain.StatusActive, Amount: money.Centavos(500), Kind: domain.KindCob}
	r := toResponse(c)
	require.Nil(t, r.AmountDue)
	require.Empty(t, r.DueDate)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/charge/api/...`
Expected: FAIL — `ruleBlock`/`discountBlock`/`abatementBlock`/`buildTerms` undefined; `createChargeRequest` has no `DueDate`; `chargeResponse` has no `AmountDue`/`DueDate`.

- [ ] **Step 3: Write minimal implementation**

Replace the entire contents of `internal/charge/api/dto.go` with:

```go
package api

import (
	"github.com/efipix/pix/internal/charge/domain"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	"github.com/efipix/pix/internal/platform/brdate"
	"github.com/efipix/pix/internal/platform/money"
)

type ruleBlock struct {
	Mode  string `json:"mode"`
	Value string `json:"value"`
}

type discountEntryBlock struct {
	Date  string `json:"date"`
	Value string `json:"value"`
}

type discountBlock struct {
	Mode    string               `json:"mode"`
	Entries []discountEntryBlock `json:"entries"`
}

type abatementBlock struct {
	Value string `json:"value"`
}

// createChargeRequest is the CobV-only POST /charges body (ADR-0006):
// due_date required, no `kind`, optional fine/interest/discount/abatement.
type createChargeRequest struct {
	Amount            string          `json:"amount" binding:"required"`
	Description       string          `json:"description"`
	DueDate           string          `json:"due_date" binding:"required"`
	ValidityAfterDays *int            `json:"validity_after_days"`
	Fine              *ruleBlock      `json:"fine"`
	Interest          *ruleBlock      `json:"interest"`
	Discount          *discountBlock  `json:"discount"`
	Abatement         *abatementBlock `json:"abatement"`
	Payer             struct {
		Doc     string `json:"doc"`
		DocType string `json:"doc_type"`
		Name    string `json:"name"`
		Email   string `json:"email"`
		Phone   string `json:"phone"`
	} `json:"payer"`
	ExternalReference string `json:"external_reference"`
}

type amountDueDTO struct {
	AsOf      string `json:"as_of"`
	Original  string `json:"original"`
	Discount  string `json:"discount"`
	Abatement string `json:"abatement"`
	Fine      string `json:"fine"`
	Interest  string `json:"interest"`
	Total     string `json:"total"`
}

type chargeResponse struct {
	ID         string        `json:"id"`
	Txid       string        `json:"txid"`
	Status     string        `json:"status"`
	Amount     string        `json:"amount"`
	QRCode     string        `json:"qr_code_image"`
	PixPayload string        `json:"pix_payload"`
	Location   string        `json:"location_id"`
	DueDate    string        `json:"due_date,omitempty"`
	AmountDue  *amountDueDTO `json:"amount_due,omitempty"`
}

// buildTerms parses the request rule blocks into domain.DueDateTerms. It validates
// formats and mode enums; semantic range/date validation is enforced by
// domain.NewDueDate (called by the use case) against the Brazilian "today".
func buildTerms(req createChargeRequest) (domain.DueDateTerms, error) {
	due, err := brdate.Parse(req.DueDate)
	if err != nil {
		return domain.DueDateTerms{}, apperrs.New(apperrs.KindValidation, "invalid due_date (want YYYY-MM-DD)")
	}
	t := domain.DueDateTerms{DueDate: due, ValidityAfterDays: 30}
	if req.ValidityAfterDays != nil {
		t.ValidityAfterDays = *req.ValidityAfterDays
	}

	if req.Fine != nil {
		if req.Fine.Mode != string(domain.FineFixed) && req.Fine.Mode != string(domain.FinePercent) {
			return t, apperrs.New(apperrs.KindValidation, "invalid fine mode")
		}
		v, err := money.ParseString(req.Fine.Value)
		if err != nil {
			return t, apperrs.New(apperrs.KindValidation, "invalid fine value")
		}
		t.Fine = &domain.Fine{Mode: domain.FineMode(req.Fine.Mode), Value: v}
	}

	if req.Interest != nil {
		if req.Interest.Mode != string(domain.InterestDailyPercent) && req.Interest.Mode != string(domain.InterestMonthlyPercent) {
			return t, apperrs.New(apperrs.KindValidation, "invalid interest mode")
		}
		v, err := money.ParseString(req.Interest.Value)
		if err != nil {
			return t, apperrs.New(apperrs.KindValidation, "invalid interest value")
		}
		t.Interest = &domain.Interest{Mode: domain.InterestMode(req.Interest.Mode), Value: v}
	}

	if req.Discount != nil {
		if req.Discount.Mode != string(domain.DiscountFixed) && req.Discount.Mode != string(domain.DiscountPercent) {
			return t, apperrs.New(apperrs.KindValidation, "invalid discount mode")
		}
		d := &domain.Discount{Mode: domain.DiscountMode(req.Discount.Mode)}
		for _, e := range req.Discount.Entries {
			date, err := brdate.Parse(e.Date)
			if err != nil {
				return t, apperrs.New(apperrs.KindValidation, "invalid discount date")
			}
			v, err := money.ParseString(e.Value)
			if err != nil {
				return t, apperrs.New(apperrs.KindValidation, "invalid discount value")
			}
			d.Entries = append(d.Entries, domain.DiscountEntry{Date: date, Value: v})
		}
		t.Discount = d
	}

	if req.Abatement != nil {
		v, err := money.ParseString(req.Abatement.Value)
		if err != nil {
			return t, apperrs.New(apperrs.KindValidation, "invalid abatement value")
		}
		t.Abatement = v
	}
	return t, nil
}

func toResponse(c *domain.Charge) chargeResponse {
	r := chargeResponse{
		ID: c.ID, Txid: c.Txid, Status: string(c.Status), Amount: c.Amount.String(),
		QRCode: c.QRCodeImage, PixPayload: c.PixPayload, Location: c.LocationID,
	}
	if c.Terms != nil {
		asOf := brdate.Today()
		b := domain.EffectiveAmount(*c.Terms, c.Amount, asOf)
		r.DueDate = c.Terms.DueDate.Format("2006-01-02")
		r.AmountDue = &amountDueDTO{
			AsOf:     asOf.Format("2006-01-02"),
			Original: b.Original.String(), Discount: b.Discount.String(), Abatement: b.Abatement.String(),
			Fine: b.Fine.String(), Interest: b.Interest.String(), Total: b.Total.String(),
		}
	}
	return r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/charge/api/...`
Expected: the `dto_test.go` unit tests PASS. (The package may still fail to **build** because `handler.go` references the old `CreateImmediateCharge` use case and `process` doesn't call `buildTerms` yet — fixed in Task 3. If your runner builds the whole package, do Task 3 before re-running; the unit tests above are validated together with Task 3's build.)

- [ ] **Step 5: Commit**

```bash
git add internal/charge/api/dto.go internal/charge/api/dto_test.go
git commit -m "feat(charge): CobV request DTO, term parsing, and amount_due response"
```

Append to `.wolf/memory.md`; add `dto_test.go` to `.wolf/anatomy.md`; update `dto.go`'s description.

---

### Task 3: Handler + `cmd/server` wiring

**Files:**
- Modify: `internal/charge/api/handler.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Update the handler to use the cobv use case + validation**

In `internal/charge/api/handler.go`:

1. Change the `Handler` struct field and `NewHandler` signature from `*chargeapp.CreateImmediateCharge` to `*chargeapp.CreateDueDateCharge`:

```go
type Handler struct {
	uc   *chargeapp.CreateDueDateCharge
	repo chargeapp.ChargeRepository
}

func NewHandler(uc *chargeapp.CreateDueDateCharge, repo chargeapp.ChargeRepository) *Handler {
	return &Handler{uc: uc, repo: repo}
}
```

2. Replace `process` to parse + validate the cobv body and call the cobv use case:

```go
func (h *Handler) process(ctx context.Context, res *tenantctx.Resolved, raw []byte) (int, []byte) {
	var req createChargeRequest
	if err := json.Unmarshal(raw, &req); err != nil || req.Amount == "" || req.DueDate == "" {
		return http.StatusUnprocessableEntity, mustJSON(gin.H{"error": "invalid request body"})
	}
	amount, err := money.ParseString(req.Amount)
	if err != nil || amount <= 0 {
		return http.StatusUnprocessableEntity, mustJSON(gin.H{"error": "invalid amount"})
	}
	terms, err := buildTerms(req)
	if err != nil {
		return httpx.StatusFor(err), mustJSON(gin.H{"error": err.Error()})
	}
	charge, err := h.uc.Execute(ctx, chargeapp.CreateDueDateChargeCmd{
		TenantID: res.TenantID, PaymentProviderID: res.ProviderID, PixKey: res.PixKey,
		Amount: amount, Description: req.Description,
		Payer: domain.Payer{
			Doc: req.Payer.Doc, DocType: req.Payer.DocType, Name: req.Payer.Name,
			Email: req.Payer.Email, Phone: req.Payer.Phone,
		},
		ExternalReference: req.ExternalReference,
		Terms:             terms,
		Today:             brdate.Today(),
	})
	if err != nil {
		return httpx.StatusFor(err), mustJSON(gin.H{"error": err.Error()})
	}
	return http.StatusCreated, mustJSON(toResponse(charge))
}
```

3. Add `"github.com/efipix/pix/internal/platform/brdate"` to the import block. The `create`/`get`/`RegisterRoutes`/`mustJSON` functions stay unchanged (GET still returns `toResponse`, which now includes `amount_due` for cobv).

- [ ] **Step 2: Update `cmd/server/main.go` wiring**

In `cmd/server/main.go`, change the use-case construction (line ~55):

```go
	createUC := chargeapp.NewCreateDueDateCharge(chargeRepo, prov)
	chargeHandler := chargeapi.NewHandler(createUC, chargeRepo)
```

(`prov := efi.New(sp, efi.SDKFactory)` and the rest of `main` are unchanged — `prov` already satisfies the extended `PixProvider` interface.)

- [ ] **Step 3: Run build + unit tests to verify they pass**

Run: `go build ./... && go test ./internal/charge/api/... ./internal/charge/app/...`
Expected: build succeeds; `dto_test.go` and app use-case tests PASS. (The `e2e_test.go`, tagged `integration`, is not built here — Task 4 rewrites it.)

- [ ] **Step 4: Commit**

```bash
git add internal/charge/api/handler.go cmd/server/main.go
git commit -m "feat(charge): wire CobV-only POST /charges handler and server"
```

Append to `.wolf/memory.md`.

---

### Task 4: Rewrite the end-to-end test for CobV

**Files:**
- Rewrite: `internal/charge/api/e2e_test.go`

- [ ] **Step 1: Write the failing test**

Replace the entire contents of `internal/charge/api/e2e_test.go` with:

```go
//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	chargeapi "github.com/efipix/pix/internal/charge/api"
	chargeapp "github.com/efipix/pix/internal/charge/app"
	chargeinfra "github.com/efipix/pix/internal/charge/infra"
	"github.com/efipix/pix/internal/platform/db"
	"github.com/efipix/pix/internal/platform/idempotency"
	"github.com/efipix/pix/internal/provider"
	tenantapi "github.com/efipix/pix/internal/tenant/api"
	tenantapp "github.com/efipix/pix/internal/tenant/app"
	tenantinfra "github.com/efipix/pix/internal/tenant/infra"
)

type fakeProv struct{ fail bool }

func (f *fakeProv) CreateImmediateCharge(_ context.Context, in *provider.ImmediateChargeInput) (*provider.ChargeResult, error) {
	return &provider.ChargeResult{Txid: in.Txid, Status: "ATIVA"}, nil
}
func (f *fakeProv) CreateDueDateCharge(_ context.Context, in *provider.DueDateChargeInput) (*provider.ChargeResult, error) {
	if f.fail {
		return nil, &providerErr{}
	}
	return &provider.ChargeResult{Txid: in.Txid, Status: "ATIVA", LocationID: "loc1", QRCodeImage: "img", PixPayload: "000201..."}, nil
}
func (f *fakeProv) GetCharge(context.Context, string, string) (*provider.ChargeResult, error) {
	return nil, nil
}

type providerErr struct{}

func (providerErr) Error() string { return "efi down" }

func run(t *testing.T, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "command %s failed", name)
}

func boot(t *testing.T, prov provider.PixProvider) *gin.Engine {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
		tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	run(t, "goose", "-dir", "../../../db/migrations", "postgres", dsn, "up")
	run(t, "psql", dsn, "-f", "../../../db/seed/dev.sql")

	t.Setenv("DATABASE_ADMIN_URL", dsn)
	pool, err := db.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	tenantRepo := tenantinfra.New(pool)
	chargeRepo := chargeinfra.New(pool)
	uc := chargeapp.NewCreateDueDateCharge(chargeRepo, prov)
	h := chargeapi.NewHandler(uc, chargeRepo)
	idem := idempotency.NewPg(pool)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(tenantapi.Middleware(tenantapp.NewResolver(tenantRepo)))
	chargeapi.RegisterRoutes(v1, h, idem)
	return r
}

func post(r *gin.Engine, key, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/charges", bytes.NewBufferString(body))
	req.Header.Set("X-Api-Key", "pk_dev_secret")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	r.ServeHTTP(w, req)
	return w
}

// validBody is a CobV charge with a future due date and a fine; amount_due with no
// late/early adjustment (future due date => "early", no discount) equals original.
const validBody = `{"amount":"100.00","due_date":"2026-12-31","fine":{"mode":"percent","value":"2.00"}}`

func TestE2ECreateCobVActiveAndReplay(t *testing.T) {
	r := boot(t, &fakeProv{})

	w := post(r, "key-1", validBody)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "ACTIVE", resp["status"])
	require.Equal(t, "100.00", resp["amount"])
	require.Equal(t, "2026-12-31", resp["due_date"])
	require.NotEmpty(t, resp["txid"])
	ad := resp["amount_due"].(map[string]any)
	require.Equal(t, "100.00", ad["original"])
	require.Equal(t, "100.00", ad["total"]) // future due date: no fine/interest/discount yet
	firstTxid := resp["txid"]

	// Replay: same key + body returns the same charge.
	w2 := post(r, "key-1", validBody)
	require.Equal(t, http.StatusCreated, w2.Code)
	var resp2 map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
	require.Equal(t, firstTxid, resp2["txid"])

	// GET returns the charge with its amount_due breakdown.
	id := resp["id"].(string)
	gw := httptest.NewRecorder()
	greq, _ := http.NewRequest(http.MethodGet, "/api/v1/charges/"+id, nil)
	greq.Header.Set("X-Api-Key", "pk_dev_secret")
	r.ServeHTTP(gw, greq)
	require.Equal(t, http.StatusOK, gw.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(gw.Body.Bytes(), &got))
	require.NotNil(t, got["amount_due"])
}

func TestE2ERejectsPastDueDate(t *testing.T) {
	r := boot(t, &fakeProv{})
	w := post(r, "kpast", `{"amount":"100.00","due_date":"2020-01-01"}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
}

func TestE2EMissingIdempotencyKey(t *testing.T) {
	r := boot(t, &fakeProv{})
	w := post(r, "", validBody)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestE2EConflictOnDifferentBody(t *testing.T) {
	r := boot(t, &fakeProv{})
	require.Equal(t, http.StatusCreated, post(r, "k", validBody).Code)
	require.Equal(t, http.StatusUnprocessableEntity,
		post(r, "k", `{"amount":"200.00","due_date":"2026-12-31"}`).Code)
}

func TestE2EProviderFailureRecordsFailed(t *testing.T) {
	r := boot(t, &fakeProv{fail: true})
	w := post(r, "kf", validBody)
	require.Equal(t, http.StatusBadGateway, w.Code)
	// Replay returns the same recorded failure.
	require.Equal(t, http.StatusBadGateway, post(r, "kf", validBody).Code)
}
```

- [ ] **Step 2: Run test to verify it passes**

Run: `go test -tags=integration ./internal/charge/api/ -v`
Expected: all five e2e cases PASS (Docker required). If `TestE2ERejectsPastDueDate` fails because the current date has passed `2026-12-31`, bump the future date in `validBody` and the assertions accordingly.

- [ ] **Step 3: Commit**

```bash
git add internal/charge/api/e2e_test.go
git commit -m "test(charge): rewrite end-to-end for CobV-only POST /charges"
```

Append to `.wolf/memory.md`.

---

## File 04 done — Phase 2 exit verification

Run the full gate (per overview exit checklist):

```bash
export PATH="$PATH:/home/fj/go/bin"
go vet ./...
go test -race -cover ./...
go test -race -tags=integration ./...
golangci-lint run ./...
go test -cover ./internal/charge/domain/   # confirm >= 80%
```

Expected: all green; domain coverage ≥ 80%. Then walk the [00-overview](2026-06-12-phase2-00-overview.md) "Phase 2 exit checklist" and tick each item.

**Homologation smoke (optional, gated):** if `EFI_CREDENTIALS`, `EFI_TEST_PROVIDER_ID`, `EFI_TEST_PIX_KEY` are set, add/extend `internal/provider/efi/sdkclient_homolog_test.go` (build tag `homolog`) to `CreateCobV` a real charge against EFí homologation and assert a non-empty `pixCopiaECola`. Skip if creds are absent (same condition as Phase 1).

**Finish the branch:** use superpowers:finishing-a-development-branch to merge `phase2-due-date-charges` → `main` (Phase 1 merged fast-forward and deleted the branch). Update `.wolf/cerebrum.md` Decision Log with the Phase 2 outcome and any deviations.
