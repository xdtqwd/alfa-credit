package domain

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// Каждый разрешённый переход проходит, всё остальное — ErrInvalidTransition.
func TestTransitionsExhaustive(t *testing.T) {
	all := []Status{StatusDraft, StatusSubmitted, StatusDataCheck, StatusScoring, StatusApproved, StatusCounterOffer,
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

func TestApplyPatchOnlyInEditableStatuses(t *testing.T) {
	amt := int64(1_000_000)
	for _, s := range []Status{StatusSubmitted, StatusApproved, StatusIssued} {
		a := &Application{Status: s}
		if err := a.ApplyPatch(DraftPatch{Amount: &amt}, t0); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("patch in %s must be forbidden, got %v", s, err)
		}
	}
	a := &Application{Status: StatusNeedInfo}
	if err := a.ApplyPatch(DraftPatch{Amount: &amt}, t0); err != nil || a.Amount != amt {
		t.Fatalf("need_info must be editable: %v", err)
	}
}

func TestApplyPatchValidation(t *testing.T) {
	bad := "nope"
	neg := int64(-1)
	cases := map[string]DraftPatch{
		"unknown product":  {ProductID: &bad},
		"negative amount":  {Amount: &neg},
		"negative revenue": {Financials: &Financials{AnnualRevenue: -5}},
	}
	for name, p := range cases {
		a := &Application{Status: StatusDraft}
		var ve *ValidationError
		if err := a.ApplyPatch(p, t0); !errors.As(err, &ve) {
			t.Fatalf("%s: want ValidationError, got %v", name, err)
		}
	}
}

func validApp() *Application {
	return &Application{
		Status: StatusDraft, ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24, INN: "7701234567",
		Company: &Company{INN: "7701234567"}, Financials: Financials{AnnualRevenue: 80_000_000},
		Contact: Contact{FullName: "Иванов Иван", Phone: "+79990001122"}, Consent: true,
	}
}

func TestValidateForSubmitFields(t *testing.T) {
	if err := validApp().ValidateForSubmit(); err != nil {
		t.Fatalf("valid application rejected: %v", err)
	}
	cases := map[string]func(a *Application){
		"product_id":                func(a *Application) { a.ProductID = "x" },
		"amount":                    func(a *Application) { a.Amount = 1 },
		"term_months":               func(a *Application) { a.TermMonths = 999 },
		"inn":                       func(a *Application) { a.INN = "12ab" },
		"financials.annual_revenue": func(a *Application) { a.Financials.AnnualRevenue = 0 },
		"contact.full_name":         func(a *Application) { a.Contact.FullName = "Ив" },
		"contact.phone":             func(a *Application) { a.Contact.Phone = "123" },
		"consent":                   func(a *Application) { a.Consent = false },
	}
	for field, mutate := range cases {
		a := validApp()
		mutate(a)
		var ve *ValidationError
		if err := a.ValidateForSubmit(); !errors.As(err, &ve) || ve.Field != field {
			t.Fatalf("%s: want ValidationError on that field, got %v", field, err)
		}
	}
	a := validApp()
	a.Company = nil
	if err := a.ValidateForSubmit(); err == nil {
		t.Fatal("application without registry company must not be submitted")
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

func TestNextPaymentAndEnrich(t *testing.T) {
	a := validApp()
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
	d := validApp()
	d.Enrich()
	if d.Schedule != nil || d.NextPayment != 0 {
		t.Fatal("draft must not have a schedule")
	}
}

func TestFormatting(t *testing.T) {
	for in, want := range map[int64]string{0: "0 ₽", 999: "999 ₽", 1000: "1 000 ₽", 1_500_000: "1 500 000 ₽"} {
		if got := Money(in); got != want {
			t.Fatalf("Money(%d)=%q want %q", in, got, want)
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
