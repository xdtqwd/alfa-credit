package pgstore

// Интеграционные тесты на настоящем PostgreSQL. Запуск:
//   TEST_DATABASE_URL='postgres://user@localhost:5432/db?sslmode=disable' go test ./internal/storage/pgstore/
// Без переменной тесты пропускаются (go test ./... остаётся быстрым и без зависимостей).

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/idempotency"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

func rnd() string { b := make([]byte, 4); _, _ = rand.Read(b); return hex.EncodeToString(b) }

// newStore — отдельная схема на тест: тесты не мешают друг другу и не трогают боевые таблицы.
func newStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционные тесты PostgreSQL пропущены")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "t_" + rnd()
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); admin.Close() })
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	s, err := Open(t.Context(), dsn+sep+"search_path="+schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

func seedUser(t *testing.T, s *Store, balance int64) *domain.User {
	t.Helper()
	u := &domain.User{ID: "u-" + rnd(), Phone: "7900" + rnd()[:7], FullName: "Тест", PasswordHash: "x", Balance: balance, CreatedAt: time.Now(),
		Company: domain.Company{INN: "7701234567", Name: "ООО «Тест»"}}
	if err := s.Users().CreateUser(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func newApp(clientID string) *domain.Application {
	now := time.Now()
	return &domain.Application{ID: "a-" + rnd(), ClientID: clientID, ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24,
		INN: "7701234567", Status: domain.StatusDraft, Checks: []domain.Check{}, Payments: []domain.Payment{},
		History: []domain.Event{{Status: domain.StatusDraft, At: now, Actor: "client"}}, CreatedAt: now, UpdatedAt: now, StatusChangedAt: now}
}

func TestMigrateIsIdempotent(t *testing.T) {
	s := newStore(t)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal("повторная миграция должна быть no-op:", err)
	}
}

type capturePub struct {
	mu   sync.Mutex
	evs  []events.Event
	fail bool
}

func (c *capturePub) Publish(_ context.Context, evs ...events.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("broker unavailable")
	}
	c.evs = append(c.evs, evs...)
	return nil
}

func TestOutboxWrittenInSameTransactionAndRelayed(t *testing.T) {
	s := newStore(t)
	ctx := obs.WithRequestID(t.Context(), "rid-42")
	u := seedUser(t, s, 0)
	a, err := s.Apps().Create(ctx, newApp(u.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.Number, "КР-") {
		t.Fatalf("number %q", a.Number)
	}
	if _, err := s.Apps().Update(ctx, a.ID, func(x *domain.Application) error {
		return x.Transition(domain.StatusSubmitted, "client", "", time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	// откат: ошибка внутри fn — ни изменения, ни события
	_, err = s.Apps().Update(ctx, a.ID, func(x *domain.Application) error {
		_ = x.Transition(domain.StatusDataCheck, "system", "", time.Now())
		return errors.New("integration failed")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if n, _ := s.OutboxPending(ctx); n != 2 {
		t.Fatalf("outbox pending = %d, want 2 (created + submitted; откатанный шаг не должен оставить события)", n)
	}

	pub := &capturePub{fail: true}
	if _, err := s.RelayOutbox(ctx, pub, 100); err == nil {
		t.Fatal("relay must fail while broker is down")
	}
	if n, _ := s.OutboxPending(ctx); n != 2 {
		t.Fatal("при недоступном брокере события остаются в outbox")
	}
	pub.fail = false
	if n, err := s.RelayOutbox(ctx, pub, 100); err != nil || n != 2 {
		t.Fatalf("relay: %d %v", n, err)
	}
	if n, _ := s.RelayOutbox(ctx, pub, 100); n != 0 {
		t.Fatal("отправленное повторно не отправляется")
	}
	if pub.evs[0].Type != events.TypeCreated || pub.evs[1].Type != events.TypeStatusChanged || pub.evs[1].Headers["request_id"] != "rid-42" {
		t.Fatalf("events: %+v", pub.evs)
	}
}

func TestListFilterSearchPagination(t *testing.T) {
	s := newStore(t)
	u := seedUser(t, s, 0)
	for i := 0; i < 5; i++ {
		_, _ = s.Apps().Create(t.Context(), newApp(u.ID))
	}
	page, _ := s.Apps().List(t.Context(), storage.Filter{ClientID: u.ID, Limit: 2, Offset: 2})
	total, _ := s.Apps().Count(t.Context(), storage.Filter{ClientID: u.ID})
	if len(page) != 2 || total != 5 {
		t.Fatalf("page=%d total=%d", len(page), total)
	}
	found, _ := s.Apps().List(t.Context(), storage.Filter{Query: page[0].Number})
	if len(found) != 1 || found[0].ID != page[0].ID {
		t.Fatal("search by number")
	}
	if n, _ := s.Apps().Count(t.Context(), storage.Filter{Query: "50%_"}); n != 0 {
		t.Fatal("LIKE wildcards in query must be escaped")
	}
}

// Деньги: списание со счёта и запись платежа — одна транзакция; конкурентные оплаты не задваиваются.
func TestPaymentsAtomicUnderConcurrency(t *testing.T) {
	s := newStore(t)
	u := seedUser(t, s, 10_000_000)
	svc := &service.Service{Repo: s.Apps(), Users: s.Users(), Registry: integrations.MockRegistry{}, Now: time.Now, Tx: s}
	a := newApp(u.ID)
	a.Amount, a.TermMonths, a.ProductID = 300_000, 3, "working-capital"
	a, _ = s.Apps().Create(t.Context(), a)
	_, _ = s.Apps().Update(t.Context(), a.ID, func(x *domain.Application) error {
		for _, st := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusApproved, domain.StatusSigning} {
			_ = x.Transition(st, "test", "", time.Now())
		}
		d := domain.Decision{Type: domain.DecisionApproved}
		d.SetOffer(domain.NewQuote(300_000, 21.5, 3))
		x.Decision = &d
		return nil
	})
	if _, err := svc.Sign(t.Context(), u.ID, a.ID, true); err != nil {
		t.Fatal(err)
	}
	afterIssue, _ := s.Users().GetUser(t.Context(), u.ID)
	if afterIssue.Balance != 10_300_000 {
		t.Fatalf("выдача должна зачислить сумму кредита: %d", afterIssue.Balance)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = svc.Pay(t.Context(), u.ID, a.ID) }()
	}
	wg.Wait()
	got, _ := s.Apps().Get(t.Context(), a.ID)
	final, _ := s.Users().GetUser(t.Context(), u.ID)
	var paid int64
	for _, p := range got.Payments {
		paid += p.Amount
	}
	if len(got.Payments) != 3 || got.Status != domain.StatusClosed {
		t.Fatalf("payments=%d status=%s (кредит на 3 мес. — ровно 3 платежа)", len(got.Payments), got.Status)
	}
	if afterIssue.Balance-final.Balance != paid {
		t.Fatalf("списано %d, записано платежей на %d", afterIssue.Balance-final.Balance, paid)
	}
}

func TestPaymentRollsBackWhenFundsInsufficient(t *testing.T) {
	s := newStore(t)
	u := seedUser(t, s, 0)
	svc := &service.Service{Repo: s.Apps(), Users: s.Users(), Now: time.Now, Tx: s}
	a := newApp(u.ID)
	a.Status = domain.StatusIssued
	d := domain.Decision{Type: domain.DecisionApproved}
	d.SetOffer(domain.NewQuote(3_000_000, 19.9, 24))
	a.Decision = &d
	a, _ = s.Apps().Create(t.Context(), a)
	if _, err := svc.Pay(t.Context(), u.ID, a.ID); !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("want insufficient funds, got %v", err)
	}
	got, _ := s.Apps().Get(t.Context(), a.ID)
	if len(got.Payments) != 0 {
		t.Fatal("при отказе платёж не должен записаться")
	}
}

func TestBalanceCannotGoNegativeAtDBLevel(t *testing.T) {
	s := newStore(t)
	u := seedUser(t, s, 100)
	_, err := s.Users().UpdateUser(t.Context(), u.ID, func(x *domain.User) error { x.Balance = -1; return nil })
	if err == nil || !strings.Contains(err.Error(), "balance") {
		t.Fatalf("CHECK (balance >= 0) must reject negative balance, got %v", err)
	}
}

func TestAuditLogIsAppendOnly(t *testing.T) {
	s := newStore(t)
	if err := s.WriteAudit(t.Context(), obs.AuditEntry{At: time.Now(), Actor: "u1", Action: "loan.pay", Entity: "application", EntityID: "a1", Outcome: "ok", Details: map[string]any{"amount": 100}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`UPDATE audit_log SET outcome='denied'`); err == nil {
		t.Fatal("audit_log must reject UPDATE")
	}
	if _, err := s.DB.Exec(`DELETE FROM audit_log`); err == nil {
		t.Fatal("audit_log must reject DELETE")
	}
	es, _ := s.AuditFor(t.Context(), "application", "a1")
	if len(es) != 1 || es[0].Details["amount"] != float64(100) {
		t.Fatalf("audit: %+v", es)
	}
}

func TestSessionsStoreOnlyTokenHash(t *testing.T) {
	s := newStore(t)
	u := seedUser(t, s, 0)
	ss := s.Sessions(time.Hour)
	tok, err := ss.Create(t.Context(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.DB.QueryRow(`SELECT count(*) FROM sessions WHERE token_hash=$1`, tok).Scan(&n)
	if n != 0 {
		t.Fatal("сырой токен не должен храниться в БД")
	}
	if uid, ok := ss.Get(t.Context(), tok); !ok || uid != u.ID {
		t.Fatal("session must resolve")
	}
	// сессию видит и другой экземпляр API (горизонтальное масштабирование)
	if uid, ok := s.Sessions(time.Hour).Get(t.Context(), tok); !ok || uid != u.ID {
		t.Fatal("another instance must accept the session")
	}
	ss.Delete(t.Context(), tok)
	if _, ok := ss.Get(t.Context(), tok); ok {
		t.Fatal("deleted session must not resolve")
	}
}

func TestIdempotencyAndInbox(t *testing.T) {
	s := newStore(t)
	st := idempotency.PG{DB: s.DB}
	ctx := t.Context()
	if _, owned, _ := st.Begin(ctx, "c1", "k", "h1"); !owned {
		t.Fatal("first begin must own the key")
	}
	if r, owned, _ := st.Begin(ctx, "c1", "k", "h1"); owned || r.Status != 0 {
		t.Fatal("in-flight key must be reported as in progress")
	}
	_ = st.Complete(ctx, "c1", "k", 200, []byte(`{"ok":true}`))
	if r, owned, _ := st.Begin(ctx, "c1", "k", "h1"); owned || r.Status != 200 || string(r.Body) != `{"ok":true}` {
		t.Fatalf("replay: %+v", r)
	}
	if _, owned, _ := st.Begin(ctx, "c2", "k", "h1"); !owned {
		t.Fatal("keys are scoped per client")
	}
	_, _, _ = st.Begin(ctx, "c3", "k", "h")
	_ = st.Release(ctx, "c3", "k")
	if _, owned, _ := st.Begin(ctx, "c3", "k", "h"); !owned {
		t.Fatal("released key can be retried")
	}

	if fresh, _ := s.MarkProcessed(ctx, "g", "e1"); !fresh {
		t.Fatal("first delivery")
	}
	if fresh, _ := s.MarkProcessed(ctx, "g", "e1"); fresh {
		t.Fatal("duplicate delivery must be detected")
	}
	if fresh, _ := s.MarkProcessed(ctx, "other", "e1"); !fresh {
		t.Fatal("inbox is per consumer group")
	}
}
