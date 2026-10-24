// Package pipeline — кредитный конвейер (банковская сторона).
//
//	Уровень 1: submitted → data_check → scoring → предварительное решение (по быстрой анкете).
//	Уровень 2: verification → окончательное решение (по полной анкете: обеспечение, поручитель).
//
// Основной путь — событийный: конвейер состоит в consumer group топика заявок (Kafka) и на каждое
// событие «статус изменился» делает следующий шаг. Шаг сам меняет статус → новое событие → следующий шаг.
// Идемпотентность: inbox (event_id уже обработан — пропускаем) + проверка ожидаемого статуса.
// Страховка: Run — «подметальщик», который доводит заявки, застрявшие дольше SweepAfter
// (например, событие ушло в DLQ или конвейер был выключен).
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/scoring"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

// Consumer — имя consumer group конвейера в Kafka.
const Consumer = "credit-decision-engine"

// Inbox — отметка «событие уже обработано» (pgstore.MarkProcessed).
type Inbox interface {
	MarkProcessed(ctx context.Context, consumer, eventID string) (bool, error)
}

type Worker struct {
	Repo     storage.Repository
	Registry integrations.Registry
	Bureau   integrations.CreditBureau
	Tx       storage.TxRunner // nil — без транзакций (файловый режим)
	Inbox    Inbox            // nil — дедупликация только по статусу
	// StepDelay — имитация времени обработки на этапе, чтобы прогресс был виден в UI.
	StepDelay time.Duration
	// Interval — период «подметальщика»; SweepAfter — сколько заявка может стоять на этапе,
	// прежде чем подметальщик продвинет её сам (0 — чистый опрос без событий, как в тестах).
	Interval   time.Duration
	SweepAfter time.Duration
	Log        *slog.Logger
	Now        func() time.Time
}

var pipelineStatuses = []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusVerification}

// Handle — обработчик события из шины (consumer group Consumer).
func (w *Worker) Handle(ctx context.Context, e events.Event) error {
	if e.Type != events.TypeStatusChanged {
		return nil
	}
	p, err := events.DecodeApplication(e)
	if err != nil {
		return nil // нечитаемое событие не повторяем
	}
	if !domain.Status(p.To).InPipeline() {
		return nil
	}
	ctx = obs.WithRequestID(ctx, e.Headers["request_id"])
	ctx = obs.WithActor(ctx, "system")
	// этап «длится» StepDelay с момента смены статуса — прогресс виден клиенту
	if wait := p.StatusAt.Add(w.StepDelay).Sub(w.Now()); wait > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	a, err := w.Repo.Get(ctx, p.ApplicationID)
	if err != nil {
		return err
	}
	if string(a.Status) != p.To {
		return nil // устаревшее событие: заявка уже ушла дальше
	}
	run := func(ctx context.Context) error {
		if w.Inbox != nil {
			fresh, err := w.Inbox.MarkProcessed(ctx, Consumer, e.ID)
			if err != nil {
				return err
			}
			if !fresh {
				obs.Default.Inc("pipeline_duplicates_total", "Повторно доставленные события, пропущенные конвейером")
				return nil
			}
		}
		return w.process(ctx, a)
	}
	if w.Tx != nil {
		return w.Tx.InTx(ctx, run)
	}
	return run(ctx)
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
	apps, err := w.Repo.List(ctx, storage.Filter{Statuses: pipelineStatuses})
	if err != nil {
		w.Log.Error("pipeline list", "err", err)
		return
	}
	now := w.Now()
	after := max(w.StepDelay, w.SweepAfter)
	for _, a := range apps {
		if now.Sub(a.StatusChangedAt) < after {
			continue
		}
		if w.SweepAfter > 0 {
			obs.Default.Inc("pipeline_swept_total", "Заявки, продвинутые подметальщиком (событие не дошло вовремя)")
			w.Log.Warn("pipeline sweep: application stuck, advancing", "app", a.Number, "status", a.Status, "since", a.StatusChangedAt)
		}
		if err := w.process(obs.WithActor(ctx, "system"), a); err != nil {
			w.Log.Error("pipeline step failed", "app", a.Number, "status", a.Status, "err", err)
		}
	}
}

