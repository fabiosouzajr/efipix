# Phase 2 · File 01 — Domain Rules: `brdate`, `DueDateTerms`, `EffectiveAmount`, `NewDueDate`

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Read [00-overview](2026-06-12-phase2-00-overview.md) first — it holds the locked decisions (percent representation, `brdate.Days` semantics, `EffectiveAmount` rules) this file implements.

**Scope:** Pure domain + a platform date helper. No DB, no HTTP, no provider. Everything here is unit-tested with no build tag.

**Files:**
- Create: `internal/platform/brdate/brdate.go`, `internal/platform/brdate/brdate_test.go`
- Create: `internal/charge/domain/rules.go`, `internal/charge/domain/rules_test.go`
- Modify: `internal/charge/domain/charge.go` (add `Terms *DueDateTerms` field + `NewDueDate` constructor)
- Create: `internal/charge/domain/duedate_test.go`

**Before starting:** create the feature branch.

```bash
git checkout -b phase2-due-date-charges
```

---

### Task 1: `brdate` — America/Sao_Paulo business-date helper

**Files:**
- Create: `internal/platform/brdate/brdate.go`
- Test: `internal/platform/brdate/brdate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/platform/brdate/brdate_test.go`:

```go
package brdate_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/brdate"
)

func TestParseIsSaoPauloMidnight(t *testing.T) {
	d, err := brdate.Parse("2026-06-10")
	require.NoError(t, err)
	y, m, day := d.Date()
	require.Equal(t, 2026, y)
	require.Equal(t, time.June, m)
	require.Equal(t, 10, day)
	require.Equal(t, 0, d.Hour())
	require.Equal(t, brdate.Loc, d.Location())
}

func TestParseRejectsBadFormat(t *testing.T) {
	_, err := brdate.Parse("10/06/2026")
	require.Error(t, err)
}

func TestDate(t *testing.T) {
	d := brdate.Date(2026, time.June, 15)
	require.Equal(t, "2026-06-15", d.Format("2006-01-02"))
	require.Equal(t, 0, d.Hour())
}

func TestDaysForward(t *testing.T) {
	require.Equal(t, 5, brdate.Days(brdate.Date(2026, time.June, 10), brdate.Date(2026, time.June, 15)))
	require.Equal(t, -5, brdate.Days(brdate.Date(2026, time.June, 15), brdate.Date(2026, time.June, 10)))
	require.Equal(t, 0, brdate.Days(brdate.Date(2026, time.June, 10), brdate.Date(2026, time.June, 10)))
}

// Locks the off-by-one decision: a UTC-midnight date (how pgx decodes a `date`
// column) compared against an SP-midnight date must keep its own civil date.
func TestDaysUsesOwnCivilDate(t *testing.T) {
	dbDate := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC) // pgx-style date value
	asOf := brdate.Date(2026, time.June, 15)                       // SP midnight
	require.Equal(t, 5, brdate.Days(dbDate, asOf))
}

func TestTodayIsMidnightInLoc(t *testing.T) {
	d := brdate.Today()
	require.Equal(t, 0, d.Hour())
	require.Equal(t, brdate.Loc, d.Location())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/brdate/...`
Expected: FAIL — package `brdate` does not exist.

- [ ] **Step 3: Write minimal implementation**

Create `internal/platform/brdate/brdate.go`:

```go
// Package brdate provides Brazilian business-date math in America/Sao_Paulo.
// "Today" and due-date comparisons use this zone, not UTC.
package brdate

import (
	"time"
	_ "time/tzdata" // embed zoneinfo so America/Sao_Paulo loads on minimal images (Alpine)
)

// Loc is the Brazilian business timezone used for all date math.
var Loc = mustLoad("America/Sao_Paulo")

func mustLoad(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic("brdate: load " + name + ": " + err.Error())
	}
	return l
}

// Date builds a date at midnight in America/Sao_Paulo.
func Date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, Loc)
}

// Today returns the current Brazilian business date (midnight, America/Sao_Paulo).
func Today() time.Time {
	n := time.Now().In(Loc)
	return Date(n.Year(), n.Month(), n.Day())
}

// Parse parses a "2006-01-02" string as a date at America/Sao_Paulo midnight.
func Parse(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, Loc)
}

// Days returns the whole days from a to b (b - a). It compares each argument's
// OWN-location civil date (it does not convert a or b into Loc first), so a
// UTC-midnight date decoded by pgx and an SP-midnight date both keep their
// intended calendar day. DST-safe (subtracts dates as UTC midnights).
func Days(a, b time.Time) int {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	au := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	bu := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	return int(bu.Sub(au).Hours() / 24)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/brdate/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/brdate/
git commit -m "feat(brdate): America/Sao_Paulo business-date helper"
```

