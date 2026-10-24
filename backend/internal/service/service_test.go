package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/auth"
	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

var ctx = context.Background()

type env struct {
	svc   *Service
	auth  *AuthService
	users storage.UserRepository
	repo  storage.Repository
}

func newEnv(t *testing.T) env {
	t.Helper()
	repo, _ := storage.NewFileStore("")
	users, _ := storage.NewUserFileStore("")
	reg := integrations.MockRegistry{}
	a := &AuthService{Users: users, Registry: reg, Now: time.Now, Sessions: auth.NewSessions(time.Hour), Limiter: auth.NewLimiter(5, time.Minute)}
	if err := a.SeedDemo(ctx); err != nil {
		t.Fatal(err)
	}
	return env{svc: &Service{Repo: repo, Users: users, Registry: reg, Now: time.Now}, auth: a, users: users, repo: repo}
}

func (e env) user(t *testing.T, phone string) *domain.User {
	t.Helper()
	u, err := e.users.FindByPhone(ctx, phone)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLoginAndRegister(t *testing.T) {
	e := newEnv(t)
	if _, tok, err := e.auth.Login(ctx, "+7 (900) 000-00-01", DemoPassword, "1.1.1.1"); err != nil || tok == "" {
		t.Fatalf("login with formatted phone: %v", err)
	}
	if _, _, err := e.auth.Login(ctx, "89000000001", DemoPassword, "1.1.1.1"); err != nil {
		t.Fatal("8XXXXXXXXXX must be normalized to 7XXXXXXXXXX")
	}
	for i := 0; i < 5; i++ {
		e.auth.Login(ctx, "79000000002", "wrong", "2.2.2.2")
	}
	if _, _, err := e.auth.Login(ctx, "79000000002", DemoPassword, "2.2.2.2"); !errors.Is(err, domain.ErrTooManyAttempts) {
		t.Fatalf("must lock after 5 wrong passwords, got %v", err)
	}
	u, _, err := e.auth.Register(ctx, RegisterInput{Phone: "+7 913 111 22 33", Password: "longpass1", FullName: "Новый Клиент", INN: "5555555555"})
	if err != nil || u.Company.INN != "5555555555" || u.Requisites.Account == "" {
		t.Fatalf("register must pull company and requisites: %+v %v", u, err)
	}
	var ve *domain.ValidationError
	if _, _, err := e.auth.Register(ctx, RegisterInput{Phone: "+7 913 111 22 33", Password: "longpass1", FullName: "Дубль Дублев", INN: "5555555555"}); !errors.As(err, &ve) || ve.Field != "phone" {
		t.Fatalf("duplicate phone: %v", err)
	}
	if _, _, err := e.auth.Register(ctx, RegisterInput{Phone: "+7 913 111 22 44", Password: "short", FullName: "Новый Клиент", INN: "5555555555"}); !errors.As(err, &ve) || ve.Field != "password" {
		t.Fatalf("short password: %v", err)
	}
	if _, _, err := e.auth.Register(ctx, RegisterInput{Phone: "+7 913 111 22 55", Password: "longpass1", FullName: "Новый Клиент", INN: "0000123456"}); !errors.As(err, &ve) || ve.Field != "inn" {
		t.Fatalf("unknown INN: %v", err)
	}
}

// flakyUsers — хранилище пользователей, у которого можно «уронить» поиск или изменение.
type flakyUsers struct {
	storage.UserRepository
	findErr, updateErr error
}

func (f flakyUsers) FindByPhone(ctx context.Context, phone string) (*domain.User, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.UserRepository.FindByPhone(ctx, phone)
}

func (f flakyUsers) UpdateUser(ctx context.Context, id string, fn func(u *domain.User) error) (*domain.User, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.UserRepository.UpdateUser(ctx, id, fn)
}

func TestLoginReportsStorageErrors(t *testing.T) {
	e := newEnv(t)
	errDB := errors.New("db down")
	// сбой поиска — это не «неверный пароль» и не попытка подбора: блокировки входа быть не должно
	e.auth.Users = flakyUsers{UserRepository: e.users, findErr: errDB}
	for range 5 {
		if _, _, err := e.auth.Login(ctx, "79000000001", DemoPassword, "3.3.3.3"); !errors.Is(err, errDB) {
			t.Fatalf("find failure: got %v, want %v", err, errDB)
		}
	}
	// сбой записи времени входа — ошибка, а не паника на пустом пользователе
	e.auth.Users = flakyUsers{UserRepository: e.users, updateErr: errDB}
	if _, _, err := e.auth.Login(ctx, "79000000001", DemoPassword, "3.3.3.3"); !errors.Is(err, errDB) {
		t.Fatalf("update failure: got %v, want %v", err, errDB)
	}
	e.auth.Users = e.users
	if _, _, err := e.auth.Login(ctx, "79000000001", DemoPassword, "3.3.3.3"); err != nil {
		t.Fatalf("login after storage recovery: %v", err)
	}
	if _, _, err := e.auth.Login(ctx, "79000000099", DemoPassword, "4.4.4.4"); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("unknown phone: got %v, want invalid credentials", err)
	}
}

