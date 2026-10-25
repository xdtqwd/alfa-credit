package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/scoring"
)

// Prescore — предварительный расчёт 1-го уровня.
type Prescore struct {
	Company  *domain.Company      `json:"company,omitempty"`
	Checks   []domain.Check       `json:"checks"`
	Score    *int                 `json:"score,omitempty"`
	Factors  []domain.ScoreFactor `json:"factors,omitempty"`
	Decision domain.Decision      `json:"decision"`
}

// prescoreNote — почему решение предварительное и что изменится на 2-м уровне.
const prescoreNote = "Расчёт предварительный — без кредитной истории: её банк запросит после входа через Госуслуги, и на полном скоринге условия могут измениться."

// Prescore — упрощённый скоринг 1-го уровня, без входа и без заявки: компания в реестре,
// сверка выручки с ФНС и долговая нагрузка. Кредитную историю (БКИ) без согласия
// идентифицированного заявителя не запрашиваем — она войдёт в полный скоринг 2-го уровня.
func (s *Service) Prescore(ctx context.Context, in *domain.Level1) (*Prescore, error) {
	p, err := in.Validate()
	if err != nil {
		return nil, err
	}
	company, err := s.Registry.Lookup(ctx, in.INN)
	res := &Prescore{Company: company, Checks: []domain.Check{}}
	reject := func(check domain.Check, reason, advice string) (*Prescore, error) {
		res.Checks = append(res.Checks, check)
		res.Decision = domain.Decision{Type: domain.DecisionRejected, Reasons: []string{reason}, Advice: []string{advice}}
		obs.Default.Inc("prescore_total", "Предварительные расчёты 1-го уровня", "decision", "rejected")
		return res, nil
	}
	switch {
	case errors.Is(err, domain.ErrCompanyNotFound):
		return reject(domain.Check{Code: "registry", Name: "ЕГРЮЛ/ЕГРИП", Result: "fail", Details: "Компания не найдена"},
			"Мы не нашли компанию с ИНН "+in.INN+" в государственном реестре (ЕГРЮЛ/ЕГРИП).", "Проверьте ИНН и рассчитайте снова.")
	case err != nil:
		return nil, err // некорректный ИНН — 422, недоступный реестр — 5xx
	case company.Status != "active":
		return reject(domain.Check{Code: "registry", Name: "ЕГРЮЛ/ЕГРИП", Result: "fail", Details: "Компания ликвидирована"},
			"По данным государственного реестра (ЕГРЮЛ) компания "+company.Name+" ликвидирована. Кредит можно выдать только действующей компании.",
			"Если компания действует, а в реестре ошибка — обратитесь в налоговую и рассчитайте снова после исправления.")
	}
	res.Checks = append(res.Checks, domain.Check{Code: "registry", Name: "ЕГРЮЛ/ЕГРИП", Result: "ok", Details: "Компания действует, регистрация с " + company.RegDate})

	if company.RevenueLastYear > 0 && float64(in.Financials.AnnualRevenue) > 1.5*float64(company.RevenueLastYear) {
		res.Checks = append(res.Checks, domain.Check{Code: "revenue", Name: "Сверка выручки с ФНС", Result: "warn",
			Details: fmt.Sprintf("Указано %s, по данным ФНС — %s", domain.Money(in.Financials.AnnualRevenue), domain.Money(company.RevenueLastYear))})
		res.Decision = domain.Decision{Type: domain.DecisionNeedInfo,
			Reasons: []string{fmt.Sprintf("Выручка за год — %s, а по данным налоговой за прошлый год — %s. Разница больше чем в полтора раза, поэтому мы не можем оценить заявку автоматически.",
				domain.Money(in.Financials.AnnualRevenue), domain.Money(company.RevenueLastYear))},
			Advice: []string{"Проверьте, не опечатались ли вы в выручке (лишний ноль — частая причина).", "Если выручка действительно выросла, укажите реальную цифру — пересчитаем."},
		}
		obs.Default.Inc("prescore_total", "Предварительные расчёты 1-го уровня", "decision", "need_info")
		return res, nil
	}
	res.Checks = append(res.Checks, domain.Check{Code: "revenue", Name: "Сверка выручки с ФНС", Result: "ok", Details: "Расхождений нет"})

	a := &domain.Application{ProductID: p.ID, Amount: in.Amount, TermMonths: in.TermMonths, INN: in.INN, Company: company, Financials: in.Financials}
	sc := scoring.Prescore(a, p, s.Now())
	res.Score, res.Factors, res.Decision = &sc.Score, sc.Factors, sc.Decision
	if d := &res.Decision; d.Type == domain.DecisionApproved || d.Type == domain.DecisionCounter {
		d.Reasons = append(d.Reasons, prescoreNote)
	}
	res.Decision.Final = false
	obs.Default.Inc("prescore_total", "Предварительные расчёты 1-го уровня", "decision", string(res.Decision.Type))
	return res, nil
}