func (w *Worker) process(ctx context.Context, a *domain.Application) error {
	start := time.Now()
	defer func() {
		obs.Default.Observe("pipeline_step_seconds", "Время шага конвейера (без паузы этапа)", time.Since(start).Seconds(), "step", string(a.Status))
	}()
	switch a.Status {
	case domain.StatusSubmitted:
		return w.update(ctx, a, func(x *domain.Application) error {
			return x.Transition(domain.StatusDataCheck, "system", "Заявка принята, проверяем данные компании", w.Now())
		})
	case domain.StatusDataCheck:
		return w.dataCheck(ctx, a)
	case domain.StatusScoring:
		return w.score(ctx, a)
	case domain.StatusVerification:
		return w.verify(ctx, a)
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
		w.Log.Info("application step", "app", a.Number, "from", expected, "rid", obs.RequestID(ctx))
	}
	return err
}

func (w *Worker) dataCheck(ctx context.Context, a *domain.Application) error {
	company, regErr := w.Registry.Lookup(ctx, a.INN)
	var invalidINN *domain.ValidationError
	if regErr != nil && !errors.Is(regErr, domain.ErrCompanyNotFound) && !errors.As(regErr, &invalidINN) {
		return regErr // реестр недоступен — ретрай на следующем тике, а не отказ «компания не найдена»
	}
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
			fmt.Sprintf("Указано %s, по данным ФНС — %s", domain.Money(a.Financials.AnnualRevenue), domain.Money(company.RevenueLastYear))))
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
	// по быстрой анкете решение предварительное; у одноуровневого продукта (овердрафт) — сразу окончательное
	res.Decision.Final = !a.TwoLevel()
	if !res.Decision.Final {
		comments[domain.StatusApproved] = "Предварительно одобрено — осталось заполнить полную анкету"
		comments[domain.StatusCounterOffer] = "Предварительно одобрено на других условиях"
	}
	obs.Default.Inc("credit_decisions_total", "Решения конвейера", "decision", string(res.Decision.Type), "product", a.ProductID)
	return w.update(ctx, a, func(x *domain.Application) error {
		s := res.Score
		x.Score = &s
		x.Factors = res.Factors
		d := res.Decision
		x.Decision = &d
		x.Checks = append(x.Checks, chk("scoring", "Скоринг", resultFor(res.Score), fmt.Sprintf("Балл %d", res.Score)))
		return x.Transition(next, "system", comments[next], w.Now())
	})
}