func TestCreatePrefillsAndClamps(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000001")
	a, err := e.svc.Create(ctx, u.ID, CreateInput{ProductID: "overdraft", Amount: 999_000_000, TermMonths: 1})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := domain.FindProduct("overdraft")
	if a.Amount != p.MaxAmount || a.TermMonths != p.MinTerm {
		t.Fatalf("must clamp to product limits: %d / %d", a.Amount, a.TermMonths)
	}
	if a.Company == nil || a.Company.INN != u.Company.INN || a.Contact.FullName != u.FullName {
		t.Fatal("company and contact must be prefilled from profile")
	}
	if _, err := e.svc.Create(ctx, u.ID, CreateInput{ProductID: "nope"}); err == nil {
		t.Fatal("unknown product must fail")
	}
}

func TestForeignApplicationIsInvisible(t *testing.T) {
	e := newEnv(t)
	owner, other := e.user(t, "79000000001"), e.user(t, "79000000002")
	a, _ := e.svc.Create(ctx, owner.ID, CreateInput{ProductID: "business-dev"})
	checks := map[string]func() error{
		"get":    func() error { _, err := e.svc.Get(ctx, other.ID, a.ID); return err },
		"patch":  func() error { _, err := e.svc.Patch(ctx, other.ID, a.ID, domain.DraftPatch{}); return err },
		"submit": func() error { _, err := e.svc.Submit(ctx, other.ID, a.ID); return err },
		"cancel": func() error { _, err := e.svc.Cancel(ctx, other.ID, a.ID); return err },
		"pay":    func() error { _, err := e.svc.Pay(ctx, other.ID, a.ID); return err },
	}
	for name, f := range checks {
		if err := f(); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s on foreign application: want ErrNotFound, got %v", name, err)
		}
	}
	if list, _ := e.svc.List(ctx, other.ID); len(list) != 0 {
		t.Fatal("foreign application leaked into list")
	}
}

