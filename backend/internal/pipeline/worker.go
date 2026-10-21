// Package pipeline — фоновый обработчик кредитного конвейера (банковская сторона).
// Продвигает заявки submitted → data_check → scoring → решение. В целевой архитектуре
// это отдельный сервис, читающий события из брокера (Kafka) вместо опроса хранилища.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/scoring"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type Worker struct {
	Repo     storage.Repository
	Registry integrations.Registry
	Bureau   integrations.CreditBureau
	// StepDelay — имитация времени обработки на этапе, чтобы прогресс был виден в UI.
	StepDelay time.Duration
	Interval  time.Duration
	Log       *slog.Logger
	Now       func() time.Time
}

func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	apps, err := w.Repo.List(ctx, storage.Filter{Statuses: []domain.Status{
		domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring,
	}})
	if err != nil {
		w.Log.Error("pipeline list", "err", err)
		return
	}
	now := w.Now()
	for _, a := range apps {
		if now.Sub(a.StatusChangedAt) < w.StepDelay {
			continue
		}
		if err := w.process(ctx, a); err != nil {
			w.Log.Error("pipeline step failed", "app", a.Number, "status", a.Status, "err", err)
		}
	}
}

func (w *Worker) process(ctx context.Context, a *domain.Application) error {
	switch a.Status {
	case domain.StatusSubmitted:
		return w.update(ctx, a, func(x *domain.Application) error {
			return x.Transition(domain.StatusDataCheck, "system", "Заявка принята, проверяем данные компании", w.Now())
		})
	case domain.StatusDataCheck:
		return w.dataCheck(ctx, a)
	case domain.StatusScoring:
		return w.score(ctx, a)
	}
	return nil
}

// update применяет изменение, только если статус не поменялся конкурентно (оптимистичная проверка).
func (w *Worker) update(ctx context.Context, a *domain.Application, fn func(*domain.Application) error) error {
	expected := a.Status
	_, err := w.Repo.Update(ctx, a.ID, func(x *domain.Application) error {
		if x.Status != expected {
			return nil
		}
		return fn(x)
	})
	if err == nil {
		w.Log.Info("application step", "app", a.Number, "from", expected)
	}
	return err
}

