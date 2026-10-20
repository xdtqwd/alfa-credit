// Package scoring — балльная модель оценки риска и кредитная логика.
// Скоринг ≠ решение: балл — один из входов, итог учитывает долговую нагрузку,
// лимиты продукта и правила банка (см. ТЗ, стр. 14–15).
package scoring

import (
	"fmt"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

const (
	baseScore        = 600
	approveThreshold = 650
	rejectThreshold  = 500
	maxDTI           = 0.5 // макс. доля платежей в ежемесячной выручке
)

type Factor struct {
	Name   string `json:"name"`
	Impact int    `json:"impact"`
}

type Result struct {
	Score    int
	Factors  []Factor
	Decision domain.Decision
}

func yearsSince(date string, now time.Time) float64 {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return 0
	}
	return now.Sub(t).Hours() / 24 / 365
}

// Evaluate считает балл и формирует кредитное решение.
func Evaluate(a *domain.Application, p domain.Product, br *domain.BureauReport, now time.Time) Result {
	score := baseScore
	var factors []Factor
	add := func(name string, v int) {
		if v != 0 {
			score += v
			factors = append(factors, Factor{name, v})
		}
	}

	// 1. Срок жизни бизнеса
	if a.Company != nil {
		switch y := yearsSince(a.Company.RegDate, now); {
		case y < 1:
			add("Бизнесу меньше года", -80)
		case y >= 3:
			add("Бизнесу больше 3 лет", 50)
		}
	}

	// 2. Кредитная история
	if br != nil {
		add("Рейтинг БКИ", clamp((br.Score-650)/2, -60, 80))
		if br.HasOverdue {
			add(fmt.Sprintf("Просрочки до %d дней", br.MaxOverdueDays), -150)
		}
		if br.ActiveLoans > 3 {
			add("Много действующих кредитов", -40)
		}
	}

	// 3. Долговая нагрузка
	monthlyRevenue := float64(a.Financials.AnnualRevenue) / 12
	payment := domain.AnnuityPayment(a.Amount, p.Rate, a.TermMonths)
	dti := 1.0
	if monthlyRevenue > 0 {
		dti = float64(a.Financials.MonthlyObligations+payment) / monthlyRevenue
	}
	switch {
	case dti <= 0.3:
		add("Низкая долговая нагрузка", 80)
	case dti <= 0.5:
		add("Умеренная долговая нагрузка", 30)
	case dti <= 0.7:
		add("Высокая долговая нагрузка", -40)
	default:
		add("Очень высокая долговая нагрузка", -120)
	}
	if float64(a.Amount) > float64(a.Financials.AnnualRevenue) {
		add("Сумма больше годовой выручки", -60)
	}

	res := Result{Score: score, Factors: factors}

	switch {
	case score < rejectThreshold:
		res.Decision = domain.Decision{Type: domain.DecisionRejected, Reasons: rejectReasons(factors)}
	case score >= approveThreshold && dti <= maxDTI:
		rate := p.Rate
		reasons := []string{"Финансовые показатели и кредитная история в норме"}
		if score >= 750 {
			rate -= 1.0
			reasons = append(reasons, "Скидка 1 п.п. к ставке за высокий рейтинг")
		}
		res.Decision = domain.Decision{
			Type: domain.DecisionApproved, Amount: a.Amount, TermMonths: a.TermMonths, Rate: rate,
			MonthlyPayment: domain.AnnuityPayment(a.Amount, rate, a.TermMonths), Reasons: reasons,
		}
	default:
		res.Decision = counterOffer(a, p, monthlyRevenue)
	}
	return res
}

// counterOffer подбирает сумму/срок под допустимую долговую нагрузку.
func counterOffer(a *domain.Application, p domain.Product, monthlyRevenue float64) domain.Decision {
	rate := p.Rate + 1.5
	maxPayment := int64(monthlyRevenue*maxDTI) - a.Financials.MonthlyObligations
	for _, term := range []int{a.TermMonths, p.MaxTerm} {
		amt := min(a.Amount, domain.MaxAmountForPayment(maxPayment, rate, term))
		amt = amt / 10_000 * 10_000
		if amt >= p.MinAmount {
			return domain.Decision{
				Type: domain.DecisionCounter, Amount: amt, TermMonths: term, Rate: rate,
				MonthlyPayment: domain.AnnuityPayment(amt, rate, term),
				Reasons: []string{
					"Запрошенный платёж превышает допустимую долговую нагрузку (50% выручки)",
					"Предлагаем условия, при которых платёж комфортен для бизнеса",
				},
			}
		}
	}
	return domain.Decision{Type: domain.DecisionRejected, Reasons: []string{"Текущая долговая нагрузка не позволяет выдать кредит по этому продукту"}}
}

func rejectReasons(f []Factor) []string {
	var out []string
	for _, x := range f {
		if x.Impact < 0 {
			out = append(out, x.Name)
		}
	}
	if len(out) == 0 {
		out = []string{"Недостаточный скоринговый балл"}
	}
	return out
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }
