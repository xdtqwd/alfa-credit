package pipeline

import (
	"strings"
	"testing"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

// preliminary проводит быструю анкету через конвейер и открывает полную (как Service.Accept).
func preliminary(t *testing.T, repo storage.Repository, w *Worker, amount int64, full domain.FullForm) *domain.Application {
	t.Helper()
	a := run(t, w, submitted(t, repo, "7701234567", amount, 80_000_000, 0).ID)
	if a.Status != domain.StatusApproved || a.Decision.Final {
		t.Fatalf("level 1 must give a preliminary approval: %s final=%v", a.Status, a.Decision.Final)
	}
	a, err := repo.Update(ctx, a.ID, func(x *domain.Application) error {
		if err := x.Transition(domain.StatusFullForm, "client", "", w.Now()); err != nil {
			return err
		}
		x.StartFullForm("")
		x.Full = &full
		if err := x.ValidateFull(); err != nil {
			return err
		}
		return x.Transition(domain.StatusVerification, "client", "", w.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
	return run(t, w, a.ID)
}

func fullForm(c domain.Collateral, g *domain.Guarantor) domain.FullForm {
	return domain.FullForm{Employees: 25, Premises: "own", PurposeDetails: "Открытие второго цеха и найм сотрудников",
		Collateral: c, Guarantor: g, StatementsConsent: true}
}

func TestFullFormFinalDecision(t *testing.T) {
	none := domain.Collateral{Type: domain.CollateralNone}
	cases := []struct {
		name     string
		amount   int64
		form     domain.FullForm
		want     domain.Status
		rateDiff float64 // окончательная ставка минус предварительная
		mention  string
	}{
		{"small loan without security → final approval on the same terms", 3_000_000, fullForm(none, nil), domain.StatusApproved, 0, "подтверждаем"},
		{"full collateral → rate −1 p.p.", 8_000_000, fullForm(domain.Collateral{Type: domain.CollateralRealty, Value: 15_000_000}, nil), domain.StatusApproved, -1, "ниже на 1 п.п."},
		{"clean guarantor covers a big loan", 8_000_000, fullForm(none, &domain.Guarantor{FullName: "Смирнов Алексей", INN: "770123456781"}), domain.StatusApproved, 0, "Поручительство"},
		{"guarantor with overdue and no collateral → need info", 8_000_000, fullForm(none, &domain.Guarantor{FullName: "Петров Игорь", INN: "540100000009"}), domain.StatusNeedInfo, 0, "просрочки"},
		{"weak collateral → need info", 8_000_000, fullForm(domain.Collateral{Type: domain.CollateralEquipment, Value: 2_000_000}, nil), domain.StatusNeedInfo, 0, "не хватает"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, _ := storage.NewFileStore("")
			w := worker(repo)
			a := preliminary(t, repo, w, c.amount, c.form)
			if a.Status != c.want {
				t.Fatalf("status = %s, want %s; %+v", a.Status, c.want, a.Decision)
			}
			if !strings.Contains(strings.Join(a.Decision.Reasons, " "), c.mention) {
				t.Fatalf("reasons must mention %q: %v", c.mention, a.Decision.Reasons)
			}
			if c.want == domain.StatusApproved {
				if !a.Decision.Final || a.Decision.Requested == nil {
					t.Fatal("final decision must be marked and compared with the preliminary one")
				}
				if diff := a.Decision.Rate - a.Preliminary.Rate; diff != c.rateDiff {
					t.Fatalf("rate diff = %.1f, want %.1f", diff, c.rateDiff)
				}
			}
			codes := map[string]bool{}
			for _, ch := range a.Checks {
				codes[ch.Code] = true
			}
			if !codes["registry"] || !codes["full_form"] || !codes["collateral"] {
				t.Fatalf("level-1 checks must stay and level-2 checks be added: %+v", a.Checks)
			}
		})
	}
}

func TestOverdraftIsSingleLevel(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	w := worker(repo)
	a := submitted(t, repo, "7701234567", 500_000, 80_000_000, 0)
	repo.Update(ctx, a.ID, func(x *domain.Application) error { x.ProductID, x.TermMonths = "overdraft", 12; return nil })
	a = run(t, w, a.ID)
	if a.Status != domain.StatusApproved || !a.Decision.Final {
		t.Fatalf("overdraft decision must be final at once: %s final=%v", a.Status, a.Decision.Final)
	}
}
