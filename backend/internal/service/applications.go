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
		Status: domain.StatusDraft, Checks: []domain.Check{},
		History:   []domain.Event{{Status: domain.StatusDraft, At: now, Actor: "client", Comment: "Анкета создана из калькулятора"}},
		CreatedAt: now, UpdatedAt: now, StatusChangedAt: now,
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
		if a.Decision != nil && a.Status == domain.StatusCounterOffer {
			a.Amount, a.TermMonths = a.Decision.Amount, a.Decision.TermMonths
		}
		return a.Transition(domain.StatusSigning, "client", "Условия приняты, документы готовы к подписанию", s.Now())
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
	return s.Repo.Update(ctx, id, func(a *domain.Application) error {
		return a.Transition(domain.StatusIssued, "client", "Договор подписан ПЭП, средства зачислены на расчётный счёт (симуляция)", s.Now())
	})
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
