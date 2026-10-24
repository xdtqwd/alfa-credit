// Package service — прикладные сценарии клиента и банка поверх домена и хранилища.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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

type CreateInput struct {
	ProductID  string `json:"product_id"`
	Amount     int64  `json:"amount"`
	TermMonths int    `json:"term_months"`
}

// Create — сразу создаёт черновик анкеты с параметрами из калькулятора
// (без промежуточного шага «оставить заявку»).
func (s *Service) Create(ctx context.Context, clientID string, in CreateInput) (*domain.Application, error) {
	p, ok := domain.FindProduct(in.ProductID)
	if !ok {
		return nil, &domain.ValidationError{Field: "product_id", Message: "неизвестный продукт"}
	}
	if in.Amount == 0 {
		in.Amount = p.DefaultAmt
	}
	if in.TermMonths == 0 {
		in.TermMonths = p.DefaultTerm
	}
	in.Amount = max(p.MinAmount, min(p.MaxAmount, in.Amount))
	in.TermMonths = max(p.MinTerm, min(p.MaxTerm, in.TermMonths))

	now := s.Now()
	a := &domain.Application{
		ID: newID(), ClientID: clientID, ProductID: p.ID,
		Amount: in.Amount, TermMonths: in.TermMonths, Purpose: p.Purposes[0],
		Status: domain.StatusDraft, Checks: []domain.Check{}, Payments: []domain.Payment{},
		History:   []domain.Event{{Status: domain.StatusDraft, At: now, Actor: "client", Comment: "Анкета создана из калькулятора"}},
		CreatedAt: now, UpdatedAt: now, StatusChangedAt: now,
	}
	// Предзаполнение из профиля: компания, реквизиты и контакт уже известны банку после входа.
	if s.Users != nil {
		if u, err := s.Users.GetUser(ctx, clientID); err == nil {
			c := u.Company
			a.INN, a.Company = c.INN, &c
			a.Contact = domain.Contact{FullName: u.FullName, Position: u.Position, Phone: "+" + u.Phone, Email: u.Email}
			a.History[0].Comment = "Анкета создана из калькулятора, данные компании взяты из профиля"
		}
	}
	out, err := s.Repo.Create(ctx, a)
	if err == nil {
		s.audit(ctx, "application.create", out.ID, nil, map[string]any{"number": out.Number, "product": out.ProductID, "amount": out.Amount, "term": out.TermMonths})
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

// Patch — автосохранение анкеты. При вводе ИНН подтягивает данные компании из реестра.
func (s *Service) Patch(ctx context.Context, clientID, id string, p domain.DraftPatch) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	if s.Users != nil {
		p.INN = nil // ИНН фиксирован профилем: заявка подаётся только от своей компании
	}
	var company *domain.Company
	innChanged := p.INN != nil
	if innChanged && domain.ValidINN(*p.INN) {
		c, err := s.Registry.Lookup(ctx, *p.INN)
		if err != nil && !errors.Is(err, domain.ErrCompanyNotFound) {
			return nil, err
		}
		company = c
	}
	return s.Repo.Update(ctx, id, func(a *domain.Application) error {
		if err := a.ApplyPatch(p, s.Now()); err != nil {
			return err
		}
		if innChanged {
			a.Company = company
		}
		return nil
	})
}

func (s *Service) Submit(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	level := 1
	out, err := s.Repo.Update(ctx, id, func(a *domain.Application) error {
		if a.EditableFull() {
			// уровень 2: полная анкета уходит на окончательное решение; предварительное решение и проверки остаются
			level = 2
			if err := a.ValidateFull(); err != nil {
				return err
			}
			comment := "Полная анкета отправлена на окончательное решение"
			if a.Status == domain.StatusNeedInfo {
				comment = "Полная анкета уточнена и отправлена повторно"
			}
			return a.Transition(domain.StatusVerification, "client", comment, s.Now())
		}
		if !a.EditableQuick() {
			return domain.ErrInvalidTransition
		}
		if err := a.ValidateForSubmit(); err != nil {
			return err
		}
		comment := "Быстрая анкета отправлена на предварительное решение"
		if !a.TwoLevel() {
			comment = "Анкета отправлена"
		}
		if a.Status == domain.StatusNeedInfo {
			comment = "Данные уточнены, анкета отправлена повторно"
		}
		a.Level = 1
		a.Decision, a.Score, a.Factors, a.Checks = nil, nil, nil, []domain.Check{}
		return a.Transition(domain.StatusSubmitted, "client", comment, s.Now())
	})
	s.audit(ctx, "application.submit", id, err, map[string]any{"level": level})
	return out, err
}

// Accept — клиент принимает предложение (одобрение или контрпредложение).
// Предварительное решение открывает полную анкету (уровень 2), окончательное — подписание.
func (s *Service) Accept(ctx context.Context, clientID, id string) (*domain.Application, error) {
	cur, err := s.Get(ctx, clientID, id)
	if err != nil {
		return nil, err
	}
	if cur.Decision != nil && !cur.Decision.Final && (cur.Status == domain.StatusApproved || cur.Status == domain.StatusCounterOffer) {
		director := ""
		if cur.Company != nil {
			director = cur.Company.Director
		}
		out, err := s.Repo.Update(ctx, id, func(a *domain.Application) error {
			if a.Decision == nil || a.Decision.Final {
				return domain.ErrInvalidTransition // решение успели поменять конкурентно
			}
			if err := a.Transition(domain.StatusFullForm, "client", "Предварительные условия приняты — осталось заполнить полную анкету", s.Now()); err != nil {
				return err
			}
			a.StartFullForm(director)
			return nil
		})
		s.audit(ctx, "application.accept_preliminary", id, err, nil)
		return out, err
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
		if a.Decision != nil && a.Decision.Amount > 0 && a.Decision.TermMonths > 0 {
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
	var a *domain.Application
	err := s.tx().InTx(ctx, func(ctx context.Context) error {
		var err error
		a, err = s.Repo.Update(ctx, id, func(a *domain.Application) error {
			return a.Transition(domain.StatusIssued, "client", "Договор подписан ПЭП, средства зачислены на расчётный счёт (симуляция)", s.Now())
		})
		if err != nil {
			return err
		}
		// Зачисление на расчётный счёт. В целевой архитектуре — проводка в АБС по событию loan.issued.
		if s.Users != nil {
			_, err = s.Users.UpdateUser(ctx, clientID, func(u *domain.User) error { u.Credit(a.Amount); return nil })
		}
		return err
	})
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
