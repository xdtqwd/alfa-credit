package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

var ctx = context.Background()

func newApp(id, client string, st domain.Status, at time.Time) *domain.Application {
	return &domain.Application{ID: id, ClientID: client, ProductID: "business-dev", Amount: 1_000_000, TermMonths: 12, Status: st, CreatedAt: at}
}

func TestFileStorePersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apps.json")
	s, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t1 := time.Now()
	a, _ := s.Create(ctx, newApp("a1", "client-1", domain.StatusDraft, t1))
	b, _ := s.Create(ctx, newApp("b1", "client-2", domain.StatusSubmitted, t1.Add(time.Second)))
	if a.Number != "КР-000001" || b.Number != "КР-000002" {
		t.Fatalf("numbers: %s %s", a.Number, b.Number)
	}
	s2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get(ctx, "a1")
	if err != nil || got.ClientID != "client-1" {
		t.Fatalf("owner must survive restart: %+v %v", got, err)
	}
	c, _ := s2.Create(ctx, newApp("c1", "client-1", domain.StatusDraft, t1))
	if c.Number != "КР-000003" {
		t.Fatalf("sequence must continue after restart, got %s", c.Number)
	}
	mine, _ := s2.List(ctx, Filter{ClientID: "client-1"})
	if len(mine) != 2 {
		t.Fatalf("client filter: %d", len(mine))
	}
	sub, _ := s2.List(ctx, Filter{Statuses: []domain.Status{domain.StatusSubmitted}})
	if len(sub) != 1 || sub[0].ID != "b1" {
		t.Fatal("status filter")
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp file must be renamed away (atomic write)")
	}
}

func TestFileStoreUpdateRollbackAndIsolation(t *testing.T) {
	s, _ := NewFileStore("")
	s.Create(ctx, newApp("a1", "c", domain.StatusDraft, time.Now()))
	boom := errors.New("boom")
	if _, err := s.Update(ctx, "a1", func(a *domain.Application) error { a.Amount = 1; return boom }); !errors.Is(err, boom) {
		t.Fatal("error must propagate")
	}
	got, _ := s.Get(ctx, "a1")
	if got.Amount != 1_000_000 {
		t.Fatal("failed update must not change stored state")
	}
	got.Amount = 42 // мутация копии не должна протекать в хранилище
	again, _ := s.Get(ctx, "a1")
	if again.Amount != 1_000_000 {
		t.Fatal("Get must return a copy")
	}
	if _, err := s.Get(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing must be ErrNotFound")
	}
	if _, err := s.Update(ctx, "missing", func(*domain.Application) error { return nil }); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("update missing must be ErrNotFound")
	}
}

// Под гонкой (go test -race): параллельные обновления не теряются.
func TestFileStoreConcurrentUpdates(t *testing.T) {
	s, _ := NewFileStore(filepath.Join(t.TempDir(), "apps.json"))
	s.Create(ctx, newApp("a1", "c", domain.StatusDraft, time.Now()))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Update(ctx, "a1", func(a *domain.Application) error { a.Amount++; return nil })
			s.List(ctx, Filter{})
		}()
	}
	wg.Wait()
	got, _ := s.Get(ctx, "a1")
	if got.Amount != 1_000_050 {
		t.Fatalf("lost updates: %d", got.Amount)
	}
}

func TestUserStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := NewUserFileStore(path)
	u := &domain.User{ID: "u1", Phone: "79000000001", PasswordHash: "pbkdf2$1$00$00", Balance: 100}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateUser(ctx, &domain.User{ID: "u2", Phone: "79000000001"}); !errors.Is(err, domain.ErrUserExists) {
		t.Fatal("duplicate phone must be rejected")
	}
	if _, err := s.UpdateUser(ctx, "u1", func(u *domain.User) error { return u.Debit(500) }); !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatal("overdraft of balance must fail")
	}
	got, _ := s.FindByPhone(ctx, "79000000001")
	if got.Balance != 100 {
		t.Fatal("failed debit must not change balance")
	}
	s2, _ := NewUserFileStore(path)
	again, err := s2.GetUser(ctx, "u1")
	if err != nil || again.PasswordHash != "pbkdf2$1$00$00" {
		t.Fatal("password hash must persist to disk")
	}
	b, _ := json.Marshal(again)
	if strings.Contains(string(b), "pbkdf2") {
		t.Fatal("password hash must never be serialized to API JSON")
	}
	if s2.CountUsers(ctx) != 1 {
		t.Fatal("count")
	}
}
