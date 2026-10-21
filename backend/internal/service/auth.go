package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/auth"
	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type AuthService struct {
	Users    storage.UserRepository
	Registry integrations.Registry
	Sessions *auth.Sessions
	Limiter  *auth.Limiter
	Now      func() time.Time
}

type RegisterInput struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	INN      string `json:"inn"`
	Email    string `json:"email"`
}

// Register — регистрация представителя бизнеса: компания и реквизиты подтягиваются по ИНН.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*domain.User, string, error) {
	phone := domain.NormalizePhone(in.Phone)
	switch {
	case len(phone) != 11 || phone[0] != '7':
		return nil, "", &domain.ValidationError{Field: "phone", Message: "укажите российский номер телефона"}
	case len([]rune(in.Password)) < 8:
		return nil, "", &domain.ValidationError{Field: "password", Message: "пароль — минимум 8 символов"}
	case len([]rune(strings.TrimSpace(in.FullName))) < 5:
		return nil, "", &domain.ValidationError{Field: "full_name", Message: "укажите ФИО"}
	}
	company, err := s.Registry.Lookup(ctx, in.INN)
	if errors.Is(err, domain.ErrCompanyNotFound) {
		return nil, "", &domain.ValidationError{Field: "inn", Message: "компания с таким ИНН не найдена"}
	}
	if err != nil {
		return nil, "", err
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, "", err
	}
	now := s.Now()
	pos := "Генеральный директор"
	if company.Type == "ИП" {
		pos = "Индивидуальный предприниматель"
	}
	u := &domain.User{
		ID: newID(), Phone: phone, FullName: strings.TrimSpace(in.FullName), Position: pos, Email: in.Email,
		PasswordHash: hash, Company: *company, Requisites: RequisitesFor(company),
		Balance: 250_000, CreatedAt: now, LastLoginAt: now,
	}
	if err := s.Users.CreateUser(ctx, u); err != nil {
		if errors.Is(err, domain.ErrUserExists) {
			return nil, "", &domain.ValidationError{Field: "phone", Message: "пользователь с таким телефоном уже есть — войдите"}
		}
		return nil, "", err
	}
	return u, s.Sessions.Create(u.ID), nil
}

// Login — вход по телефону и паролю с защитой от перебора.
func (s *AuthService) Login(ctx context.Context, phoneRaw, password, ip string) (*domain.User, string, error) {
	phone := domain.NormalizePhone(phoneRaw)
	if s.Limiter.Blocked("p:"+phone) || s.Limiter.Blocked("ip:"+ip) {
		return nil, "", domain.ErrTooManyAttempts
	}
	u, err := s.Users.FindByPhone(ctx, phone)
	if err != nil || !auth.VerifyPassword(u.PasswordHash, password) {
		s.Limiter.Fail("p:" + phone)
		s.Limiter.Fail("ip:" + ip)
		return nil, "", domain.ErrInvalidCredentials
	}
	s.Limiter.Reset("p:" + phone)
	u, _ = s.Users.UpdateUser(ctx, u.ID, func(x *domain.User) error { x.LastLoginAt = s.Now(); return nil })
	return u, s.Sessions.Create(u.ID), nil
}

func (s *AuthService) Logout(token string) { s.Sessions.Delete(token) }

func (s *AuthService) Me(ctx context.Context, userID string) (*domain.User, error) {
	return s.Users.GetUser(ctx, userID)
}

type ProfilePatch struct {
	Email    *string `json:"email"`
	Position *string `json:"position"`
}

func (s *AuthService) UpdateProfile(ctx context.Context, userID string, p ProfilePatch) (*domain.User, error) {
	return s.Users.UpdateUser(ctx, userID, func(u *domain.User) error {
		if p.Email != nil {
			u.Email = strings.TrimSpace(*p.Email)
		}
		if p.Position != nil {
			u.Position = strings.TrimSpace(*p.Position)
		}
		return nil
	})
}

// RequisitesFor генерирует детерминированные синтетические реквизиты по ИНН.
func RequisitesFor(c *domain.Company) domain.Requisites {
	h := fnv.New64a()
	_, _ = h.Write([]byte(c.INN))
	n := h.Sum64()
	r := domain.Requisites{
		Account:     fmt.Sprintf("40702810%012d", n%1_000_000_000_000),
		BankName:    "АО «Альфа-Банк» (демо)",
		BIK:         "044525593",
		CorrAccount: "30101810200000000593",
	}
	if c.Type == "ИП" {
		r.Account = "40802810" + r.Account[8:]
	} else if len(c.INN) >= 4 {
		r.KPP = c.INN[:4] + "01001"
	}
	return r
}

// DemoAccount — тестовые учётные записи для жюри (README → «Тестовые аккаунты»).
type DemoAccount struct {
	Phone, FullName, INN string
	Balance              int64
}

var DemoAccounts = []DemoAccount{
	{"79000000001", "Смирнов Алексей Викторович", "7701234567", 1_200_000},
	{"79000000002", "Кузнецова Мария Олеговна", "2224000012", 380_000},
	{"79000000003", "Петров Игорь Сергеевич", "5401000009", 90_000},
	{"79000000004", "Орлов Олег Петрович", "6600000000", 15_000},
}

const DemoPassword = "demo1234"

// SeedDemo создаёт демо-аккаунты, если хранилище пустое.
func (s *AuthService) SeedDemo(ctx context.Context) error {
	if s.Users.CountUsers(ctx) > 0 {
		return nil
	}
	for _, d := range DemoAccounts {
		c, err := s.Registry.Lookup(ctx, d.INN)
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(DemoPassword)
		if err != nil {
			return err
		}
		now := s.Now()
		pos := "Генеральный директор"
		if c.Type == "ИП" {
			pos = "Индивидуальный предприниматель"
		}
		u := &domain.User{
			ID: newID(), Phone: d.Phone, FullName: d.FullName, Position: pos,
			Email: "demo" + d.Phone[len(d.Phone)-1:] + "@example.com", PasswordHash: hash,
			Company: *c, Requisites: RequisitesFor(c), Balance: d.Balance, CreatedAt: now,
		}
		if err := s.Users.CreateUser(ctx, u); err != nil {
			return err
		}
	}
	return nil
}
