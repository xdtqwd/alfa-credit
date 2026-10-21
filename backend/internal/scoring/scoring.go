// Package scoring — балльная модель оценки риска и кредитная логика.
// Скоринг ≠ решение: балл — один из входов, итог учитывает долговую нагрузку,
// лимиты продукта и правила банка (см. ТЗ, стр. 14–15).
// Каждый фактор сопровождается объяснением простыми словами на данных анкеты.
package scoring

import (
	"fmt"
	"strings"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

const (
	baseScore        = 600
	approveThreshold = 650
	rejectThreshold  = 500
	maxDTI           = 0.5 // макс. доля платежей в ежемесячной выручке
	counterMarkup    = 1.5 // надбавка к ставке при контрпредложении, п.п.
)

// Factor — фактор балла (для клиента, с цифрами из анкеты); хранится в заявке.
type Factor = domain.ScoreFactor

// Пороги решения — экспортируются, чтобы клиент и банк видели шкалу.
const (
	ApproveThreshold = approveThreshold
	RejectThreshold  = rejectThreshold
	BaseScore        = baseScore
)

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

func pct(v float64) string { return fmt.Sprintf("%.0f%%", v*100) }

// Evaluate считает балл и формирует кредитное решение.
func Evaluate(a *domain.Application, p domain.Product, br *domain.BureauReport, now time.Time) Result {
	score := baseScore
	var factors []Factor
	add := func(name string, v int, explain string) {
		score += v
		factors = append(factors, Factor{Name: name, Impact: v, Explain: explain})
	}

	// 1. Срок жизни бизнеса
	if a.Company != nil {
		switch y := yearsSince(a.Company.RegDate, now); {
		case y < 1:
			add("Бизнесу меньше года", -80, "Компании меньше года — у нас пока мало истории, чтобы оценить её устойчивость.")
		case y >= 3:
			add("Бизнесу больше 3 лет", 50, fmt.Sprintf("Компания работает %s — это говорит об устойчивом бизнесе.", domain.Years(y)))
		}
	}

	// 2. Кредитная история
	if br != nil {
		if v := clamp((br.Score-650)/2, -60, 80); v != 0 {
			if v > 0 {
				add("Рейтинг БКИ", v, "Хорошая кредитная история: прошлые кредиты обслуживались без проблем.")
			} else {
				add("Рейтинг БКИ", v, "Рейтинг в бюро кредитных историй ниже среднего.")
			}
		}
		if br.HasOverdue {
			add(fmt.Sprintf("Просрочки до %d дней", br.MaxOverdueDays), -150,
				fmt.Sprintf("В кредитной истории есть просрочки платежей до %d дней — это главный фактор риска.", br.MaxOverdueDays))
		}
		if br.ActiveLoans > 3 {
			add("Много действующих кредитов", -40, fmt.Sprintf("Сейчас у компании %d действующих кредитов — нагрузка уже заметная.", br.ActiveLoans))
		}
	}

	// 3. Долговая нагрузка (DTI) — на цифрах из анкеты
	monthlyRevenue := float64(a.Financials.AnnualRevenue) / 12
	requested := domain.NewQuote(a.Amount, p.Rate, a.TermMonths)
	dti := 1.0
	if monthlyRevenue > 0 {
		dti = float64(a.Financials.MonthlyObligations+requested.MonthlyPayment) / monthlyRevenue
	}
	load := fmt.Sprintf("Платёж по новому кредиту %s", domain.Money(requested.MonthlyPayment))
	if a.Financials.MonthlyObligations > 0 {
		load += fmt.Sprintf(" вместе с текущими платежами %s", domain.Money(a.Financials.MonthlyObligations))
	}
	load += fmt.Sprintf(" займёт %s месячной выручки (%s в месяц).", pct(dti), domain.Money(int64(monthlyRevenue)))
	switch {
	case dti <= 0.3:
		add("Низкая долговая нагрузка", 80, load+" Это комфортная нагрузка.")
	case dti <= 0.5:
		add("Умеренная долговая нагрузка", 30, load+" Это в пределах нормы — до 50%.")
	case dti <= 0.7:
		add("Высокая долговая нагрузка", -40, load+" Это больше допустимых 50%: платить будет тяжело.")
	default:
		add("Очень высокая долговая нагрузка", -120, load+" Это намного больше допустимых 50%.")
	}
	if a.Amount > a.Financials.AnnualRevenue {
		add("Сумма больше годовой выручки", -60, fmt.Sprintf("Запрошенная сумма %s больше выручки компании за год (%s).", domain.Money(a.Amount), domain.Money(a.Financials.AnnualRevenue)))
	}

	res := Result{Score: score, Factors: factors}
	explain := func(sign int) []string {
		var out []string
		for _, f := range factors {
			if f.Impact*sign > 0 || (sign > 0 && f.Impact == 0) {
				out = append(out, f.Explain)
			}
		}
		return out
	}

	switch {
	case score < rejectThreshold:
		reasons := explain(-1)
		if len(reasons) == 0 {
			reasons = []string{"Совокупная оценка риска ниже порога банка."}
		}
		d := domain.Decision{Type: domain.DecisionRejected, Reasons: reasons, Requested: &requested}
		d.Advice = rejectAdvice(a, factors)
		res.Decision = d
	case score >= approveThreshold && dti <= maxDTI:
		rate := p.Rate
		reasons := append([]string{"Всё в порядке — одобряем на тех условиях, которые вы запросили."}, explain(+1)...)
		if score >= 750 {
			rate -= 1.0
			reasons = append(reasons, fmt.Sprintf("За высокую оценку снизили ставку на 1 п.п.: %s вместо %s.", domain.Rate(rate), domain.Rate(p.Rate)))
		}
		d := domain.Decision{Type: domain.DecisionApproved, Reasons: reasons, Requested: &requested}
		d.SetOffer(domain.NewQuote(a.Amount, rate, a.TermMonths))
		res.Decision = d
	default:
		res.Decision = counterOffer(a, p, monthlyRevenue, requested, explain(-1))
	}
	return res
}

// counterOffer подбирает сумму/срок под допустимую долговую нагрузку и объясняет, что изменилось.
func counterOffer(a *domain.Application, p domain.Product, monthlyRevenue float64, requested domain.Quote, why []string) domain.Decision {
	rate := p.Rate + counterMarkup
	maxPayment := int64(monthlyRevenue*maxDTI) - a.Financials.MonthlyObligations
	for _, term := range []int{a.TermMonths, p.MaxTerm} {
		amt := min(a.Amount, domain.MaxAmountForPayment(maxPayment, rate, term))
		amt = amt / 10_000 * 10_000
		if amt < p.MinAmount {
			continue
		}
		offer := domain.NewQuote(amt, rate, term)
		var what []string
		if amt < a.Amount {
			what = append(what, fmt.Sprintf("уменьшили сумму до %s", domain.Money(amt)))
		}
		if term != a.TermMonths {
			what = append(what, fmt.Sprintf("увеличили срок до %s", domain.Months(term)))
		}
		change := "подобрали условия"
		if len(what) > 0 {
			change = joinRu(what)
		}
		reasons := append(why,
			fmt.Sprintf("Чтобы платёж уложился в 50%% выручки, мы %s. Новый платёж — %s в месяц.", change, domain.Money(offer.MonthlyPayment)),
			fmt.Sprintf("Ставка выше базовой на %s п.п. (%s вместо %s) из-за повышенной нагрузки.", strings.Replace(fmt.Sprintf("%.1f", counterMarkup), ".", ",", 1), domain.Rate(rate), domain.Rate(p.Rate)),
		)
		d := domain.Decision{Type: domain.DecisionCounter, Reasons: reasons, Requested: &requested}
		d.SetOffer(offer)
		return d
	}
	return domain.Decision{
		Type: domain.DecisionRejected, Requested: &requested,
		Reasons: append(why, fmt.Sprintf("Даже на максимальный срок %s платёж не укладывается в 50%% выручки с учётом текущих платежей.", domain.Months(p.MaxTerm))),
		Advice:  []string{"Погасите часть текущих кредитов и подайте заявку снова.", "Попробуйте овердрафт — он подходит для небольших сумм на короткий срок."},
	}
}

func rejectAdvice(a *domain.Application, f []Factor) []string {
	var out []string
	for _, x := range f {
		switch x.Name {
		case "Очень высокая долговая нагрузка", "Высокая долговая нагрузка", "Сумма больше годовой выручки":
			out = append(out, fmt.Sprintf("Попробуйте меньшую сумму — например, %s.", domain.Money(a.Amount/2/10_000*10_000)))
		case "Бизнесу меньше года":
			out = append(out, "Через несколько месяцев работы шансы на одобрение вырастут.")
		}
		if len(x.Name) > 0 && x.Impact <= -150 {
			out = append(out, "Закройте просроченные платежи — после этого кредитная история начнёт улучшаться.")
		}
	}
	if len(out) == 0 {
		out = []string{"Подайте заявку на меньшую сумму или на другой продукт."}
	}
	return out
}

func joinRu(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	s := xs[0]
	for _, x := range xs[1 : len(xs)-1] {
		s += ", " + x
	}
	return s + " и " + xs[len(xs)-1]
}

func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }
