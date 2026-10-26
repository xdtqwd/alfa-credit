package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/auth"
	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type AuthService struct {
	Users    storage.UserRepository
	Registry integrations.Registry
	ESIA     integrations.ESIA // nil — вход через Госуслуги выключен
	Sessions auth.SessionStore
	Limiter  *auth.Limiter
	Now      func() time.Time
	Audit    *obs.Auditor
}

func (s *AuthService) audit(ctx context.Context, action, userID, outcome string, details map[string]any) {
	s.Audit.Record(ctx, obs.AuditEntry{Action: action, Entity: "user", EntityID: userID, Outcome: outcome, Actor: userID, Details: details})
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
	u := &domain.User{
		ID: newID(), Role: domain.RoleClient, Phone: phone, FullName: strings.TrimSpace(in.FullName), Position: positionFor(company), Email: in.Email,
		PasswordHash: hash, Company: *company, Requisites: RequisitesFor(company),
		Balance: 250_000, CreatedAt: now, LastLoginAt: now,
	}
	if err := s.Users.CreateUser(ctx, u); err != nil {
		if errors.Is(err, domain.ErrUserExists) {
			return nil, "", &domain.ValidationError{Field: "phone", Message: "пользователь с таким телефоном уже есть — войдите"}
		}
		return nil, "", err
	}
	s.audit(ctx, "auth.register", u.ID, "ok", map[string]any{"inn": u.Company.INN})
	tok, err := s.Sessions.Create(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	return u, tok, nil
}

// dummyPasswordHash — с ним сверяется пароль, когда телефона нет среди пользователей: проверка идёт
// так же долго, как для настоящего аккаунта, и по времени ответа не узнать, зарегистрирован ли номер.
var dummyPasswordHash = sync.OnceValue(func() string {
	h, _ := auth.HashPassword("no-such-user")
	return h
})

// Login — вход по телефону и паролю с защитой от перебора.
func (s *AuthService) Login(ctx context.Context, phoneRaw, password, ip string) (*domain.User, string, error) {
	phone := domain.NormalizePhone(phoneRaw)
	if s.Limiter.Blocked("p:"+phone) || s.Limiter.Blocked("ip:"+ip) {
		s.audit(ctx, "auth.login", "", "denied", map[string]any{"reason": "too_many_attempts", "phone": obs.MaskPhone(phone), "ip": ip})
		return nil, "", domain.ErrTooManyAttempts
	}
	u, err := s.Users.FindByPhone(ctx, phone)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, "", err // сбой хранилища — не «неверный пароль» и не попытка подбора
	}
	hash := dummyPasswordHash()
	if u != nil {
		hash = u.PasswordHash
	}
	if !auth.VerifyPassword(hash, password) || u == nil {
		s.Limiter.Fail("p:" + phone)
		s.Limiter.Fail("ip:" + ip)
		s.audit(ctx, "auth.login", "", "denied", map[string]any{"reason": "invalid_credentials", "phone": obs.MaskPhone(phone), "ip": ip})
		return nil, "", domain.ErrInvalidCredentials
	}
	s.Limiter.Reset("p:" + phone)
	u, err = s.Users.UpdateUser(ctx, u.ID, func(x *domain.User) error { x.LastLoginAt = s.Now(); return nil })
	if err != nil {
		return nil, "", err
	}
	s.audit(ctx, "auth.login", u.ID, "ok", map[string]any{"ip": ip})
	tok, err := s.Sessions.Create(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	return u, tok, nil
}

// ESIAPersons — учётные записи для демо-окна Госуслуг: кто это и какой компанией руководит.
func (s *AuthService) ESIAPersons(ctx context.Context) []integrations.ESIAPersonView {
	var out []integrations.ESIAPersonView
	for _, p := range s.ESIA.Persons(ctx) {
		v := integrations.ESIAPersonView{ID: p.ID, FullName: p.Identity.FullName, SNILS: integrations.MaskSNILS(p.Identity.SNILS), CompanyINN: p.CompanyINN}
		if c, err := s.Registry.Lookup(ctx, p.CompanyINN); err == nil {
			v.CompanyName, v.Role = c.Name, positionFor(c)
		}
		out = append(out, v)
	}
	return out
}

// ESIALogin — вход через Госуслуги (ЕСИА). Личность подтверждена Госуслугами, поэтому пароль не нужен:
// аккаунт находим по подтверждённому телефону, а если его нет — создаём (компания — из реестра по ИНН,
// которым человек руководит). Личность сохраняется в профиле: без неё заявку не подать.
func (s *AuthService) ESIALogin(ctx context.Context, personID, ip string) (*domain.User, string, error) {
	if s.Limiter.Blocked("ip:" + ip) {
		return nil, "", domain.ErrTooManyAttempts
	}
	p, ok := s.ESIA.Person(ctx, personID)
	if !ok {
		s.Limiter.Fail("ip:" + ip)
		return nil, "", &domain.ValidationError{Field: "person_id", Message: "учётная запись Госуслуг не найдена"}
	}
	now := s.Now()
	id := p.Identity
	id.VerifiedAt = now
	u, err := s.Users.FindByPhone(ctx, p.Phone)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		c, err := s.Registry.Lookup(ctx, p.CompanyINN)
		if err != nil {
			return nil, "", err
		}
		u = &domain.User{
			ID: newID(), Role: domain.RoleClient, Phone: p.Phone, FullName: id.FullName, Position: positionFor(c),
			Company: *c, Requisites: RequisitesFor(c), Balance: 250_000, CreatedAt: now,
		}
		if err := s.Users.CreateUser(ctx, u); err != nil {
			return nil, "", err
		}
		s.audit(ctx, "auth.register", u.ID, "ok", map[string]any{"inn": c.INN, "via": "esia"})
	case err != nil:
		return nil, "", err
	case u.IsEmployee():
		return nil, "", &domain.ValidationError{Field: "person_id", Message: "сотрудник банка входит в кабинет банка по телефону и паролю"}
	case u.Company.INN != p.CompanyINN:
		// Личность из Госуслуг подтверждает руководителя ДРУГОЙ компании: прикрепив её к этому аккаунту,
		// человек смог бы подать заявку за чужую компанию.
		return nil, "", &domain.ValidationError{Field: "person_id", Message: fmt.Sprintf(
			"этот телефон привязан к аккаунту компании «%s», а по данным Госуслуг вы руководите компанией с ИНН %s — заявку за неё из этого аккаунта не подать",
			u.Company.Name, p.CompanyINN)}
	}
	u, err = s.Users.UpdateUser(ctx, u.ID, func(x *domain.User) error { x.Identity, x.LastLoginAt = &id, now; return nil })
	if err != nil {
		return nil, "", err
	}
	s.audit(ctx, "auth.esia", u.ID, "ok", map[string]any{"ip": ip})
	tok, err := s.Sessions.Create(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	return u, tok, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) {
	if uid, ok := s.Sessions.Get(ctx, token); ok {
		s.audit(ctx, "auth.logout", uid, "ok", nil)
	}
	s.Sessions.Delete(ctx, token)
}

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
			if u.IsEmployee() { // должность сотрудника ведёт кадровая система, не он сам
				return &domain.ValidationError{Field: "position", Message: "должность сотрудника меняется через отдел кадров"}
			}
			u.Position = strings.TrimSpace(*p.Position)
		}
		return nil
	})
}

// positionFor — должность представителя бизнеса по типу компании.
func positionFor(c *domain.Company) string {
	if c.Type == "ИП" {
		return "Индивидуальный предприниматель"
	}
	return "Генеральный директор"
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

// DemoEmployee — сотрудник банка для жюри: входит тем же паролем и попадает в кабинет банка.
var DemoEmployee = domain.User{
	Phone: "79000000005", FullName: "Соколова Анна Дмитриевна", Position: "Кредитный аналитик",
	Email: "a.sokolova@alfabank.example",
	Employee: &domain.Employee{
		PersonnelNo: "АБ-04217", Department: "Кредитование малого и среднего бизнеса", Branch: "Операционный офис «Барнаульский»",
		Permissions: []string{domain.PermViewApplications, domain.PermViewAudit, domain.PermViewPlatform},
	},
}

const DemoPassword = "demo1234"

// SeedDemo создаёт недостающие демо-аккаунты. Идемпотентно: существующие записи не трогает,
// поэтому новый демо-аккаунт появляется и в базе, созданной прошлой версией.
func (s *AuthService) SeedDemo(ctx context.Context) error {
	exists := func(phone string) bool {
		_, err := s.Users.FindByPhone(ctx, phone)
		return err == nil
	}
	create := func(u *domain.User) error {
		hash, err := auth.HashPassword(DemoPassword) // своя соль у каждого аккаунта
		if err != nil {
			return err
		}
		u.PasswordHash = hash
		if err := s.Users.CreateUser(ctx, u); err != nil && !errors.Is(err, domain.ErrUserExists) {
			return err
		}
		return nil
	}
	for _, d := range DemoAccounts {
		if exists(d.Phone) {
			continue
		}
		c, err := s.Registry.Lookup(ctx, d.INN)
		if err != nil {
			return err
		}
		if err := create(&domain.User{
			ID: newID(), Role: domain.RoleClient, Phone: d.Phone, FullName: d.FullName, Position: positionFor(c),
			Email:   "demo" + d.Phone[len(d.Phone)-1:] + "@example.com",
			Company: *c, Requisites: RequisitesFor(c), Balance: d.Balance, CreatedAt: s.Now(),
		}); err != nil {
			return err
		}
	}
	if !exists(DemoEmployee.Phone) {
		e := DemoEmployee
		card := *DemoEmployee.Employee
		card.Permissions = append([]string(nil), card.Permissions...)
		e.ID, e.Role, e.CreatedAt, e.Employee = newID(), domain.RoleEmployee, s.Now(), &card
		if err := create(&e); err != nil {
			return err
		}
	}
	return nil
}
