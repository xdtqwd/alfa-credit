package pipeline

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/scoring"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

var ctx = context.Background()

var testIdentity = &domain.Identity{FullName: "Смирнов Алексей Викторович",
	Passport: domain.Passport{Series: "4512", Number: "345678", IssuedAt: "2016-05-24", IssuedBy: "ГУ МВД России по г. Москве", DivisionCode: "770-045"}}

// level2Form — минимально заполненный 2-й уровень: паспорт руководителя (для ООО) и согласие.
func level2Form(c *domain.Company) domain.FullForm {
	f := domain.FullForm{Collateral: domain.Collateral{Type: domain.CollateralNone}, Consent: true}
	if c.Type != "ИП" {
		p := testIdentity.Passport
		f.DirectorPassport = &p
	}
	return f
}

func submitted(t *testing.T, repo storage.Repository, inn string, amount, revenue, obligations int64) *domain.Application {
	t.Helper()
	return submittedWith(t, repo, inn, amount, revenue, obligations, nil)
}

// submittedWith — заявка, отправленная на полный скоринг: с предварительным расчётом 1-го уровня
// (тот же скоринг без кредитной истории) и личностью из Госуслуг. full == nil — минимальный 2-й уровень.
func submittedWith(t *testing.T, repo storage.Repository, inn string, amount, revenue, obligations int64, full *domain.FullForm) *domain.Application {
	t.Helper()
	reg := integrations.MockRegistry{}
	c, err := reg.Lookup(ctx, inn)
	if err != nil {
		t.Fatal(err)
	}
	a := &domain.Application{
		ID: "app-" + inn, ClientID: "c", ProductID: "business-dev", Amount: amount, TermMonths: 24, INN: inn, Company: c,
		Financials: domain.Financials{AnnualRevenue: revenue, MonthlyObligations: obligations},
		Level:      2, Identity: testIdentity,
		Status: domain.StatusSubmitted, StatusChangedAt: time.Now().Add(-time.Hour),
	}
	f := level2Form(c)
	if full != nil {
		f = *full
	}
	a.Full = &f
	p, _ := domain.FindProduct(a.ProductID)
	pre := scoring.Prescore(a, p, time.Now()).Decision
	a.Preliminary = &pre
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
	llc := func(edit func(f *domain.FullForm)) *domain.FullForm {
		f := level2Form(&domain.Company{Type: "ООО"})
		edit(&f)
		return &f
	}
	cases := []struct {
		name                 string
		inn                  string
		amount, revenue, obl int64
		full                 *domain.FullForm
		want                 domain.Status
		reasonMustMention    string
	}{
		{"healthy → approved", "7701234567", 3_000_000, 80_000_000, 0, nil, domain.StatusApproved, ""},
		{"too big → counter offer", "2224000012", 20_000_000, 18_000_000, 200_000,
			llc(func(f *domain.FullForm) {
				f.Guarantor = &domain.Guarantor{FullName: "Кузнецов Олег", INN: "222400000123"}
			}), domain.StatusCounterOffer, "50%"},
		{"overdue in БКИ → rejected on full scoring", "5401000009", 3_000_000, 30_000_000, 300_000, nil, domain.StatusRejected, "просрочки"},
		{"liquidated → rejected at data check", "6600000000", 1_000_000, 10_000_000, 0, nil, domain.StatusRejected, "ликвидирована"},
		{"invalid director passport → need info", "7701234567", 3_000_000, 80_000_000, 0,
			llc(func(f *domain.FullForm) { f.DirectorPassport.Number = "000000" }), domain.StatusNeedInfo, "недействителен"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, _ := storage.NewFileStore("")
			w := worker(repo)
			a := run(t, w, submittedWith(t, repo, c.inn, c.amount, c.revenue, c.obl, c.full).ID)
			if a.Status != c.want {
				t.Fatalf("status = %s, want %s; decision %+v", a.Status, c.want, a.Decision)
			}
			if a.Decision == nil || len(a.Decision.Reasons) == 0 {
				t.Fatal("every decision must be explained")
			}
			if c.reasonMustMention != "" && !strings.Contains(strings.ToLower(strings.Join(a.Decision.Reasons, " ")), strings.ToLower(c.reasonMustMention)) {
				t.Fatalf("reasons must mention %q: %q", c.reasonMustMention, a.Decision.Reasons)
			}
			if len(a.Checks) == 0 || !a.Decision.Final {
				t.Fatal("checks must be recorded and the decision be final")
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

// flakyRegistry отвечает ошибкой, пока реестр «лежит».
type flakyRegistry struct {
	integrations.MockRegistry
	down *bool
}

func (r flakyRegistry) Lookup(ctx context.Context, inn string) (*domain.Company, error) {
	if *r.down {
		return nil, errors.New("registry timeout")
	}
	return r.MockRegistry.Lookup(ctx, inn)
}

// Регрессия: недоступный реестр отклонял заявку как «компания не найдена». Теперь шаг повторяется.
func TestRegistryOutageIsRetriedNotRejected(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	w := worker(repo)
	down := true
	w.Registry = flakyRegistry{down: &down}
	a := submitted(t, repo, "7701234567", 3_000_000, 80_000_000, 0)
	if err := w.Step(ctx, a); err != nil { // submitted → data_check
		t.Fatal(err)
	}
	a, _ = repo.Get(ctx, a.ID)
	if err := w.Step(ctx, a); err == nil {
		t.Fatal("registry outage must be returned for retry")
	}
	if a, _ = repo.Get(ctx, a.ID); a.Status != domain.StatusDataCheck {
		t.Fatalf("status = %s, want %s: outage is not a rejection", a.Status, domain.StatusDataCheck)
	}
	down = false
	if a = run(t, w, a.ID); a.Status != domain.StatusApproved {
		t.Fatalf("after registry recovery: status = %s, want %s", a.Status, domain.StatusApproved)
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
