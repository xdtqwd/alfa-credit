// Package pipeline — кредитный конвейер (банковская сторона) для 2-го уровня анкеты.
//
//	Предварительный расчёт 1-го уровня считается синхронно без заявки (service.Prescore).
//	Здесь — полный скоринг: submitted → data_check (реестр, БКИ, Госуслуги, паспорт, протокол,
//	поручитель) → scoring (балл с кредитной историей и правила обеспечения) → окончательное решение.
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

var pipelineStatuses = []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring}

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

// dataCheck — проверки 2-го уровня. Окончательные отказы — только по реестру; сбой внешнего
// сервиса — ретрай на следующем тике, а не отказ.
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
	f := a.Full
	if f == nil {
		f = &domain.FullForm{}
	}
	var guarantor *domain.BureauReport
	if f.Guarantor != nil {
		r, err := w.Bureau.Report(ctx, f.Guarantor.INN)
		if err != nil {
			return err
		}
		guarantor = r
	}

	var checks []domain.Check
	next, comment := domain.StatusScoring, "Данные подтверждены, считаем полный скоринг"
	var decision *domain.Decision

	switch {
	case regErr != nil:
		checks = append(checks, chk("registry", "ЕГРЮЛ/ЕГРИП", "fail", "Компания не найдена"))
		next, comment = domain.StatusRejected, "Компания не найдена в реестре"
		decision = &domain.Decision{Type: domain.DecisionRejected, Final: true, Reasons: []string{"Мы не нашли компанию с ИНН " + a.INN + " в государственном реестре (ЕГРЮЛ/ЕГРИП)."}, Advice: []string{"Проверьте ИНН и оформите заявку снова."}}
	case company.Status != "active":
		checks = append(checks, chk("registry", "ЕГРЮЛ/ЕГРИП", "fail", "Компания ликвидирована"))
		next, comment = domain.StatusRejected, "Компания не действует по данным реестра"
		decision = &domain.Decision{Type: domain.DecisionRejected, Final: true, Reasons: []string{"По данным государственного реестра (ЕГРЮЛ) компания " + company.Name + " ликвидирована. Кредит можно выдать только действующей компании."}, Advice: []string{"Если компания действует, а в реестре ошибка — обратитесь в налоговую и подайте заявку после исправления."}}
	default:
		checks = append(checks, chk("registry", "ЕГРЮЛ/ЕГРИП", "ok", "Компания действует, регистрация с "+company.RegDate))
	}

	if a.Identity != nil {
		checks = append(checks, chk("esia", "Личность (Госуслуги)", "ok", a.Identity.FullName+", паспорт "+a.Identity.Passport.Masked()))
	}
	if report.HasOverdue {
		checks = append(checks, chk("bki", "Кредитная история (БКИ)", "warn", fmt.Sprintf("Есть просрочки до %d дней, кредитов: %d", report.MaxOverdueDays, report.ActiveLoans)))
	} else {
		checks = append(checks, chk("bki", "Кредитная история (БКИ)", "ok", fmt.Sprintf("Просрочек нет, действующих кредитов: %d", report.ActiveLoans)))
	}
	if company != nil && company.RevenueLastYear > 0 && float64(a.Financials.AnnualRevenue) > 1.5*float64(company.RevenueLastYear) {
		checks = append(checks, chk("revenue", "Сверка выручки с ФНС", "warn",
			fmt.Sprintf("Указано %s, по данным ФНС — %s", domain.Money(a.Financials.AnnualRevenue), domain.Money(company.RevenueLastYear))))
	} else {
		checks = append(checks, chk("revenue", "Сверка выручки с ФНС", "ok", "Расхождений нет"))
	}
	if a.IsLLC() && f.DirectorPassport != nil {
		// мок МВД: паспорт с номером 000000 — недействительный (для демонстрации уточнения)
		if f.DirectorPassport.Number == "000000" {
			checks = append(checks, chk("passport", "Паспорт руководителя (МВД)", "fail", "Паспорт "+f.DirectorPassport.Masked()+" недействителен"))
			if next == domain.StatusScoring {
				next, comment = domain.StatusNeedInfo, "Уточните паспорт руководителя"
				decision = &domain.Decision{Type: domain.DecisionNeedInfo, Final: true,
					Reasons: []string{"Паспорт руководителя " + f.DirectorPassport.Masked() + " по данным МВД недействителен."},
					Advice:  []string{"Проверьте серию и номер паспорта руководителя и отправьте снова."}}
			}
		} else {
			checks = append(checks, chk("passport", "Паспорт руководителя (МВД)", "ok", "Паспорт "+f.DirectorPassport.Masked()+" действителен"))
		}
	}
	if a.NeedsProtocol() && f.Protocol != nil {
		checks = append(checks, chk("protocol", "Решение об одобрении крупной сделки", "ok", f.Protocol.Name))
	}
	if g := f.Guarantor; g != nil {
		if guarantor.HasOverdue {
			checks = append(checks, chk("guarantor", "Поручитель (БКИ)", "fail", fmt.Sprintf("%s: просрочки до %d дней", g.FullName, guarantor.MaxOverdueDays)))
		} else {
			checks = append(checks, chk("guarantor", "Поручитель (БКИ)", "ok", g.FullName+": просрочек нет"))
		}
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

// score — полный скоринг: балл с кредитной историей и правила обеспечения.
//
//   - Сумма больше domain.SecurityRequiredFrom: обеспечение должно покрывать хотя бы половину кредита
//     залогом (в зачёт — оценка с дисконтом) или быть поручительство без просрочек в БКИ.
//   - Залог покрывает всю сумму — ставка на 1 п.п. ниже.
func (w *Worker) score(ctx context.Context, a *domain.Application) error {
	p, ok := domain.FindProduct(a.ProductID)
	if !ok {
		return fmt.Errorf("unknown product %q", a.ProductID)
	}
	res := scoring.Evaluate(a, p, a.Bureau, w.Now())
	d := res.Decision
	d.Final = true
	f := a.Full
	if f == nil {
		f = &domain.FullForm{Collateral: domain.Collateral{Type: domain.CollateralNone}}
	}
	var checks []domain.Check
	if d.Type == domain.DecisionApproved || d.Type == domain.DecisionCounter {
		amount := d.Amount
		need := amount > domain.SecurityRequiredFrom
		liquid := f.Collateral.LiquidValue()
		switch {
		case f.Collateral.Type == domain.CollateralNone && need:
			checks = append(checks, chk("collateral", "Залог", "warn", "Без залога"))
		case f.Collateral.Type == domain.CollateralNone:
			checks = append(checks, chk("collateral", "Залог", "ok", fmt.Sprintf("Не нужен для суммы до %s", domain.GroupDigits(domain.SecurityRequiredFrom))))
		case need && liquid*2 < amount:
			checks = append(checks, chk("collateral", "Залог", "warn", "Покрывает меньше половины кредита: "+f.Collateral.Describe()))
		default:
			checks = append(checks, chk("collateral", "Залог", "ok", capitalize(f.Collateral.Describe())))
		}
		guarantorOK, guarantorBad := false, false
		for _, c := range a.Checks {
			if c.Code == "guarantor" {
				guarantorOK, guarantorBad = c.Result == "ok", c.Result != "ok"
			}
		}
		if need && liquid*2 < amount && !guarantorOK {
			reasons := []string{fmt.Sprintf("Для кредита больше %s нужно обеспечение: залог, покрывающий хотя бы половину суммы (%s), или поручительство.",
				domain.Money(domain.SecurityRequiredFrom), domain.Money(amount/2))}
			if guarantorBad {
				reasons = append(reasons, fmt.Sprintf("У поручителя %s в кредитной истории есть просрочки — такое поручительство банк принять не может.", f.Guarantor.FullName))
			}
			if f.Collateral.Type != domain.CollateralNone {
				reasons = append(reasons, fmt.Sprintf("Залог — %s: этого не хватает.", f.Collateral.Describe()))
			}
			d = domain.Decision{Type: domain.DecisionNeedInfo, Final: true, Reasons: reasons,
				Advice: []string{"Укажите другого поручителя — например, второго участника компании.", "Или добавьте залог: недвижимость, транспорт, оборудование или депозит."}}
		} else {
			if liquid >= amount {
				old := d.Rate
				d.SetOffer(domain.NewQuote(amount, d.Rate-1.0, d.TermMonths))
				d.Reasons = append(d.Reasons, fmt.Sprintf("Залог полностью покрывает кредит, поэтому ставка ниже на 1 п.п.: %s вместо %s.", domain.Rate(d.Rate), domain.Rate(old)))
			} else if f.Collateral.Type != domain.CollateralNone {
				d.Reasons = append(d.Reasons, "Залог учтён: "+f.Collateral.Describe()+".")
			}
			if guarantorOK {
				d.Reasons = append(d.Reasons, "Поручительство "+f.Guarantor.FullName+" принято: кредитная история без просрочек.")
			} else if guarantorBad {
				d.Reasons = append(d.Reasons, "Поручительство не учитываем: у поручителя есть просрочки в БКИ. Залога для решения достаточно.")
			}
			// окончательные условия сравниваем с предварительными, если банк их подтвердил
			if pre := a.Preliminary; pre != nil && d.Type == domain.DecisionApproved {
				q := domain.NewQuote(a.Amount, pre.Rate, a.TermMonths)
				d.Requested = &q
			}
		}
	}
	next := map[domain.DecisionType]domain.Status{
		domain.DecisionApproved: domain.StatusApproved,
		domain.DecisionCounter:  domain.StatusCounterOffer,
		domain.DecisionRejected: domain.StatusRejected,
		domain.DecisionNeedInfo: domain.StatusNeedInfo,
	}[d.Type]
	comments := map[domain.Status]string{
		domain.StatusApproved:     "Кредит одобрен по полному скорингу",
		domain.StatusCounterOffer: "Одобрено на других условиях",
		domain.StatusRejected:     "Отказ по полному скорингу",
		domain.StatusNeedInfo:     "Уточните обеспечение",
	}
	obs.Default.Inc("credit_decisions_total", "Решения конвейера", "decision", string(d.Type), "product", a.ProductID)
	return w.update(ctx, a, func(x *domain.Application) error {
		s := res.Score
		x.Score = &s
		x.Factors = res.Factors
		x.Decision = &d
		x.Checks = append(append(x.Checks, chk("scoring", "Полный скоринг", resultFor(res.Score), fmt.Sprintf("Балл %d", res.Score))), checks...)
		return x.Transition(next, "system", comments[next], w.Now())
	})
}

func capitalize(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
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