func (w *Worker) dataCheck(ctx context.Context, a *domain.Application) error {
	company, regErr := w.Registry.Lookup(ctx, a.INN)
	report, bkiErr := w.Bureau.Report(ctx, a.INN)
	if bkiErr != nil {
		return bkiErr // ретрай на следующем тике
	}

	var checks []domain.Check
	next, comment := domain.StatusScoring, "Данные подтверждены, оцениваем риск"
	var decision *domain.Decision

	switch {
	case regErr != nil:
		checks = append(checks, chk("registry", "ЕГРЮЛ/ЕГРИП", "fail", "Компания не найдена"))
		next, comment = domain.StatusRejected, "Компания не найдена в реестре"
		decision = &domain.Decision{Type: domain.DecisionRejected, Reasons: []string{"Мы не нашли компанию с ИНН " + a.INN + " в государственном реестре (ЕГРЮЛ/ЕГРИП)."}, Advice: []string{"Проверьте ИНН в профиле компании."}}
	case company.Status != "active":
		checks = append(checks, chk("registry", "ЕГРЮЛ/ЕГРИП", "fail", "Компания ликвидирована"))
		next, comment = domain.StatusRejected, "Компания не действует по данным реестра"
		decision = &domain.Decision{Type: domain.DecisionRejected, Reasons: []string{"По данным государственного реестра (ЕГРЮЛ) компания " + company.Name + " ликвидирована. Кредит можно выдать только действующей компании."}, Advice: []string{"Если компания действует, а в реестре ошибка — обратитесь в налоговую и подайте заявку после исправления."}}
	default:
		checks = append(checks, chk("registry", "ЕГРЮЛ/ЕГРИП", "ok", "Компания действует, регистрация с "+company.RegDate))
	}

	if report.HasOverdue {
		checks = append(checks, chk("bki", "Кредитная история (БКИ)", "warn", fmt.Sprintf("Есть просрочки до %d дней, кредитов: %d", report.MaxOverdueDays, report.ActiveLoans)))
	} else {
		checks = append(checks, chk("bki", "Кредитная история (БКИ)", "ok", fmt.Sprintf("Просрочек нет, действующих кредитов: %d", report.ActiveLoans)))
	}

	if next == domain.StatusScoring && company.RevenueLastYear > 0 &&
		float64(a.Financials.AnnualRevenue) > 1.5*float64(company.RevenueLastYear) {
		checks = append(checks, chk("revenue", "Сверка выручки с ФНС", "warn",
			fmt.Sprintf("Указано %s ₽, по данным ФНС — %s ₽", money(a.Financials.AnnualRevenue), money(company.RevenueLastYear))))
		next = domain.StatusNeedInfo
		comment = "Уточните выручку: она заметно выше данных ФНС"
		decision = &domain.Decision{Type: domain.DecisionNeedInfo,
			Reasons: []string{fmt.Sprintf("В анкете выручка за год — %s, а по данным налоговой за прошлый год — %s. Разница больше чем в полтора раза, поэтому мы не можем оценить заявку автоматически.",
				domain.Money(a.Financials.AnnualRevenue), domain.Money(company.RevenueLastYear))},
			Advice: []string{"Проверьте, не опечатались ли вы в выручке (лишний ноль — частая причина).", "Если выручка действительно выросла, укажите реальную цифру и отправьте анкету снова — мы пересчитаем."},
		}
	} else if next == domain.StatusScoring {
		checks = append(checks, chk("revenue", "Сверка выручки с ФНС", "ok", "Расхождений нет"))
	}

	return w.update(ctx, a, func(x *domain.Application) error {
		if company != nil {
			x.Company = company
		}
		x.Bureau = report
		x.Checks = checks
		x.Decision = decision
		return x.Transition(next, "system", comment, w.Now())
	})
}

func (w *Worker) score(ctx context.Context, a *domain.Application) error {
	p, ok := domain.FindProduct(a.ProductID)
	if !ok {
		return fmt.Errorf("unknown product %q", a.ProductID)
	}
	res := scoring.Evaluate(a, p, a.Bureau, w.Now())
	next := map[domain.DecisionType]domain.Status{
		domain.DecisionApproved: domain.StatusApproved,
		domain.DecisionCounter:  domain.StatusCounterOffer,
		domain.DecisionRejected: domain.StatusRejected,
		domain.DecisionNeedInfo: domain.StatusNeedInfo,
	}[res.Decision.Type]
	comments := map[domain.Status]string{
		domain.StatusApproved:     "Кредит одобрен",
		domain.StatusCounterOffer: "Одобрено на других условиях",
		domain.StatusRejected:     "Отказ по заявке",
		domain.StatusNeedInfo:     "Нужно уточнить данные",
	}
	return w.update(ctx, a, func(x *domain.Application) error {
		s := res.Score
		x.Score = &s
		d := res.Decision
		x.Decision = &d
		x.Checks = append(x.Checks, chk("scoring", "Скоринг", resultFor(res.Score), fmt.Sprintf("Балл %d", res.Score)))
		return x.Transition(next, "system", comments[next], w.Now())
	})
}

func resultFor(score int) string {
	switch {
	case score >= 650:
		return "ok"
	case score >= 500:
		return "warn"
	}
	return "fail"
}

func money(v int64) string {
	s := fmt.Sprintf("%d", v)
	out := []byte{}
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
}

// Step выполняет один шаг конвейера для заявки (используется в тестах и ручном продвижении).
func (w *Worker) Step(ctx context.Context, a *domain.Application) error { return w.process(ctx, a) }

func chk(code, name, result, details string) domain.Check {
	return domain.Check{Code: code, Name: name, Result: result, Details: details}
}
