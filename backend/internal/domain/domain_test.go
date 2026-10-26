package domain

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// Каждый разрешённый переход проходит, всё остальное — ErrInvalidTransition.
func TestTransitionsExhaustive(t *testing.T) {
	all := []Status{StatusDraft, StatusFullForm, StatusSubmitted, StatusDataCheck, StatusScoring, StatusApproved, StatusCounterOffer,
		StatusNeedInfo, StatusRejected, StatusSigning, StatusIssued, StatusClosed, StatusCancelled}
	allowed := 0
	for _, from := range all {
		for _, to := range all {
			a := &Application{Status: from}
			err := a.Transition(to, "test", "", t0)
			if CanTransition(from, to) {
				allowed++
				if err != nil || a.Status != to || len(a.History) != 1 || !a.StatusChangedAt.Equal(t0) {
					t.Fatalf("%s→%s must succeed and be audited: err=%v app=%+v", from, to, err, a)
				}
			} else if !errors.Is(err, ErrInvalidTransition) || a.Status != from {
				t.Fatalf("%s→%s must be rejected, got err=%v status=%s", from, to, err, a.Status)
			}
		}
	}
	if allowed == 0 {
		t.Fatal("no transitions allowed at all")
	}
	// конечные статусы — тупики
	for _, final := range []Status{StatusRejected, StatusCancelled, StatusClosed} {
		for _, to := range all {
			if CanTransition(final, to) {
				t.Fatalf("final status %s must not transition to %s", final, to)
			}
		}
	}
}

// Автосохранение — только 2-й уровень и только пока его можно править.
func TestApplyPatchOnlyInEditableStatuses(t *testing.T) {
	ff := &FullForm{Consent: true}
	for _, s := range []Status{StatusDraft, StatusSubmitted, StatusApproved, StatusIssued} {
		a := &Application{Status: s, Level: 2}
		if err := a.ApplyPatch(DraftPatch{FullForm: ff}, t0); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("patch in %s must be forbidden, got %v", s, err)
		}
	}
	for _, s := range []Status{StatusFullForm, StatusNeedInfo} {
		a := &Application{Status: s, Level: 2}
		if err := a.ApplyPatch(DraftPatch{FullForm: ff}, t0); err != nil || a.Full == nil || !a.Full.Consent || a.Full.Collateral.Type != CollateralNone {
			t.Fatalf("%s must be editable: %v %+v", s, err, a.Full)
		}
	}
}

func TestApplyPatchValidation(t *testing.T) {
	cases := map[string]FullForm{
		"unknown collateral": {Collateral: Collateral{Type: "yacht", Value: 1}},
		"negative value":     {Collateral: Collateral{Type: CollateralRealty, Value: -5}},
		"huge protocol":      {Protocol: &Document{Name: "p.pdf", Size: MaxDocumentSize + 1}},
	}
	for name, f := range cases {
		a := &Application{Status: StatusFullForm, Level: 2}
		var ve *ValidationError
		if err := a.ApplyPatch(DraftPatch{FullForm: &f}, t0); !errors.As(err, &ve) {
			t.Fatalf("%s: want ValidationError, got %v", name, err)
		}
	}
}

func validLevel1() Level1 {
	return Level1{ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24, INN: "7701234567",
		Financials: Financials{AnnualRevenue: 80_000_000}}
}

func TestLevel1Validate(t *testing.T) {
	in := validLevel1()
	if _, err := in.Validate(); err != nil || in.Purpose != "Развитие бизнеса" {
		t.Fatalf("valid level 1 rejected or purpose not defaulted: %v %q", err, in.Purpose)
	}
	cases := map[string]func(in *Level1){
		"product_id":                     func(in *Level1) { in.ProductID = "x" },
		"amount":                         func(in *Level1) { in.Amount = 1 },
		"term_months":                    func(in *Level1) { in.TermMonths = 999 },
		"inn":                            func(in *Level1) { in.INN = "12ab" },
		"financials.annual_revenue":      func(in *Level1) { in.Financials.AnnualRevenue = 0 },
		"financials.monthly_obligations": func(in *Level1) { in.Financials.MonthlyObligations = -1 },
	}
	for field, mutate := range cases {
		in := validLevel1()
		mutate(&in)
		var ve *ValidationError
		if _, err := in.Validate(); !errors.As(err, &ve) || ve.Field != field {
			t.Fatalf("%s: want ValidationError on that field, got %v", field, err)
		}
	}
}

func TestPassportValidate(t *testing.T) {
	ok := Passport{Series: "4512", Number: "345678", IssuedAt: "2016-05-24", IssuedBy: "ГУ МВД России", DivisionCode: "770-045"}
	if err := ok.Validate("p", t0); err != nil {
		t.Fatalf("valid passport rejected: %v", err)
	}
	if ok.Masked() != "45 12 ••••78" {
		t.Fatalf("masked: %q", ok.Masked())
	}
	cases := map[string]func(p *Passport){
		"p.series":        func(p *Passport) { p.Series = "45" },
		"p.number":        func(p *Passport) { p.Number = "34567x" },
		"p.issued_at":     func(p *Passport) { p.IssuedAt = "2030-01-01" },
		"p.division_code": func(p *Passport) { p.DivisionCode = "77-45" },
		"p.issued_by":     func(p *Passport) { p.IssuedBy = "" },
	}
	for field, mutate := range cases {
		p := ok
		mutate(&p)
		var ve *ValidationError
		if err := p.Validate("p", t0); !errors.As(err, &ve) || ve.Field != field {
			t.Fatalf("%s: want ValidationError on that field, got %v", field, err)
		}
	}
}