// approve подводит заявку в статус approved с окончательным решением (конвейер тестируется отдельно).
func approve(t *testing.T, e env, clientID string, amount int64, rate float64) *domain.Application {
	t.Helper()
	a, _ := e.svc.Create(ctx, clientID, CreateInput{ProductID: "business-dev", Amount: amount, TermMonths: 12})
	a, err := e.repo.Update(ctx, a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusApproved} {
			if err := x.Transition(s, "test", "", time.Now()); err != nil {
				return err
			}
		}
		d := domain.Decision{Type: domain.DecisionApproved, Final: true}
		d.SetOffer(domain.NewQuote(amount, rate, 12))
		x.Decision = &d
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSignCreditsBalanceAndPayDebits(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000001")
	a := approve(t, e, u.ID, 1_000_000, 12)
	if _, err := e.svc.Sign(ctx, u.ID, a.ID, true); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("cannot sign before accepting")
	}
	if _, err := e.svc.Accept(ctx, u.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Sign(ctx, u.ID, a.ID, false); err == nil {
		t.Fatal("sign without confirmation must fail")
	}
	if _, err := e.svc.Sign(ctx, u.ID, a.ID, true); err != nil {
		t.Fatal(err)
	}
	after := e.user(t, "79000000001")
	if after.Balance != u.Balance+1_000_000 {
		t.Fatalf("issue must credit the account: %d → %d", u.Balance, after.Balance)
	}
	sched := domain.Schedule(1_000_000, 12, 12)
	paid, err := e.svc.Pay(ctx, u.ID, a.ID)
	if err != nil || len(paid.Payments) != 1 || paid.Payments[0].Amount != sched[0].Payment {
		t.Fatalf("pay: %v %+v", err, paid)
	}
	if got := e.user(t, "79000000001").Balance; got != after.Balance-sched[0].Payment {
		t.Fatalf("pay must debit exactly the schedule payment: %d", got)
	}
}

func TestPayInsufficientFundsChangesNothing(t *testing.T) {
	e := newEnv(t)
	poor := e.user(t, "79000000004") // баланс 15 000 ₽
	a := approve(t, e, poor.ID, 1_000_000, 12)
	e.svc.Accept(ctx, poor.ID, a.ID)
	e.svc.Sign(ctx, poor.ID, a.ID, true)
	// списываем почти всё, чтобы на платёж не хватило
	e.users.UpdateUser(ctx, poor.ID, func(u *domain.User) error { u.Balance = 10; return nil })
	if _, err := e.svc.Pay(ctx, poor.ID, a.ID); !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}
	got, _ := e.svc.Get(ctx, poor.ID, a.ID)
	if len(got.Payments) != 0 || e.user(t, "79000000004").Balance != 10 {
		t.Fatal("failed payment must not record anything or move money")
	}
}

func TestPayUntilClosed(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000001")
	a := approve(t, e, u.ID, 300_000, 12)
	e.svc.Accept(ctx, u.ID, a.ID)
	e.svc.Sign(ctx, u.ID, a.ID, true)
	var last *domain.Application
	for i := 0; i < 12; i++ {
		var err error
		if last, err = e.svc.Pay(ctx, u.ID, a.ID); err != nil {
			t.Fatalf("payment %d: %v", i+1, err)
		}
	}
	if last.Status != domain.StatusClosed {
		t.Fatalf("after all payments status = %s", last.Status)
	}
	if _, err := e.svc.Pay(ctx, u.ID, a.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("no payments after closing")
	}
	var total int64
	for _, p := range last.Payments {
		total += p.Amount
	}
	if total != domain.NewQuote(300_000, 12, 12).Total {
		t.Fatalf("sum of payments %d must equal quote total", total)
	}
}

func TestAcceptCounterOfferAppliesNewTerms(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000002")
	a, _ := e.svc.Create(ctx, u.ID, CreateInput{ProductID: "business-dev", Amount: 20_000_000, TermMonths: 24})
	e.repo.Update(ctx, a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusCounterOffer} {
			x.Transition(s, "test", "", time.Now())
		}
		d := domain.Decision{Type: domain.DecisionCounter}
		d.SetOffer(domain.NewQuote(10_000_000, 21.4, 36))
		x.Decision = &d
		return nil
	})
	// предварительное контрпредложение: условия фиксируются и открывается полная анкета
	got, err := e.svc.Accept(ctx, u.ID, a.ID)
	if err != nil || got.Amount != 10_000_000 || got.TermMonths != 36 || got.Status != domain.StatusFullForm || got.Level != 2 {
		t.Fatalf("counter accept: %v %+v", err, got)
	}
	if got.Preliminary == nil || got.Preliminary.Rate != 21.4 || got.Full == nil || got.Full.Guarantor == nil {
		t.Fatalf("preliminary terms must be kept, guarantor suggested for >5 mln: %+v %+v", got.Preliminary, got.Full)
	}
}

