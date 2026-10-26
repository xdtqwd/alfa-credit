package service

import (
	"context"
	"errors"
	"strings"
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
	a := &AuthService{Users: users, Registry: reg, ESIA: integrations.MockESIA{}, Now: time.Now, Sessions: auth.NewSessions(time.Hour), Limiter: auth.NewLimiter(5, time.Minute)}
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

// esia — вход через Госуслуги демо-человеком (esia-1 — руководитель «Северного ветра», …).
func (e env) esia(t *testing.T, personID string) *domain.User {
	t.Helper()
	u, tok, err := e.auth.ESIALogin(ctx, personID, "9.9.9.9")
	if err != nil || tok == "" || u.Identity == nil {
		t.Fatalf("esia login %s: %v %+v", personID, err, u)
	}
	return u
}

func level1(inn string, amount int64) domain.Level1 {
	return domain.Level1{ProductID: "business-dev", Amount: amount, TermMonths: 24, INN: inn,
		Financials: domain.Financials{AnnualRevenue: 80_000_000}}
}

// Предварительный расчёт 1-го уровня — без БКИ: компания, сверка с ФНС и нагрузка.
func TestPrescore(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name string
		in   domain.Level1
		want domain.DecisionType
	}{
		{"healthy → approved", level1("7701234567", 3_000_000), domain.DecisionApproved},
		{"too big → counter", domain.Level1{ProductID: "business-dev", Amount: 20_000_000, TermMonths: 24, INN: "2224000012",
			Financials: domain.Financials{AnnualRevenue: 18_000_000, MonthlyObligations: 200_000}}, domain.DecisionCounter},
		{"revenue ≫ ФНС → need info", domain.Level1{ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24, INN: "2224000012",
			Financials: domain.Financials{AnnualRevenue: 50_000_000}}, domain.DecisionNeedInfo},
		{"liquidated → rejected", level1("6600000000", 1_000_000), domain.DecisionRejected},
		{"not in registry → rejected", level1("0000123456", 1_000_000), domain.DecisionRejected},
		// просрочки в БКИ на 1-м уровне не видны — отказ будет на полном скоринге
		{"overdue company passes prescore", domain.Level1{ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24, INN: "5401000009",
			Financials: domain.Financials{AnnualRevenue: 30_000_000, MonthlyObligations: 300_000}}, domain.DecisionApproved},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := c.in
			res, err := e.svc.Prescore(ctx, &in)
			if err != nil || res.Decision.Type != c.want || res.Decision.Final || len(res.Decision.Reasons) == 0 {
				t.Fatalf("prescore: %v %+v", err, res)
			}
			for _, f := range res.Factors {
				if strings.Contains(f.Name, "БКИ") || strings.Contains(f.Name, "Просрочки") {
					t.Fatalf("prescore must not use the credit bureau: %+v", res.Factors)
				}
			}
		})
	}
	bad := level1("12x", 1_000_000)
	var ve *domain.ValidationError
	if _, err := e.svc.Prescore(ctx, &bad); !errors.As(err, &ve) || ve.Field != "inn" {
		t.Fatalf("invalid INN: %v", err)
	}
}

func TestESIALogin(t *testing.T) {
	e := newEnv(t)
	u := e.esia(t, "esia-1")
	if u.Phone != "79000000001" || u.Identity.Passport.Number != "345678" || u.Identity.VerifiedAt.IsZero() {
		t.Fatalf("existing demo account must get the Gosuslugi identity: %+v", u)
	}
	if again := e.user(t, "79000000001"); again.Identity == nil {
		t.Fatal("identity must be saved in the profile")
	}
	ip := e.esia(t, "esia-5") // у Иванова аккаунта не было — создаётся по данным реестра
	if ip.Company.INN != "222400000012" || ip.Company.Type != "ИП" || ip.Position != "Индивидуальный предприниматель" || ip.Requisites.Account == "" {
		t.Fatalf("new account from Gosuslugi: %+v", ip)
	}
	var ve *domain.ValidationError
	if _, _, err := e.auth.ESIALogin(ctx, "esia-404", "9.9.9.9"); !errors.As(err, &ve) {
		t.Fatalf("unknown person: %v", err)
	}
	if persons := e.auth.ESIAPersons(ctx); len(persons) != 5 || persons[0].CompanyName != "ООО «Северный ветер»" || strings.Contains(persons[0].SNILS, "233") {
		t.Fatalf("persons view must name the company and mask SNILS: %+v", persons)
	}
}