Then append to `.wolf/memory.md` and add the new files to `.wolf/anatomy.md` under `## internal/platform/brdate/`.

---

### Task 2: `EffectiveAmount` + rule value objects

This is the heaviest math in Phase 2. We write the value objects and the pure rule function together (the test references the types, forcing them into existence), then exhaustively test the rules.

**Files:**
- Create: `internal/charge/domain/rules.go`
- Test: `internal/charge/domain/rules_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/charge/domain/rules_test.go` (white-box `package domain` so it can test the unexported `roundHalfUp`):

```go
package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/platform/brdate"
	"github.com/efipix/pix/internal/platform/money"
)

func TestRoundHalfUp(t *testing.T) {
	require.Equal(t, money.Centavos(0), roundHalfUp(4, 10))  // 0.4 -> 0
	require.Equal(t, money.Centavos(1), roundHalfUp(5, 10))  // 0.5 -> 1
	require.Equal(t, money.Centavos(2), roundHalfUp(15, 10)) // 1.5 -> 2
	require.Equal(t, money.Centavos(0), roundHalfUp(0, 0))   // guard: den 0
}

var due = brdate.Date(2026, time.June, 10)

func TestEffectiveOnTime(t *testing.T) {
	terms := DueDateTerms{DueDate: due}
	b := EffectiveAmount(terms, 10000, due)
	require.Equal(t, money.Centavos(10000), b.Original)
	require.Equal(t, money.Centavos(0), b.Fine)
	require.Equal(t, money.Centavos(0), b.Interest)
	require.Equal(t, money.Centavos(0), b.Discount)
	require.Equal(t, money.Centavos(10000), b.Total)
}

func TestEffectiveLateFineFixedAndDailyInterest(t *testing.T) {
	terms := DueDateTerms{
		DueDate:  due,
		Fine:     &Fine{Mode: FineFixed, Value: 500},                  // R$5.00 fixed
		Interest: &Interest{Mode: InterestDailyPercent, Value: 100},   // 1.00%/day
	}
	b := EffectiveAmount(terms, 10000, brdate.Date(2026, time.June, 15)) // 5 days late
	require.Equal(t, money.Centavos(500), b.Fine)
	require.Equal(t, money.Centavos(500), b.Interest) // 10000*100*5/10000 = 500
	require.Equal(t, money.Centavos(11000), b.Total)
}

func TestEffectiveLateFinePercent(t *testing.T) {
	terms := DueDateTerms{DueDate: due, Fine: &Fine{Mode: FinePercent, Value: 250}} // 2.50%
	b := EffectiveAmount(terms, 10000, brdate.Date(2026, time.June, 11))            // 1 day late
	require.Equal(t, money.Centavos(250), b.Fine) // 10000*250/10000 = 250
	require.Equal(t, money.Centavos(10250), b.Total)
}

func TestEffectiveLateMonthlyInterestProRates(t *testing.T) {
	terms := DueDateTerms{DueDate: due, Interest: &Interest{Mode: InterestMonthlyPercent, Value: 300}} // 3.00%/mo
	b := EffectiveAmount(terms, 10000, brdate.Date(2026, time.June, 20))                                // 10 days late
	require.Equal(t, money.Centavos(100), b.Interest) // 10000*300*10/(10000*30) = 100
	require.Equal(t, money.Centavos(10100), b.Total)
}

func TestEffectiveEarlyDiscountFixedPicksNearestDeadline(t *testing.T) {
	terms := DueDateTerms{
		DueDate: due,
		Discount: &Discount{Mode: DiscountFixed, Entries: []DiscountEntry{
			{Date: brdate.Date(2026, time.June, 8), Value: 300},  // R$3.00 if paid by Jun 8
			{Date: brdate.Date(2026, time.June, 10), Value: 100}, // R$1.00 if paid by Jun 10
		}},
	}
	// Pay Jun 5: both deadlines still met, nearest is Jun 8 -> R$3.00.
	b := EffectiveAmount(terms, 10000, brdate.Date(2026, time.June, 5))
	require.Equal(t, money.Centavos(300), b.Discount)
	require.Equal(t, money.Centavos(9700), b.Total)

	// Pay Jun 9: Jun 8 deadline missed, only Jun 10 qualifies -> R$1.00.
	b2 := EffectiveAmount(terms, 10000, brdate.Date(2026, time.June, 9))
	require.Equal(t, money.Centavos(100), b2.Discount)
}

func TestEffectiveEarlyDiscountPercent(t *testing.T) {
	terms := DueDateTerms{
		DueDate: due,
		Discount: &Discount{Mode: DiscountPercent, Entries: []DiscountEntry{
			{Date: brdate.Date(2026, time.June, 8), Value: 250}, // 2.50%
		}},
	}
	b := EffectiveAmount(terms, 10000, brdate.Date(2026, time.June, 5))
	require.Equal(t, money.Centavos(250), b.Discount) // 2.5% of 10000
	require.Equal(t, money.Centavos(9750), b.Total)
}

func TestEffectiveEarlyNoQualifyingDiscount(t *testing.T) {
	terms := DueDateTerms{
		DueDate: due,
		Discount: &Discount{Mode: DiscountFixed, Entries: []DiscountEntry{
			{Date: brdate.Date(2026, time.June, 8), Value: 300},
		}},
	}
	b := EffectiveAmount(terms, 10000, brdate.Date(2026, time.June, 9)) // Jun 8 already passed
	require.Equal(t, money.Centavos(0), b.Discount)
	require.Equal(t, money.Centavos(10000), b.Total)
}

func TestEffectiveAbatementAlwaysSubtracted(t *testing.T) {
	// On time, abatement still applies.
	onTime := EffectiveAmount(DueDateTerms{DueDate: due, Abatement: 200}, 10000, due)
	require.Equal(t, money.Centavos(200), onTime.Abatement)
	require.Equal(t, money.Centavos(9800), onTime.Total)

	// Late: fine + abatement combine.
	late := EffectiveAmount(
		DueDateTerms{DueDate: due, Abatement: 200, Fine: &Fine{Mode: FineFixed, Value: 500}},
		10000, brdate.Date(2026, time.June, 11))
	require.Equal(t, money.Centavos(10300), late.Total) // 10000 - 200 + 500
}

func TestEffectiveTotalClampedAtZero(t *testing.T) {
	terms := DueDateTerms{DueDate: due, Abatement: 20000} // abatement exceeds original
	b := EffectiveAmount(terms, 10000, due)
	require.Equal(t, money.Centavos(0), b.Total)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/charge/domain/...`
