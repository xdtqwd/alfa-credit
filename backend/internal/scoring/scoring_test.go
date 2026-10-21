package scoring

import (
	"strings"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

var now = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

func app(amount, revenue, obligations int64, term int, regDate string) *domain.Application {
	return &domain.Application{
		ProductID: "business-dev", Amount: amount, TermMonths: term,
		Company:    &domain.Company{RegDate: regDate, Status: "active"},
		Financials: domain.Financials{AnnualRevenue: revenue, MonthlyObligations: obligations},
	}
}

func TestEvaluate(t *testing.T) {
	p, _ := domain.FindProduct("business-dev")
	good := &domain.BureauReport{Score: 760}
	bad := &domain.BureauReport{Score: 540, HasOverdue: true, MaxOverdueDays: 45, ActiveLoans: 4}

	cases := []struct {
		name string
		app  *domain.Application
		br   *domain.BureauReport
		want domain.DecisionType
	}{
		{"healthy business", app(3_000_000, 84_000_000, 0, 24, "2015-03-12"), good, domain.DecisionApproved},
		{"amount too big → counter", app(20_000_000, 18_000_000, 200_000, 24, "2018-07-01"), good, domain.DecisionCounter},
		{"overdue → reject", app(3_000_000, 30_000_000, 300_000, 24, "2010-09-20"), bad, domain.DecisionRejected},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Evaluate(c.app, p, c.br, now)
			if r.Decision.Type != c.want {
				t.Fatalf("got %s (score %d, factors %+v), want %s", r.Decision.Type, r.Score, r.Factors, c.want)
			}
			if r.Decision.Type == domain.DecisionCounter {
				if r.Decision.Amount >= c.app.Amount && r.Decision.TermMonths == c.app.TermMonths {
					t.Fatalf("counter offer must change terms: %+v", r.Decision)
				}
			}
		})
	}
}

func TestAnnuity(t *testing.T) {
	// 1 000 000 ₽ на 12 мес под 12% → 88 849 ₽
	if got := domain.AnnuityPayment(1_000_000, 12, 12); got != 88849 {
		t.Fatalf("annuity = %d", got)
	}
}

func TestTransitions(t *testing.T) {
	if !domain.CanTransition(domain.StatusDraft, domain.StatusSubmitted) {
		t.Fatal("draft → submitted must be allowed")
	}
	if domain.CanTransition(domain.StatusDraft, domain.StatusIssued) {
		t.Fatal("draft → issued must be forbidden")
	}
}

// Единый расчёт: сумма графика = Total, последний платёж закрывает долг в ноль.
func TestQuoteConsistency(t *testing.T) {
	q := domain.NewQuote(3_000_000, 19.9, 24)
	s := domain.Schedule(3_000_000, 19.9, 24)
	var sum int64
	for _, i := range s {
		sum += i.Payment
	}
	if sum != q.Total || s[len(s)-1].Rest != 0 || q.Overpayment != q.Total-3_000_000 {
		t.Fatalf("quote %+v, schedule sum %d, last rest %d", q, sum, s[len(s)-1].Rest)
	}
}

func TestCounterOfferExplainsWithFormData(t *testing.T) {
	p, _ := domain.FindProduct("business-dev")
	a := app(20_000_000, 18_000_000, 200_000, 24, "2018-07-01")
	r := Evaluate(a, p, &domain.BureauReport{Score: 760}, now)
	d := r.Decision
	if d.Type != domain.DecisionCounter || d.Requested == nil || d.Requested.Amount != 20_000_000 || d.Overpayment <= 0 {
		t.Fatalf("decision %+v", d)
	}
	found := false
	for _, s := range d.Reasons {
		if strings.Contains(s, "1 500 000 ₽") { // месячная выручка из анкеты
			found = true
		}
	}
	if !found {
		t.Fatalf("reasons must cite form data: %q", d.Reasons)
	}
}