func TestValidINN(t *testing.T) {
	for inn, want := range map[string]bool{"7701234567": true, "222400000012": true, "": false, "123": false, "77012345678": false, "77012345a7": false} {
		if ValidINN(inn) != want {
			t.Fatalf("ValidINN(%q) = %v", inn, !want)
		}
	}
}

// Свойства графика на множестве входов: долг гасится ровно в ноль, проценты неотрицательны,
// сумма графика = Total в Quote, переплата растёт со сроком.
func TestScheduleProperties(t *testing.T) {
	for _, amount := range []int64{50_000, 300_000, 1_234_567, 30_000_000} {
		for _, rate := range []float64{0, 12, 19.9, 23} {
			prevOver := int64(-1)
			for _, n := range []int{1, 3, 12, 24, 60} {
				s := Schedule(amount, rate, n)
				if len(s) != n {
					t.Fatalf("len=%d want %d", len(s), n)
				}
				var principal, total int64
				for i, row := range s {
					if row.Interest < 0 || row.Principal < 0 || row.Payment != row.Principal+row.Interest {
						t.Fatalf("bad row %d: %+v", i, row)
					}
					principal += row.Principal
					total += row.Payment
				}
				if principal != amount || s[n-1].Rest != 0 {
					t.Fatalf("amount=%d rate=%v n=%d: principal sum %d, last rest %d", amount, rate, n, principal, s[n-1].Rest)
				}
				q := NewQuote(amount, rate, n)
				if q.Total != total || q.Overpayment != total-amount {
					t.Fatalf("quote %+v disagrees with schedule total %d", q, total)
				}
				if rate > 0 && q.Overpayment < prevOver {
					t.Fatalf("overpayment must grow with term: %d < %d", q.Overpayment, prevOver)
				}
				prevOver = q.Overpayment
			}
		}
	}
	if Schedule(0, 10, 12) != nil || Schedule(1000, 10, 0) != nil {
		t.Fatal("degenerate inputs must give empty schedule")
	}
}

func TestMaxAmountForPaymentInvertsAnnuity(t *testing.T) {
	for _, n := range []int{6, 24, 84} {
		amt := MaxAmountForPayment(100_000, 21.4, n)
		if pay := AnnuityPayment(amt, 21.4, n); pay > 100_000+1 {
			t.Fatalf("n=%d: amount %d gives payment %d > 100000", n, amt, pay)
		}
	}
	if MaxAmountForPayment(-1, 10, 12) != 0 {
		t.Fatal("negative payment must give zero amount")
	}
}

func TestCalculateLimits(t *testing.T) {
	p, _ := FindProduct("overdraft")
	if _, err := Calculate(p, p.MaxAmount+1, p.MinTerm); err == nil {
		t.Fatal("amount above max must fail")
	}
	if _, err := Calculate(p, p.MinAmount, p.MaxTerm+1); err == nil {
		t.Fatal("term above max must fail")
	}
	if q, err := Calculate(p, p.MinAmount, p.MinTerm); err != nil || q.MonthlyPayment <= 0 {
		t.Fatalf("valid calc failed: %v %+v", err, q)
	}
	for _, x := range Products() {
		if x.DefaultPayment != NewQuote(x.DefaultAmt, x.Rate, x.DefaultTerm).MonthlyPayment {
			t.Fatalf("%s: default payment not from single source", x.ID)
		}
	}
}

func issuedApp() *Application {
	return &Application{Status: StatusFullForm, ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24, INN: "7701234567"}
}

func TestNextPaymentAndEnrich(t *testing.T) {
	a := issuedApp()
	a.Status = StatusIssued
	a.Decision = &Decision{Rate: 18.9}
	a.Enrich()
	if a.Quote == nil || len(a.Schedule) != 24 || a.NextPayment != a.Schedule[0].Payment {
		t.Fatalf("enrich: quote=%v schedule=%d next=%d", a.Quote, len(a.Schedule), a.NextPayment)
	}
	for i := 0; i < 24; i++ {
		a.Payments = append(a.Payments, Payment{N: i + 1})
	}
	if a.NextPaymentAmount() != 0 {
		t.Fatal("no payment after the last one")
	}
	d := issuedApp()
	d.Enrich()
	if d.Schedule != nil || d.NextPayment != 0 {
		t.Fatal("application before issue must not have a schedule")
	}
}

func TestFormatting(t *testing.T) {
	for in, want := range map[int64]string{0: "0 ₽", 999: "999 ₽", 1000: "1 000 ₽", 1_500_000: "1 500 000 ₽"} {
		if got := Money(in); got != want {
			t.Fatalf("Money(%d)=%q want %q", in, got, want)
		}
	}
	for in, want := range map[int64]string{5_000_000: "5 000 000", 18_000_000: "18 000 000", 100: "100"} {
		if got := GroupDigits(in); got != want {
			t.Fatalf("GroupDigits(%d)=%q want %q", in, got, want)
		}
	}
	for in, want := range map[int]string{1: "1 месяц", 2: "2 месяца", 5: "5 месяцев", 11: "11 месяцев", 21: "21 месяц", 24: "24 месяца"} {
		if got := Months(in); got != want {
			t.Fatalf("Months(%d)=%q want %q", in, got, want)
		}
	}
	if Rate(19.9) != "19,9%" {
		t.Fatalf("Rate: %q", Rate(19.9))
	}
}