// Регрессия: личность руководителя одной компании нельзя прикрепить к аккаунту другой компании
// с тем же телефоном — иначе по ней подавалась бы заявка за чужую компанию.
func TestESIALoginRejectsAccountOfAnotherCompany(t *testing.T) {
	e := newEnv(t)
	other, _, err := e.auth.Register(ctx, RegisterInput{Phone: "79000000006", Password: "longpass1", FullName: "Иванов Иван Иванович", INN: "5555555555"})
	if err != nil {
		t.Fatal(err)
	}
	var ve *domain.ValidationError
	if _, _, err := e.auth.ESIALogin(ctx, "esia-5", "9.9.9.9"); !errors.As(err, &ve) || ve.Field != "person_id" {
		t.Fatalf("esia-5 runs 222400000012, the account is 5555555555: want person_id error, got %v", err)
	}
	if got, _ := e.users.GetUser(ctx, other.ID); got.Identity != nil {
		t.Fatal("identity must not be attached to an account of another company")
	}
}

// Заявка создаётся только после входа через Госуслуги и только от компании, которой человек руководит.
func TestCreateAfterGosuslugi(t *testing.T) {
	e := newEnv(t)
	var ve *domain.ValidationError
	phoneOnly := e.user(t, "79000000001")
	if _, err := e.svc.Create(ctx, phoneOnly.ID, level1("7701234567", 3_000_000)); !errors.As(err, &ve) || ve.Field != "identity" {
		t.Fatalf("without Gosuslugi: %v", err)
	}
	u := e.esia(t, "esia-1")
	if _, err := e.svc.Create(ctx, u.ID, level1("2224000012", 3_000_000)); !errors.As(err, &ve) || ve.Field != "inn" {
		t.Fatalf("someone else's company: %v", err)
	}
	if _, err := e.svc.Create(ctx, u.ID, level1("7701234567", 999_000_000)); !errors.As(err, &ve) || ve.Field != "amount" {
		t.Fatalf("amount out of range: %v", err)
	}
	a, err := e.svc.Create(ctx, u.ID, level1("7701234567", 3_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != domain.StatusFullForm || a.Level != 2 || a.Preliminary == nil || a.Preliminary.Final || a.Identity == nil {
		t.Fatalf("application must start at level 2 with the preliminary decision: %+v", a)
	}
	if a.Company == nil || a.Contact.FullName != u.FullName || a.Full == nil || a.Full.DirectorPassport == nil || len(a.Checks) == 0 {
		t.Fatalf("company, contact, checks and director passport must be prefilled: %+v %+v", a, a.Full)
	}
	orlov := e.esia(t, "esia-4") // компания ликвидирована — предварительный расчёт отказал
	if _, err := e.svc.Create(ctx, orlov.ID, level1("6600000000", 1_000_000)); !errors.As(err, &ve) || ve.Field != "prescore" {
		t.Fatalf("rejected prescore: %v", err)
	}
}

func TestForeignApplicationIsInvisible(t *testing.T) {
	e := newEnv(t)
	owner, other := e.esia(t, "esia-1"), e.user(t, "79000000002")
	a, err := e.svc.Create(ctx, owner.ID, level1("7701234567", 3_000_000))
	if err != nil {
		t.Fatal(err)
	}
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

// decide подводит заявку 2-го уровня к окончательному решению (конвейер тестируется отдельно).
func decide(t *testing.T, e env, phone string, amount int64, to domain.Status, d domain.Decision) *domain.Application {
	t.Helper()
	persons := map[string]string{"79000000001": "esia-1", "79000000002": "esia-2", "79000000004": "esia-4"}
	u := e.esia(t, persons[phone])
	in := level1(u.Company.INN, amount)
	if phone == "79000000004" { // ликвидированная компания: заявку подводим к решению напрямую
		in.INN = "7701234567"
	}
	a, err := e.svc.Create(ctx, u.ID, in)
	if err != nil {
		a = &domain.Application{ID: "forced-" + phone, ClientID: u.ID, ProductID: "business-dev", Amount: amount, TermMonths: 12,
			Status: domain.StatusFullForm, Level: 2, Payments: []domain.Payment{}, Checks: []domain.Check{}}
		if a, err = e.repo.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	a, err = e.repo.Update(ctx, a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, to} {
			if err := x.Transition(s, "test", "", time.Now()); err != nil {
				return err
			}
		}
		d.Final = true
		x.Decision = &d
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// approve подводит заявку к окончательному одобрению на сумму amount под rate на 12 месяцев.
func approve(t *testing.T, e env, clientID string, amount int64, rate float64) *domain.Application {
	t.Helper()
	u, err := e.users.GetUser(ctx, clientID)
	if err != nil {
		t.Fatal(err)
	}
	d := domain.Decision{Type: domain.DecisionApproved}
	d.SetOffer(domain.NewQuote(amount, rate, 12))
	return decide(t, e, u.Phone, amount, domain.StatusApproved, d)
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

// unsavedRepo — хранилище, которое выполняет изменение, но не может его записать (например, диск полон).
type unsavedRepo struct{ storage.Repository }

func (r unsavedRepo) Update(ctx context.Context, id string, fn func(a *domain.Application) error) (*domain.Application, error) {
	a, err := r.Repository.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := fn(a); err != nil {
		return nil, err
	}
	return nil, errors.New("disk full")
}

// Регрессия: в файловом режиме (без транзакций) подпись атомарна — заявка не становится «выданной»
// без зачисления денег, а деньги не остаются на счёте, если заявка не сохранилась.
func TestSignCompensatesWhenApplicationNotSaved(t *testing.T) {
	e := newEnv(t)
	u := e.user(t, "79000000001")
	a := approve(t, e, u.ID, 1_000_000, 12)
	if _, err := e.svc.Accept(ctx, u.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	// счёт недоступен — заявка остаётся на подписании
	e.svc.Users = flakyUsers{UserRepository: e.users, updateErr: errors.New("db down")}
	if _, err := e.svc.Sign(ctx, u.ID, a.ID, true); err == nil {
		t.Fatal("sign must fail when the account cannot be credited")
	}
	if got, _ := e.svc.Get(ctx, u.ID, a.ID); got.Status != domain.StatusSigning {
		t.Fatalf("application must not be issued without money, got %s", got.Status)
	}
	e.svc.Users = e.users
	// заявка не сохранилась — зачисление откатывается
	e.svc.Repo = unsavedRepo{e.repo}
	if _, err := e.svc.Sign(ctx, u.ID, a.ID, true); err == nil {
		t.Fatal("sign must fail when the application is not saved")
	}
	if got := e.user(t, "79000000001").Balance; got != u.Balance {
		t.Fatalf("credited amount must be reverted: %d → %d", u.Balance, got)
	}
	e.svc.Repo = e.repo
	if got, _ := e.svc.Get(ctx, u.ID, a.ID); got.Status != domain.StatusSigning {
		t.Fatalf("application must stay in signing, got %s", got.Status)
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
	d := domain.Decision{Type: domain.DecisionCounter}
	d.SetOffer(domain.NewQuote(4_000_000, 21.4, 36))
	a := decide(t, e, "79000000002", 3_000_000, domain.StatusCounterOffer, d)
	got, err := e.svc.Accept(ctx, a.ClientID, a.ID)
	if err != nil || got.Amount != 4_000_000 || got.TermMonths != 36 || got.Status != domain.StatusSigning {
		t.Fatalf("counter accept must apply the bank's terms: %v %+v", err, got)
	}
}

// 2-й уровень: до отправки подписать и принять нечего, отправляется только заполненная анкета.
func TestLevel2Submit(t *testing.T) {
	e := newEnv(t)
	u := e.esia(t, "esia-1")
	a, err := e.svc.Create(ctx, u.ID, level1("7701234567", 3_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Sign(ctx, u.ID, a.ID, true); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("cannot sign on a preliminary decision")
	}
	if _, err := e.svc.Accept(ctx, u.ID, a.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("preliminary decision cannot be accepted — only the final one")
	}
	var ve *domain.ValidationError
	if _, err := e.svc.Submit(ctx, u.ID, a.ID); !errors.As(err, &ve) || ve.Field != "full_form.consent" {
		t.Fatalf("level 2 without consent must not be submitted: %v", err)
	}
	full := *a.Full
	full.Consent = true
	if _, err := e.svc.Patch(ctx, u.ID, a.ID, domain.DraftPatch{FullForm: &full}); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Submit(ctx, u.ID, a.ID)
	if err != nil || got.Status != domain.StatusSubmitted || got.Preliminary == nil || len(got.Checks) != 0 {
		t.Fatalf("level 2 submit: %v %+v", err, got)
	}
	if _, err := e.svc.Patch(ctx, u.ID, a.ID, domain.DraftPatch{FullForm: &full}); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("cannot edit after submit")
	}
}

func TestSubmitResetsPreviousDecision(t *testing.T) {
	e := newEnv(t)
	u := e.esia(t, "esia-1")
	a, _ := e.svc.Create(ctx, u.ID, level1("7701234567", 3_000_000))
	full := *a.Full
	full.Consent = true
	e.svc.Patch(ctx, u.ID, a.ID, domain.DraftPatch{FullForm: &full})
	e.repo.Update(ctx, a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusNeedInfo} {
			x.Transition(s, "test", "", time.Now())
		}
		x.Decision = &domain.Decision{Type: domain.DecisionNeedInfo, Reasons: []string{"x"}, Final: true}
		x.Checks = []domain.Check{{Code: "passport"}}
		return nil
	})
	got, err := e.svc.Submit(ctx, u.ID, a.ID)
	if err != nil || got.Decision != nil || len(got.Checks) != 0 || got.Status != domain.StatusSubmitted {
		t.Fatalf("resubmit must clear old decision: %v %+v", err, got)
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
