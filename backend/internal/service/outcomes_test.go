package service

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/pipeline"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

// Каждый эталонный кейс проходит путь клиента: предварительный расчёт (1-й уровень) и, если он
// пропускает дальше, — заявка с документами через настоящий конвейер (моки ЕГРЮЛ/ФНС/БКИ + полный скоринг).
// Ожидаемый исход и уровень, на котором он выносится, должны совпасть с фактическими.
func TestScenariosProduceExpectedOutcome(t *testing.T) {
	ctx := context.Background()
	inn := map[string]string{}
	for _, d := range DemoAccounts {
		inn[d.Phone] = d.INN
	}
	person := map[string]string{}
	for _, p := range (integrations.MockESIA{}).Persons(ctx) {
		person[p.Phone] = p.ID
	}
	seen := map[string]bool{}
	for _, sc := range Scenarios {
		t.Run(sc.ID, func(t *testing.T) {
			if seen[sc.ID] {
				t.Fatalf("duplicate id %s", sc.ID)
			}
			seen[sc.ID] = true
			p, ok := domain.FindProduct(sc.Product)
			if !ok {
				t.Fatalf("unknown product %s", sc.Product)
			}
			if sc.Amount < p.MinAmount || sc.Amount > p.MaxAmount || sc.Term < p.MinTerm || sc.Term > p.MaxTerm {
				t.Fatalf("scenario params outside product limits: %+v", sc)
			}
			companyINN, ok := inn[sc.Phone]
			if !ok {
				t.Fatalf("phone %s is not a demo account", sc.Phone)
			}
			c, err := integrations.MockRegistry{}.Lookup(ctx, companyINN)
			if err != nil {
				t.Fatal(err)
			}
			if c.Name != sc.Company {
				t.Fatalf("company = %q, scenario says %q", c.Name, sc.Company)
			}
			wantTone := map[string]string{"approved": "positive", "counter_offer": "neutral", "need_info": "neutral", "rejected": "negative"}[sc.Outcome]
			if sc.Tone != wantTone {
				t.Fatalf("tone %s for outcome %s", sc.Tone, sc.Outcome)
			}

			repo, _ := storage.NewFileStore("")
			svc := &Service{Repo: repo, Registry: integrations.MockRegistry{}, Now: time.Now}
			in := domain.Level1{ProductID: sc.Product, Amount: sc.Amount, TermMonths: sc.Term, INN: companyINN,
				Financials: domain.Financials{AnnualRevenue: sc.Revenue, MonthlyObligations: sc.Monthly}}
			pre, err := svc.Prescore(ctx, &in)
			if err != nil {
				t.Fatal(err)
			}
			passed := pre.Decision.Type == domain.DecisionApproved || pre.Decision.Type == domain.DecisionCounter
			if sc.Level == 1 {
				if string(pre.Decision.Type) != sc.Outcome {
					t.Fatalf("level 1 outcome = %s, scenario promises %s", pre.Decision.Type, sc.Outcome)
				}
				if sc.Outcome == "counter_offer" && pre.Decision.Amount >= sc.Amount {
					t.Fatalf("контрпредложение должно уменьшать сумму: %d", pre.Decision.Amount)
				}
				return
			}
			if !passed {
				t.Fatalf("prescore must let the scenario to level 2: %+v", pre.Decision)
			}

			pp, _ := (integrations.MockESIA{}).Person(ctx, person[sc.Phone])
			a := &domain.Application{ID: "s-" + sc.ID, ClientID: "c", ProductID: sc.Product, Amount: sc.Amount, TermMonths: sc.Term,
				INN: companyINN, Company: c, Financials: in.Financials, Checks: pre.Checks}
			a.StartLevel2(pre.Decision, &pp.Identity)
			a.Full.Consent = true
			if sc.Collateral > 0 {
				a.Full.Collateral = domain.Collateral{Type: domain.CollateralRealty, Value: sc.Collateral}
			}
			if sc.Guarantor != "" {
				a.Full.Guarantor = &domain.Guarantor{FullName: "Поручитель Тестовый", INN: sc.Guarantor}
			}
			if err := a.ValidateFull(time.Now()); err != nil {
				t.Fatalf("scenario level 2 is incomplete: %v", err)
			}
			a.Status, a.StatusChangedAt = domain.StatusSubmitted, time.Now().Add(-time.Hour)
			a, _ = repo.Create(ctx, a)
			w := &pipeline.Worker{Repo: repo, Registry: integrations.MockRegistry{}, Bureau: integrations.MockBureau{},
				Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
			for i := 0; i < 5 && a.Status.InPipeline(); i++ {
				if err := w.Step(ctx, a); err != nil {
					t.Fatal(err)
				}
				a, _ = repo.Get(ctx, a.ID)
			}
			if string(a.Status) != sc.Outcome {
				t.Fatalf("outcome = %s, scenario promises %s (score %v, decision %+v)", a.Status, sc.Outcome, a.Score, a.Decision)
			}
			if sc.ID == "approve-best" && !(a.Decision.Rate < p.Rate) {
				t.Fatalf("сценарий обещает сниженную ставку: %v vs %v", a.Decision.Rate, p.Rate)
			}
			if sc.ID == "approve-collateral" && !(a.Decision.Rate < a.Preliminary.Rate-1) {
				t.Fatalf("залог на всю сумму должен снизить ставку ещё на 1 п.п.: %v vs %v", a.Decision.Rate, a.Preliminary.Rate)
			}
			if sc.Outcome == "counter_offer" && a.Decision.Amount >= sc.Amount {
				t.Fatalf("контрпредложение должно уменьшать сумму: %d", a.Decision.Amount)
			}
			if a.Score != nil && len(a.Factors) == 0 {
				t.Fatal("scored application must keep its factors")
			}
		})
	}
}
