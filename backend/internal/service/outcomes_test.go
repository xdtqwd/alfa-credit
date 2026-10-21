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

// Каждый эталонный кейс прогоняется через настоящий конвейер (моки ЕГРЮЛ/ФНС/БКИ + скоринг) —
// ожидаемый исход должен совпасть с фактическим.
func TestScenariosProduceExpectedOutcome(t *testing.T) {
	ctx := context.Background()
	inn := map[string]string{}
	for _, d := range DemoAccounts {
		inn[d.Phone] = d.INN
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
			repo, _ := storage.NewFileStore("")
			a, _ := repo.Create(ctx, &domain.Application{
				ID: "s-" + sc.ID, ClientID: "c", ProductID: sc.Product, Amount: sc.Amount, TermMonths: sc.Term, INN: companyINN, Company: c,
				Financials: domain.Financials{AnnualRevenue: sc.Revenue, MonthlyObligations: sc.Monthly},
				Status:     domain.StatusSubmitted, StatusChangedAt: time.Now().Add(-time.Hour),
			})
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
			wantTone := map[string]string{"approved": "positive", "counter_offer": "neutral", "need_info": "neutral", "rejected": "negative"}[sc.Outcome]
			if sc.Tone != wantTone {
				t.Fatalf("tone %s for outcome %s", sc.Tone, sc.Outcome)
			}
			if sc.ID == "approve-best" && !(a.Decision.Rate < p.Rate) {
				t.Fatalf("сценарий обещает сниженную ставку: %v vs %v", a.Decision.Rate, p.Rate)
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
