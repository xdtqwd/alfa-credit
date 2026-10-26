// Package service — прикладные сценарии клиента и банка поверх домена и хранилища.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type Service struct {
	Repo     storage.Repository
	Users    storage.UserRepository
	Registry integrations.Registry
	Now      func() time.Time
	// Tx — транзакции БД (pgstore). nil — файловый режим без транзакций (атомарность — блокировки + компенсации).
	Tx    storage.TxRunner
	Audit *obs.Auditor
}

func (s *Service) tx() storage.TxRunner {
	if s.Tx == nil {
		return storage.NoTx{}
	}
	return s.Tx
}

// transactional — настоящая ли транзакция (тогда откат делает БД и ручная компенсация не нужна).
func (s *Service) transactional() bool {
	_, none := s.tx().(storage.NoTx)
	return !none
}

func (s *Service) audit(ctx context.Context, action, id string, err error, details map[string]any) {
	outcome := "ok"
	if err != nil {
		outcome = "denied"
		if details == nil {
			details = map[string]any{}
		}
		details["error"] = err.Error()
	}
	s.Audit.Record(ctx, obs.AuditEntry{Action: action, Entity: "application", EntityID: id, Outcome: outcome, Details: details})
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Create — заявка создаётся после входа через Госуслуги (начало 2-го уровня). Данные 1-го уровня
// проверяются заново: предварительный расчёт считает сервер, а не присылает браузер.
func (s *Service) Create(ctx context.Context, clientID string, in domain.Level1) (*domain.Application, error) {
	u, err := s.Users.GetUser(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if u.Identity == nil {
		return nil, &domain.ValidationError{Field: "identity", Message: "войдите через Госуслуги — без подтверждённой личности заявку не подать"}
	}
	if u.Company.INN != in.INN {
		return nil, &domain.ValidationError{Field: "inn", Message: fmt.Sprintf("%s по данным ЕГРЮЛ не руководит компанией с ИНН %s — войдите через Госуслуги от имени её руководителя", u.FullName, in.INN)}
	}
	pre, err := s.Prescore(ctx, &in)
	if err != nil {
		return nil, err
	}
	if t := pre.Decision.Type; t != domain.DecisionApproved && t != domain.DecisionCounter {
		return nil, &domain.ValidationError{Field: "prescore", Message: "по предварительному расчёту кредит не одобрен — измените условия на 1-м уровне"}
	}

	now := s.Now()
	verdict := "одобрено"
	if pre.Decision.Type == domain.DecisionCounter {
		verdict = "одобрено на других условиях"
	}
	a := &domain.Application{
		ID: newID(), ClientID: clientID, ProductID: in.ProductID,
		Amount: in.Amount, TermMonths: in.TermMonths, Purpose: in.Purpose,
		INN: in.INN, Company: pre.Company, Financials: in.Financials,
		Contact: domain.Contact{FullName: u.FullName, Position: u.Position, Phone: "+" + u.Phone, Email: u.Email},
		Status:  domain.StatusFullForm, Checks: pre.Checks, Score: pre.Score, Factors: pre.Factors, Payments: []domain.Payment{},
		History: []domain.Event{{Status: domain.StatusFullForm, At: now, Actor: "client",
			Comment: "Предварительный расчёт: " + verdict + ". Личность подтверждена через Госуслуги — осталось приложить документы"}},
		CreatedAt: now, UpdatedAt: now, StatusChangedAt: now,
	}
	id := *u.Identity
	a.StartLevel2(pre.Decision, &id)
	out, err := s.Repo.Create(ctx, a)
	if err == nil {
		s.audit(ctx, "application.create", out.ID, nil, map[string]any{"number": out.Number, "product": out.ProductID, "amount": out.Amount, "term": out.TermMonths, "prescore": string(pre.Decision.Type)})
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, clientID, id string) (*domain.Application, error) {
	a, err := s.Repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.ClientID != clientID {
		return nil, domain.ErrNotFound // не раскрываем существование чужих заявок
	}
	return a, nil
}

func (s *Service) List(ctx context.Context, clientID string) ([]*domain.Application, error) {
	return s.Repo.List(ctx, storage.Filter{ClientID: clientID})
}

// Page — страница заявок клиента и общее количество (пагинация и фильтр по статусам).
func (s *Service) Page(ctx context.Context, clientID string, f storage.Filter) ([]*domain.Application, int, error) {
	f.ClientID = clientID
	return s.page(ctx, f)
}

func (s *Service) page(ctx context.Context, f storage.Filter) ([]*domain.Application, int, error) {
	items, err := s.Repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.Repo.Count(ctx, f)
	return items, total, err
}

func (s *Service) LookupCompany(ctx context.Context, inn string) (*domain.Company, error) {
	return s.Registry.Lookup(ctx, inn)
}

// Patch — автосохранение 2-го уровня анкеты.
func (s *Service) Patch(ctx context.Context, clientID, id string, p domain.DraftPatch) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	return s.Repo.Update(ctx, id, func(a *domain.Application) error { return a.ApplyPatch(p, s.Now()) })
}

// Submit — 2-й уровень уходит на полный скоринг: проверки с кредитной историей и окончательное решение.
func (s *Service) Submit(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	out, err := s.Repo.Update(ctx, id, func(a *domain.Application) error {
		if !a.EditableFull() {
			return domain.ErrInvalidTransition
		}
		if err := a.ValidateFull(s.Now()); err != nil {
			return err
		}
		comment := "Документы отправлены на полный скоринг"
		if a.Status == domain.StatusNeedInfo {
			comment = "Данные уточнены и отправлены повторно"
		}
		a.Decision, a.Checks = nil, []domain.Check{}
		return a.Transition(domain.StatusSubmitted, "client", comment, s.Now())
	})
	s.audit(ctx, "application.submit", id, err, nil)
	return out, err
}

// Accept — клиент принимает окончательное решение (одобрение или другие условия) и переходит к подписанию.
func (s *Service) Accept(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	out, err := s.Repo.Update(ctx, id, func(a *domain.Application) error {
		if a.Decision == nil || !a.Decision.Final {
			return domain.ErrInvalidTransition
		}
		// Договор заключается ровно на условиях решения банка (и при одобрении, и при контрпредложении) —
		// иначе график, выдача и платежи считались бы от параметров анкеты.
		if err := a.Transition(domain.StatusSigning, "client", "Условия приняты, документы готовы к подписанию", s.Now()); err != nil {
			return err
		}
		if a.Decision.Amount > 0 && a.Decision.TermMonths > 0 {
			a.Amount, a.TermMonths = a.Decision.Amount, a.Decision.TermMonths
		}
		return nil
	})
	s.audit(ctx, "application.accept", id, err, nil)
	return out, err
}

// Sign — подписание простой электронной подписью внутри продукта (симуляция) и выдача.
// Смена статуса на «выдан» и зачисление денег на счёт — одна транзакция БД: либо обе, либо ничего.
func (s *Service) Sign(ctx context.Context, clientID, id string, confirm bool) (*domain.Application, error) {
	if !confirm {
		return nil, &domain.ValidationError{Field: "confirm", Message: "подтвердите подписание"}
	}
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	// Зачисление идёт ВНУТРИ атомарного обновления заявки, как в Pay: порядок блокировок «заявка → счёт».
	// В PostgreSQL это одна транзакция; в файловом режиме — компенсация, если заявка не сохранилась.
	var a *domain.Application
	var credited int64
	err := s.tx().InTx(ctx, func(ctx context.Context) error {
		var err error
		a, err = s.Repo.Update(ctx, id, func(x *domain.Application) error {
			if err := x.Transition(domain.StatusIssued, "client", "Договор подписан ПЭП, средства зачислены на расчётный счёт (симуляция)", s.Now()); err != nil {
				return err
			}
			// Зачисление на расчётный счёт. В целевой архитектуре — проводка в АБС по событию loan.issued.
			if s.Users == nil {
				return nil
			}
			if _, err := s.Users.UpdateUser(ctx, clientID, func(u *domain.User) error { u.Credit(x.Amount); return nil }); err != nil {
				return err
			}
			credited = x.Amount
			return nil
		})
		return err
	})
	if err != nil && credited > 0 && !s.transactional() {
		// файловый режим: деньги зачислены, но заявка не сохранилась — списываем обратно
		if _, cerr := s.Users.UpdateUser(ctx, clientID, func(u *domain.User) error { return u.Debit(credited) }); cerr != nil {
			obs.L(ctx).Error("sign compensation failed: credited amount not reverted", "app", id, "amount", credited, "err", cerr)
		}
	}
	s.audit(ctx, "loan.sign_and_disburse", id, err, map[string]any{"amount": amountOf(a)})
	if err != nil {
		return nil, err
	}
	obs.Default.Inc("loans_issued_total", "Выданные кредиты")
	obs.Default.Add("loans_issued_amount_rub_total", "Сумма выданных кредитов, ₽", a.Amount)
	return a, nil
}

func amountOf(a *domain.Application) int64 {
	if a == nil {
		return 0
	}
	return a.Amount
}

// Pay — оплата очередного платежа по кредиту с расчётного счёта клиента.
func (s *Service) Pay(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	// Сумма платежа считается и списывается ВНУТРИ атомарного обновления заявки (строка заявки
	// заблокирована): параллельные запросы выстраиваются в очередь и каждый видит актуальное число
	// платежей. Порядок блокировок всегда «заявка → счёт клиента» — дедлока не будет.
	// В PostgreSQL списание и запись платежа — одна транзакция; в файловом режиме — компенсация.
	var debited int64
	var updated *domain.Application
	err := s.tx().InTx(ctx, func(ctx context.Context) error {
		var err error
		updated, err = s.Repo.Update(ctx, id, func(x *domain.Application) error {
			if x.Status != domain.StatusIssued {
				return domain.ErrInvalidTransition
			}
			amount := x.NextPaymentAmount()
			if amount <= 0 {
				return domain.ErrInvalidTransition
			}
			if _, err := s.Users.UpdateUser(ctx, clientID, func(u *domain.User) error { return u.Debit(amount) }); err != nil {
				return err
			}
			debited = amount
			now := s.Now()
			x.Payments = append(x.Payments, domain.Payment{N: len(x.Payments) + 1, At: now, Amount: amount})
			x.UpdatedAt = now
			if len(x.Payments) >= x.TermMonths {
				return x.Transition(domain.StatusClosed, "client", "Кредит полностью погашен", now)
			}
			return nil
		})
		return err
	})
	if err != nil && debited > 0 && !s.transactional() {
		// файловый режим: деньги списаны, но заявка не сохранилась — возвращаем на счёт
		if _, cerr := s.Users.UpdateUser(ctx, clientID, func(u *domain.User) error { u.Credit(debited); return nil }); cerr != nil {
			obs.L(ctx).Error("pay compensation failed: debited amount not returned", "app", id, "amount", debited, "err", cerr)
		}
	}
	s.audit(ctx, "loan.pay", id, err, map[string]any{"amount": debited})
	if err != nil {
		return nil, err
	}
	obs.Default.Inc("loan_payments_total", "Платежи по кредитам")
	return updated, nil
}

func (s *Service) Cancel(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	out, err := s.Repo.Update(ctx, id, func(a *domain.Application) error {
		return a.Transition(domain.StatusCancelled, "client", "Заявка отменена клиентом", s.Now())
	})
	s.audit(ctx, "application.cancel", id, err, nil)
	return out, err
}

// BankPage — заявки для кабинета банка с поиском, фильтром и пагинацией.
func (s *Service) BankPage(ctx context.Context, f storage.Filter) ([]*domain.Application, int, error) {
	f.ClientID = ""
	return s.page(ctx, f)
}