Expected: FAIL — `DueDateTerms`, `Fine`, `EffectiveAmount`, `roundHalfUp`, etc. undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/charge/domain/rules.go`:

```go
package domain

import (
	"time"

	"github.com/efipix/pix/internal/platform/brdate"
	"github.com/efipix/pix/internal/platform/money"
)

// Rule modes. Percent values are hundredths of a percent (see overview "Percent
// representation"): 2.50% == money.Centavos(250). Fixed values are centavos.
type (
	FineMode     string
	InterestMode string
	DiscountMode string
)

const (
	FineFixed   FineMode = "fixed"
	FinePercent FineMode = "percent"

	InterestDailyPercent   InterestMode = "daily_percent"
	InterestMonthlyPercent InterestMode = "monthly_percent"

	DiscountFixed   DiscountMode = "fixed"
	DiscountPercent DiscountMode = "percent"
)

type Fine struct {
	Mode  FineMode
	Value money.Centavos
}

type Interest struct {
	Mode  InterestMode
	Value money.Centavos
}

type DiscountEntry struct {
	Date  time.Time
	Value money.Centavos
}

type Discount struct {
	Mode    DiscountMode
	Entries []DiscountEntry // 1..3 date-banded entries
}

// DueDateTerms is the CobV value object: a due date plus optional payer-charge
// rules. Each of Fine/Interest/Discount is nil when not configured; Abatement
// is 0 when not configured.
type DueDateTerms struct {
	DueDate           time.Time
	ValidityAfterDays int
	Fine              *Fine
	Interest          *Interest
	Discount          *Discount
	Abatement         money.Centavos
}

