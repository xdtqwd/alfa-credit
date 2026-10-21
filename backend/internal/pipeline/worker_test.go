package pipeline

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

var ctx = context.Background()

func submitted(t *testing.T, repo storage.Repository, inn string, amount, revenue, obligations int64) *domain.Application {
	t.Helper()
	reg := integrations.MockRegistry{}
	c, err := reg.Lookup(ctx, inn)
	if err != nil {
		t.Fatal(err)
	}
	a := &domain.Application{
		ID: "app-" + inn, ClientID: "c", ProductID: "business-dev", Amount: amount, TermMonths: 24, INN: inn, Company: c,
		Financials: domain.Financials{AnnualRevenue: revenue, MonthlyObligations: obligations},
		Status:     domain.StatusSubmitted, StatusChangedAt: time.Now().Add(-time.Hour),
	}
	a, err = repo.Create(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func run(t *testing.T, w *Worker, id string) *domain.Application {
	t.Helper()
	for i := 0; i < 5; i++ {
		a, _ := w.Repo.Get(ctx, id)
		if !a.Status.InPipeline() {
			return a
		}
		if err := w.Step(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := w.Repo.Get(ctx, id)
	return a
}

func worker(repo storage.Repository) *Worker {
	return &Worker{Repo: repo, Registry: integrations.MockRegistry{}, Bureau: integrations.MockBureau{},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now, StepDelay: 0, Interval: time.Millisecond}
}

func TestPipelineScenarios(t *testing.T) {
	cases := []struct {
		name                 string
		inn                  string
		amount, revenue, obl int64
		want                 domain.Status
		reasonMustMention    string
	}{
		{"healthy → approved", "7701234567", 3_000_000, 80_000_000, 0, domain.StatusApproved, ""},
		{"too big → counter offer", "2224000012", 20_000_000, 18_000_000, 200_000, domain.StatusCounterOffer, "50%"},
		{"revenue ≫ ФНС → need info", "2224000012", 3_000_000, 50_000_000, 0, domain.StatusNeedInfo, "18 000 000 ₽"},
		{"overdue → rejected", "5401000009", 3_000_000, 30_000_000, 300_000, domain.StatusRejected, "просрочки"},
		{"liquidated → rejected at data check", "6600000000", 1_000_000, 10_000_000, 0, domain.StatusRejected, "ликвидирована"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, _ := storage.NewFileStore("")
			w := worker(repo)
			a := run(t, w, submitted(t, repo, c.inn, c.amount, c.revenue, c.obl).ID)
			if a.Status != c.want {
				t.Fatalf("status = %s, want %s; decision %+v", a.Status, c.want, a.Decision)
			}
			if a.Decision == nil || len(a.Decision.Reasons) == 0 {
				t.Fatal("every decision must be explained")
			}
			if c.reasonMustMention != "" && !strings.Contains(strings.ToLower(strings.Join(a.Decision.Reasons, " ")), strings.ToLower(c.reasonMustMention)) {
				t.Fatalf("reasons must mention %q: %q", c.reasonMustMention, a.Decision.Reasons)
			}
			if len(a.Checks) == 0 {
				t.Fatal("checks must be recorded")
			}
			if c.want == domain.StatusCounterOffer && (a.Decision.Requested == nil || a.Decision.Amount >= c.amount && a.Decision.TermMonths == 24) {
				t.Fatalf("counter offer must change terms and keep requested quote: %+v", a.Decision)
			}
			// аудит: каждый шаг конвейера записан в историю
			if len(a.History) < 2 {
				t.Fatalf("history: %+v", a.History)
			}
		})
	}
}

func TestStepDelayRespected(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	w := worker(repo)
	w.StepDelay = time.Hour
	a := submitted(t, repo, "7701234567", 3_000_000, 80_000_000, 0)
	repo.Update(ctx, a.ID, func(x *domain.Application) error { x.StatusChangedAt = time.Now(); return nil })
	w.tick(ctx)
	got, _ := repo.Get(ctx, a.ID)
	if got.Status != domain.StatusSubmitted {
		t.Fatal("worker must wait StepDelay before advancing")
	}
	w.StepDelay = 0
	w.tick(ctx)
	got, _ = repo.Get(ctx, a.ID)
	if got.Status != domain.StatusDataCheck {
		t.Fatalf("after delay status = %s", got.Status)
	}
}

// Если клиент отменил заявку, пока шёл шаг, конвейер не должен её «оживлять».
func TestConcurrentCancelIsNotOverwritten(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	w := worker(repo)
	a := submitted(t, repo, "7701234567", 3_000_000, 80_000_000, 0)
	stale, _ := repo.Get(ctx, a.ID) // снимок до отмены
	repo.Update(ctx, a.ID, func(x *domain.Application) error {
		return x.Transition(domain.StatusCancelled, "client", "", time.Now())
	})
	if err := w.Step(ctx, stale); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(ctx, a.ID)
	if got.Status != domain.StatusCancelled {
		t.Fatalf("cancelled application was advanced to %s", got.Status)
	}
}