// verify — окончательное решение по полной анкете (уровень 2).
//
//   - Сумма больше domain.SecurityRequiredFrom: обеспечение должно покрывать хотя бы половину кредита
//     залогом (в зачёт — оценка с дисконтом) или быть поручительство без просрочек в БКИ.
//   - Залог покрывает всю сумму — ставка на 1 п.п. ниже предварительной.
//   - Иначе — окончательно одобряем на предварительных условиях.
func (w *Worker) verify(ctx context.Context, a *domain.Application) error {
	f, pre := a.Full, a.Preliminary
	if f == nil || pre == nil {
		return fmt.Errorf("application %s: verification without full form", a.Number)
	}
	checks := []domain.Check{chk("full_form", "Полная анкета", "ok",
		fmt.Sprintf("Сотрудников: %d, цель описана, согласие на выписки получено", f.Employees))}

	need := a.Amount > domain.SecurityRequiredFrom
	liquid := f.Collateral.LiquidValue()
	switch {
	case f.Collateral.Type == domain.CollateralNone && need:
		checks = append(checks, chk("collateral", "Залог", "warn", "Без залога"))
	case f.Collateral.Type == domain.CollateralNone:
		checks = append(checks, chk("collateral", "Залог", "ok", fmt.Sprintf("Не нужен для суммы до %s", domain.GroupDigits(domain.SecurityRequiredFrom))))
	case need && liquid*2 < a.Amount:
		checks = append(checks, chk("collateral", "Залог", "warn", "Покрывает меньше половины кредита: "+f.Collateral.Describe()))
	default:
		checks = append(checks, chk("collateral", "Залог", "ok", capitalize(f.Collateral.Describe())))
	}

	guarantorOK, guarantorBad := false, ""
	if g := f.Guarantor; g != nil {
		r, err := w.Bureau.Report(ctx, g.INN)
		if err != nil {
			return err // ретрай на следующем тике
		}
		if r.HasOverdue {
			guarantorBad = fmt.Sprintf("У поручителя %s в кредитной истории есть просрочки до %d дней — такое поручительство банк принять не может.", g.FullName, r.MaxOverdueDays)
			checks = append(checks, chk("guarantor", "Поручитель (БКИ)", "fail", fmt.Sprintf("%s: просрочки до %d дней", g.FullName, r.MaxOverdueDays)))
		} else {
			guarantorOK = true
			checks = append(checks, chk("guarantor", "Поручитель (БКИ)", "ok", g.FullName+": просрочек нет"))
		}
	}

	if need && liquid*2 < a.Amount && !guarantorOK {
		reasons := []string{fmt.Sprintf("Для кредита больше %s нужно обеспечение: залог, покрывающий хотя бы половину суммы (%s), или поручительство.",
			domain.Money(domain.SecurityRequiredFrom), domain.Money(a.Amount/2))}
		if guarantorBad != "" {
			reasons = append(reasons, guarantorBad)
		}
		if f.Collateral.Type != domain.CollateralNone {
			reasons = append(reasons, fmt.Sprintf("Залог — %s: этого не хватает.", f.Collateral.Describe()))
		}
		d := &domain.Decision{Type: domain.DecisionNeedInfo, Reasons: reasons,
			Advice: []string{"Укажите другого поручителя — например, второго участника компании.", "Или добавьте залог: недвижимость, транспорт, оборудование или депозит."}}
		return w.update(ctx, a, func(x *domain.Application) error {
			x.Checks = append(baseChecks(x.Checks), checks...)
			x.Decision = d
			return x.Transition(domain.StatusNeedInfo, "system", "Уточните обеспечение в полной анкете", w.Now())
		})
	}

	rate := pre.Rate
	reasons := []string{"Полная анкета проверена — подтверждаем предварительные условия."}
	if liquid >= a.Amount {
		rate -= 1.0
		reasons = append(reasons, fmt.Sprintf("Залог полностью покрывает кредит, поэтому ставка ниже на 1 п.п.: %s вместо %s.", domain.Rate(rate), domain.Rate(pre.Rate)))
	} else if f.Collateral.Type != domain.CollateralNone {
		reasons = append(reasons, "Залог учтён: "+f.Collateral.Describe()+".")
	}
	if guarantorOK {
		reasons = append(reasons, "Поручительство "+f.Guarantor.FullName+" принято: кредитная история без просрочек.")
	} else if guarantorBad != "" {
		reasons = append(reasons, "Поручительство не учитываем: у поручителя есть просрочки в БКИ. Залога для решения достаточно.")
	}
	preQuote := domain.NewQuote(a.Amount, pre.Rate, a.TermMonths)
	d := domain.Decision{Type: domain.DecisionApproved, Reasons: reasons, Requested: &preQuote, Final: true}
	d.SetOffer(domain.NewQuote(a.Amount, rate, a.TermMonths))
	obs.Default.Inc("credit_decisions_total", "Решения конвейера", "decision", "final_approved", "product", a.ProductID)
	return w.update(ctx, a, func(x *domain.Application) error {
		x.Checks = append(baseChecks(x.Checks), checks...)
		x.Decision = &d
		return x.Transition(domain.StatusApproved, "system", "Кредит одобрен окончательно", w.Now())
	})
}

func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// baseChecks — проверки уровня 1 без результатов прошлой проверки полной анкеты (при повторной отправке).
func baseChecks(cs []domain.Check) []domain.Check {
	out := make([]domain.Check, 0, len(cs))
	for _, c := range cs {
		if c.Code != "full_form" && c.Code != "collateral" && c.Code != "guarantor" {
			out = append(out, c)
		}
	}
	return out
}

func resultFor(score int) string {
	switch {
	case score >= scoring.ApproveThreshold:
		return "ok"
	case score >= scoring.RejectThreshold:
		return "warn"
	}
	return "fail"
}

// Step выполняет один шаг конвейера для заявки (используется в тестах и ручном продвижении).
func (w *Worker) Step(ctx context.Context, a *domain.Application) error { return w.process(ctx, a) }

func chk(code, name, result, details string) domain.Check {
	return domain.Check{Code: code, Name: name, Result: result, Details: details}
}