// AmountBreakdown is the per-component quote computed by EffectiveAmount.
type AmountBreakdown struct {
	Original  money.Centavos
	Discount  money.Centavos
	Abatement money.Centavos
	Fine      money.Centavos
	Interest  money.Centavos
	Total     money.Centavos
}

// EffectiveAmount quotes the amount due for `base` as of `asOf`. Pure (no I/O).
// See 00-overview "EffectiveAmount rules" for the authoritative definition.
func EffectiveAmount(terms DueDateTerms, base money.Centavos, asOf time.Time) AmountBreakdown {
	b := AmountBreakdown{Original: base}
	diff := brdate.Days(terms.DueDate, asOf) // asOf - due, in days
	switch {
	case diff < 0: // early
		b.Discount = earlyDiscount(terms.Discount, base, asOf)
	case diff == 0: // on time
		// nothing extra
	default: // late
		b.Fine = fineAmount(terms.Fine, base)
		b.Interest = interestAmount(terms.Interest, base, diff)
	}
	b.Abatement = terms.Abatement // always subtracted
	total := int64(base) - int64(b.Discount) - int64(b.Abatement) + int64(b.Fine) + int64(b.Interest)
	if total < 0 {
		total = 0
	}
	b.Total = money.Centavos(total)
	return b
}

func fineAmount(f *Fine, base money.Centavos) money.Centavos {
	if f == nil {
		return 0
	}
	switch f.Mode {
	case FineFixed:
		return f.Value
	case FinePercent:
		return pct(base, f.Value)
	}
	return 0
}

func interestAmount(in *Interest, base money.Centavos, daysLate int) money.Centavos {
	if in == nil || daysLate <= 0 {
		return 0
	}
	switch in.Mode {
	case InterestDailyPercent:
		return roundHalfUp(int64(base)*int64(in.Value)*int64(daysLate), 10000)
	case InterestMonthlyPercent:
		return roundHalfUp(int64(base)*int64(in.Value)*int64(daysLate), 10000*30)
	}
	return 0
}

func earlyDiscount(d *Discount, base money.Centavos, asOf time.Time) money.Centavos {
	if d == nil || len(d.Entries) == 0 {
		return 0
	}
	var best *DiscountEntry
	for i := range d.Entries {
		e := &d.Entries[i]
		if brdate.Days(asOf, e.Date) < 0 { // e.Date < asOf: deadline missed
			continue
		}
		if best == nil || brdate.Days(e.Date, best.Date) > 0 { // e.Date earlier than best
			best = e
		}
	}
	if best == nil {
		return 0
	}
	switch d.Mode {
	case DiscountFixed:
		return best.Value
	case DiscountPercent:
		return pct(base, best.Value)
	}
	return 0
}

// pct applies a hundredths-of-a-percent value to base: base * value / 10000.
func pct(base, valueHundredthsPercent money.Centavos) money.Centavos {
	return roundHalfUp(int64(base)*int64(valueHundredthsPercent), 10000)
}

