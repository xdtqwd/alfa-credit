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
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type Service struct {
	Repo     storage.Repository
	Users    storage.UserRepository
	Registry integrations.Registry
	Now      func() time.Time
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
	return s.Repo.Create(ctx, a)
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
	return s.Repo.Update(ctx, id, func(a *domain.Application) error {
		if err := a.ValidateForSubmit(); err != nil {
			return err
		}
		comment := "Анкета отправлена"
		if a.Status == domain.StatusNeedInfo {
			comment = "Данные уточнены, анкета отправлена повторно"
		}
		a.Decision, a.Score, a.Checks = nil, nil, []domain.Check{}
		return a.Transition(domain.StatusSubmitted, "client", comment, s.Now())
	})
}

// Accept — клиент принимает предложение (одобрение или контрпредложение).
func (s *Service) Accept(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	return s.Repo.Update(ctx, id, func(a *domain.Application) error {
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
}

// Sign — подписание простой электронной подписью внутри продукта (симуляция) и выдача.
func (s *Service) Sign(ctx context.Context, clientID, id string, confirm bool) (*domain.Application, error) {
	if !confirm {
		return nil, &domain.ValidationError{Field: "confirm", Message: "подтвердите подписание"}
	}
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	a, err := s.Repo.Update(ctx, id, func(a *domain.Application) error {
		return a.Transition(domain.StatusIssued, "client", "Договор подписан ПЭП, средства зачислены на расчётный счёт (симуляция)", s.Now())
	})
	if err != nil {
		return nil, err
	}
	// Зачисление на расчётный счёт. В целевой архитектуре — сага с компенсацией через АБС.
	if s.Users != nil {
		if _, err := s.Users.UpdateUser(ctx, clientID, func(u *domain.User) error { u.Credit(a.Amount); return nil }); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// Pay — оплата очередного платежа по кредиту с расчётного счёта клиента.
func (s *Service) Pay(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	// Сумма платежа считается и списывается ВНУТРИ атомарного обновления заявки:
	// параллельные запросы выстраиваются в очередь и каждый видит актуальное число платежей
	// (иначе два запроса оплатили бы один и тот же «очередной» платёж по устаревшему снимку).
	// Порядок блокировок всегда «заявка → пользователь», обратного пути нет — дедлока не будет.
	var debited int64
	updated, err := s.Repo.Update(ctx, id, func(x *domain.Application) error {
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
	if err != nil {
		if debited > 0 {
			// компенсация: деньги списаны, но заявка не сохранилась — возвращаем на счёт
			_, _ = s.Users.UpdateUser(ctx, clientID, func(u *domain.User) error { u.Credit(debited); return nil })
		}
		return nil, err
	}
	return updated, nil
}

func (s *Service) Cancel(ctx context.Context, clientID, id string) (*domain.Application, error) {
	if _, err := s.Get(ctx, clientID, id); err != nil {
		return nil, err
	}
	return s.Repo.Update(ctx, id, func(a *domain.Application) error {
		return a.Transition(domain.StatusCancelled, "client", "Заявка отменена клиентом", s.Now())
	})
}

// BankList — все заявки для сотрудника банка.
func (s *Service) BankList(ctx context.Context) ([]*domain.Application, error) {
	return s.Repo.List(ctx, storage.Filter{})
}