func TestTwoLevelForm(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000001")
	a, _ := e.svc.Create(ctx, u.ID, CreateInput{ProductID: "business-dev", Amount: 2_000_000, TermMonths: 24})
	e.repo.Update(ctx, a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusApproved} {
			x.Transition(s, "test", "", time.Now())
		}
		d := domain.Decision{Type: domain.DecisionApproved} // предварительное
		d.SetOffer(domain.NewQuote(2_000_000, 19.9, 24))
		x.Decision = &d
		return nil
	})
	if _, err := e.svc.Sign(ctx, u.ID, a.ID, true); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("cannot sign on a preliminary decision")
	}
	got, err := e.svc.Accept(ctx, u.ID, a.ID)
	if err != nil || got.Status != domain.StatusFullForm {
		t.Fatalf("accept preliminary: %v %+v", err, got)
	}
	// быстрая анкета закрыта, правится только полная
	amount := int64(9_000_000)
	var ve *domain.ValidationError
	if _, err := e.svc.Patch(ctx, u.ID, a.ID, domain.DraftPatch{Amount: &amount}); !errors.As(err, &ve) {
		t.Fatalf("level-1 fields must be locked on level 2: %v", err)
	}
	if _, err := e.svc.Submit(ctx, u.ID, a.ID); !errors.As(err, &ve) || ve.Field != "full_form.employees" {
		t.Fatalf("empty full form must not be submitted: %v", err)
	}
	full := domain.FullForm{Employees: 12, Premises: "rent", PurposeDetails: "Ремонт второго зала и закупка мебели",
		Collateral: domain.Collateral{Type: domain.CollateralNone}, StatementsConsent: true}
	if _, err := e.svc.Patch(ctx, u.ID, a.ID, domain.DraftPatch{FullForm: &full}); err != nil {
		t.Fatal(err)
	}
	got, err = e.svc.Submit(ctx, u.ID, a.ID)
	if err != nil || got.Status != domain.StatusVerification || got.Preliminary == nil {
		t.Fatalf("full form submit: %v %+v", err, got)
	}
}

func TestSubmitResetsPreviousDecision(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000001")
	a, _ := e.svc.Create(ctx, u.ID, CreateInput{ProductID: "business-dev"})
	rev := domain.Financials{AnnualRevenue: 80_000_000}
	consent := true
	e.svc.Patch(ctx, u.ID, a.ID, domain.DraftPatch{Financials: &rev, Consent: &consent})
	e.repo.Update(ctx, a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusNeedInfo} {
			x.Transition(s, "test", "", time.Now())
		}
		x.Decision = &domain.Decision{Type: domain.DecisionNeedInfo, Reasons: []string{"x"}}
		x.Checks = []domain.Check{{Code: "revenue"}}
		return nil
	})
	got, err := e.svc.Submit(ctx, u.ID, a.ID)
	if err != nil || got.Decision != nil || len(got.Checks) != 0 || got.Status != domain.StatusSubmitted {
		t.Fatalf("resubmit must clear old decision: %v %+v", err, got)
	}
	if _, err := e.svc.Patch(ctx, u.ID, a.ID, domain.DraftPatch{Financials: &rev}); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("cannot edit after submit")
	}
}

// Регрессия: при одобрении договор заключается на условиях решения, а не анкеты.
func TestAcceptAlwaysUsesDecisionTerms(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000001")
	a := approve(t, e, u.ID, 1_000_000, 12)
	e.repo.Update(ctx, a.ID, func(x *domain.Application) error { x.Amount = 5_000_000; return nil }) // анкета разошлась с решением
	got, err := e.svc.Accept(ctx, u.ID, a.ID)
	if err != nil || got.Amount != 1_000_000 || got.TermMonths != 12 {
		t.Fatalf("accepted terms must equal decision: %v %d/%d", err, got.Amount, got.TermMonths)
	}
	if _, err := e.svc.Accept(ctx, u.ID, a.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("double accept must be rejected")
	}
}

func TestSeedDemoIsIdempotentAndAddsEmployee(t *testing.T) {
	ctx := context.Background()
	users, _ := storage.NewUserFileStore("")
	a := &AuthService{Users: users, Registry: integrations.MockRegistry{}, Now: time.Now}
	for range 2 {
		if err := a.SeedDemo(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := users.CountUsers(ctx); n != len(DemoAccounts)+1 {
		t.Fatalf("users = %d, want %d", n, len(DemoAccounts)+1)
	}
	e, err := users.FindByPhone(ctx, DemoEmployee.Phone)
	if err != nil || !e.IsEmployee() || !e.Can(domain.PermViewAudit) || e.Company.INN != "" {
		t.Fatalf("employee = %+v, %v", e, err)
	}
	c, _ := users.FindByPhone(ctx, DemoAccounts[0].Phone)
	if c.IsEmployee() || c.Can(domain.PermViewApplications) {
		t.Fatal("client must have no bank permissions")
	}
}