// roundHalfUp returns round(num/den) with halves rounded up (non-negative inputs).
func roundHalfUp(num, den int64) money.Centavos {
	if den == 0 {
		return 0
	}
	return money.Centavos((2*num + den) / (2 * den))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/charge/domain/...`
Expected: PASS (all rule tests + existing `charge_test.go`/`transitions_test.go`).

- [ ] **Step 5: Commit**

```bash
git add internal/charge/domain/rules.go internal/charge/domain/rules_test.go
git commit -m "feat(charge): DueDateTerms value object and pure EffectiveAmount rules"
```

Append to `.wolf/memory.md`; add `rules.go`/`rules_test.go` to `.wolf/anatomy.md`.

---

### Task 3: `NewDueDate` constructor + `Charge.Terms` field + validation

**Files:**
- Modify: `internal/charge/domain/charge.go` (add `Terms *DueDateTerms` to `Charge`; add `NewDueDateParams` + `NewDueDate` + validation helpers)
- Test: `internal/charge/domain/duedate_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/charge/domain/duedate_test.go`:

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/brdate"
	"github.com/efipix/pix/internal/platform/money"
)

func baseParams() domain.NewDueDateParams {
	today := brdate.Date(2026, time.June, 1)
	return domain.NewDueDateParams{
		TenantID: "t1", PaymentProviderID: "p1", PixKey: "k@e.com",
		Amount: money.Centavos(10000),
		Terms:  domain.DueDateTerms{DueDate: brdate.Date(2026, time.June, 10), ValidityAfterDays: 30},
		Today:  today,
	}
}

func TestNewDueDateCreatesCobVCreated(t *testing.T) {
	c, err := domain.NewDueDate(baseParams())
	require.NoError(t, err)
	require.Equal(t, domain.KindCobV, c.Kind)
	require.Equal(t, domain.StatusCreated, c.Status)
	require.NotEmpty(t, c.Txid)
	require.NotNil(t, c.Terms)
	require.Len(t, c.Events, 1)
	require.Equal(t, "created", c.Events[0].EventType)
}

func TestNewDueDateRejectsPastDueDate(t *testing.T) {
	p := baseParams()
	p.Terms.DueDate = brdate.Date(2026, time.May, 31) // before Today (Jun 1)
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateRejectsZeroDueDate(t *testing.T) {
	p := baseParams()
	p.Terms.DueDate = time.Time{}
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateRejectsNegativeValidity(t *testing.T) {
	p := baseParams()
	p.Terms.ValidityAfterDays = -1
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateRejectsPercentOutOfRange(t *testing.T) {
	p := baseParams()
	p.Terms.Fine = &domain.Fine{Mode: domain.FinePercent, Value: 10001} // > 100.00%
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateRejectsFixedFineZero(t *testing.T) {
	p := baseParams()
	p.Terms.Fine = &domain.Fine{Mode: domain.FineFixed, Value: 0}
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateRejectsTooManyDiscountEntries(t *testing.T) {
	p := baseParams()
	p.Terms.Discount = &domain.Discount{Mode: domain.DiscountFixed, Entries: []domain.DiscountEntry{
		{Date: brdate.Date(2026, time.June, 5), Value: 100},
		{Date: brdate.Date(2026, time.June, 6), Value: 100},
		{Date: brdate.Date(2026, time.June, 7), Value: 100},
		{Date: brdate.Date(2026, time.June, 8), Value: 100},
	}}
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateRejectsDiscountDateNotAfterToday(t *testing.T) {
	p := baseParams()
	p.Terms.Discount = &domain.Discount{Mode: domain.DiscountFixed, Entries: []domain.DiscountEntry{
		{Date: brdate.Date(2026, time.June, 1), Value: 100}, // == Today, must be strictly after
	}}
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateRejectsDiscountDateAfterDueDate(t *testing.T) {
	p := baseParams()
	p.Terms.Discount = &domain.Discount{Mode: domain.DiscountFixed, Entries: []domain.DiscountEntry{
		{Date: brdate.Date(2026, time.June, 11), Value: 100}, // after due_date Jun 10
	}}
	_, err := domain.NewDueDate(p)
	require.Error(t, err)
}

func TestNewDueDateAcceptsFullTerms(t *testing.T) {
	p := baseParams()
	p.Terms.Fine = &domain.Fine{Mode: domain.FinePercent, Value: 200}
	p.Terms.Interest = &domain.Interest{Mode: domain.InterestMonthlyPercent, Value: 100}
	p.Terms.Discount = &domain.Discount{Mode: domain.DiscountFixed, Entries: []domain.DiscountEntry{
		{Date: brdate.Date(2026, time.June, 5), Value: 300},
	}}
	p.Terms.Abatement = money.Centavos(150)
	c, err := domain.NewDueDate(p)
	require.NoError(t, err)
	require.Equal(t, money.Centavos(150), c.Terms.Abatement)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/charge/domain/...`
Expected: FAIL — `NewDueDate`, `NewDueDateParams` undefined; `Charge` has no `Terms` field.

- [ ] **Step 3: Write minimal implementation**

In `internal/charge/domain/charge.go`, add the `Terms` field to the `Charge` struct (place it after the `Payer Payer` line, before `ExternalReference`):

```go
	Payer             Payer
	Terms             *DueDateTerms // nil for cob; set for cobv
	ExternalReference string
```

Then append to `internal/charge/domain/charge.go` (after `NewImmediate`):

```go
type NewDueDateParams struct {
	TenantID          string
	PaymentProviderID string
	PixKey            string
	Amount            money.Centavos
	Description       string
	Payer             Payer
	ExternalReference string
	Terms             DueDateTerms
	Today             time.Time // SP business date used for past-date validation
}

// NewDueDate builds a CobV charge (Kind=cobv, status CREATED, event "created").
func NewDueDate(p NewDueDateParams) (*Charge, error) {
	if p.Amount <= 0 {
		return nil, apperrs.New(apperrs.KindValidation, "amount must be positive")
	}
	if p.PixKey == "" {
		return nil, apperrs.New(apperrs.KindValidation, "pix key required")
	}
	if err := validateTerms(p.Terms, p.Today); err != nil {
		return nil, err
	}
	terms := p.Terms
	c := &Charge{
		ID: uuid.NewString(), TenantID: p.TenantID, PaymentProviderID: p.PaymentProviderID,
		Txid: NewTxid(), Kind: KindCobV, Status: StatusCreated, Amount: p.Amount,
		PixKey: p.PixKey, Description: p.Description,
		Payer: p.Payer, Terms: &terms, ExternalReference: p.ExternalReference, Version: 0,
	}
	c.appendEvent("created")
	return c, nil
}

func validateTerms(t DueDateTerms, today time.Time) error {
	if t.DueDate.IsZero() {
		return apperrs.New(apperrs.KindValidation, "due_date required")
	}
	if brdate.Days(today, t.DueDate) < 0 {
		return apperrs.New(apperrs.KindValidation, "due_date must not be in the past")
	}
	if t.ValidityAfterDays < 0 {
		return apperrs.New(apperrs.KindValidation, "validity_after_days must be >= 0")
	}
	if t.Fine != nil {
		if err := validateRuleValue(t.Fine.Mode == FinePercent, t.Fine.Value); err != nil {
			return err
		}
	}
	if t.Interest != nil { // interest modes are always percent
		if err := validatePercent(t.Interest.Value); err != nil {
			return err
		}
	}
	if t.Discount != nil {
		if n := len(t.Discount.Entries); n == 0 || n > 3 {
			return apperrs.New(apperrs.KindValidation, "discount needs 1-3 entries")
		}
		for _, e := range t.Discount.Entries {
			if brdate.Days(today, e.Date) <= 0 {
				return apperrs.New(apperrs.KindValidation, "discount date must be after today")
			}
			if brdate.Days(e.Date, t.DueDate) < 0 {
				return apperrs.New(apperrs.KindValidation, "discount date must not be after due_date")
			}
			if err := validateRuleValue(t.Discount.Mode == DiscountPercent, e.Value); err != nil {
				return err
			}
		}
	}
	if t.Abatement < 0 {
		return apperrs.New(apperrs.KindValidation, "abatement must be > 0")
	}
	return nil
}

func validateRuleValue(isPercent bool, v money.Centavos) error {
	if isPercent {
		return validatePercent(v)
	}
	if v <= 0 {
		return apperrs.New(apperrs.KindValidation, "fixed value must be > 0")
	}
	return nil
}

func validatePercent(v money.Centavos) error {
	if v < 0 || v > 10000 { // [0,100]% in hundredths-of-a-percent
		return apperrs.New(apperrs.KindValidation, "percent must be in [0,100]")
	}
	return nil
}
```

`charge.go` already imports `time`, `github.com/google/uuid`, `apperrs`, and `money`; `brdate` is the only new import. Add `"github.com/efipix/pix/internal/platform/brdate"` to its import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/charge/domain/...`
Expected: PASS (all domain tests, including the unchanged `charge_test.go`/`transitions_test.go` — `NewImmediate` still works; the new `Terms` field is nil for cob).

Then verify the coverage gate for the domain package:

Run: `go test -cover ./internal/charge/domain/`
Expected: coverage `≥ 80%` (the rule + constructor tests push it well past).

- [ ] **Step 5: Commit**

```bash
git add internal/charge/domain/charge.go internal/charge/domain/duedate_test.go
git commit -m "feat(charge): NewDueDate constructor with CobV terms validation"
```

Append to `.wolf/memory.md`; note in `.wolf/anatomy.md` that `charge.go` gained `NewDueDate`/`Terms` and `duedate_test.go` was added.

---

## File 01 done — checkpoint

Verify before moving on:

```bash
go vet ./internal/charge/... ./internal/platform/brdate/...
go test -race -cover ./internal/charge/domain/ ./internal/platform/brdate/
export PATH="$PATH:/home/fj/go/bin" && golangci-lint run ./internal/charge/... ./internal/platform/brdate/...
```

Expected: all green, domain coverage ≥ 80%. Proceed to [02-schema-repository](2026-06-12-phase2-02-schema-repository.md).
