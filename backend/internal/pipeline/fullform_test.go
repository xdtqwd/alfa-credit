package pipeline

import (
	"strings"
	"testing"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

func fullForm(c domain.Collateral, g *domain.Guarantor) *domain.FullForm {
	f := level2Form(&domain.Company{Type: "ООО"})
	f.Collateral, f.Guarantor = c, g
	return &f
}

// Полный скоринг: кредитная история и обеспечение меняют предварительные условия.
func TestFullScoringFinalDecision(t *testing.T) {
	none := domain.Collateral{Type: domain.CollateralNone}
	cases := []struct {
		name     string
		amount   int64
		form     *domain.FullForm
		want     domain.Status
		rateDiff float64 // окончательная ставка минус предварительная
		mention  string
	}{
		{"good credit history lowers the rate", 3_000_000, fullForm(none, nil), domain.StatusApproved, -1, "высокую оценку"},
		{"full collateral → another −1 p.p.", 8_000_000, fullForm(domain.Collateral{Type: domain.CollateralRealty, Value: 15_000_000}, nil), domain.StatusApproved, -2, "Залог полностью покрывает"},
		{"clean guarantor covers a big loan", 8_000_000, fullForm(none, &domain.Guarantor{FullName: "Смирнов Алексей", INN: "770123456781"}), domain.StatusApproved, -1, "Поручительство"},
		{"guarantor with overdue and no collateral → need info", 8_000_000, fullForm(none, &domain.Guarantor{FullName: "Петров Игорь", INN: "540100000009"}), domain.StatusNeedInfo, 0, "просрочки"},
		{"weak collateral → need info", 8_000_000, fullForm(domain.Collateral{Type: domain.CollateralEquipment, Value: 2_000_000}, nil), domain.StatusNeedInfo, 0, "не хватает"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo, _ := storage.NewFileStore("")
			w := worker(repo)
			a := run(t, w, submittedWith(t, repo, "7701234567", c.amount, 80_000_000, 0, c.form).ID)
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
			if !codes["registry"] || !codes["bki"] || !codes["esia"] || !codes["passport"] || !codes["scoring"] {
				t.Fatalf("level-2 checks must be recorded: %+v", a.Checks)
			}
		})
	}
}

// Протокол об одобрении крупной сделки попадает в проверки банка.
func TestProtocolIsChecked(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	w := worker(repo)
	f := fullForm(domain.Collateral{Type: domain.CollateralRealty, Value: 60_000_000}, nil)
	f.Protocol = &domain.Document{Name: "Протокол ОСУ №3.pdf", Size: 120_000}
	a := run(t, w, submittedWith(t, repo, "7701234567", 26_000_000, 84_000_000, 0, f).ID)
	for _, c := range a.Checks {
		if c.Code == "protocol" && c.Result == "ok" && strings.Contains(c.Details, "Протокол ОСУ") {
			return
		}
	}
	t.Fatalf("protocol check missing: %s %+v", a.Status, a.Checks)
}
