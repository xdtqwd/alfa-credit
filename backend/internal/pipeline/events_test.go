package pipeline

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

// Конвейер работает только на событиях (без опроса хранилища): отправка анкеты →
// status_changed → шаг → новый status_changed → … → решение.
func TestEventDrivenPipelineReachesDecision(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	bus := events.NewMemory(log)
	base, _ := storage.NewFileStore("")
	repo := &storage.Publishing{Repository: base, Bus: bus}
	w := &Worker{Repo: repo, Registry: integrations.MockRegistry{}, Bureau: integrations.MockBureau{},
		StepDelay: 20 * time.Millisecond, Log: log, Now: time.Now}
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = bus.Subscribe(c, events.TopicApplications, Consumer, w.Handle) }()
	time.Sleep(20 * time.Millisecond)

	co, _ := integrations.MockRegistry{}.Lookup(c, "7701234567")
	now := time.Now()
	a, _ := repo.Create(c, &domain.Application{ID: "ev-1", ClientID: "c", ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24,
		INN: co.INN, Company: co, Financials: domain.Financials{AnnualRevenue: co.RevenueLastYear}, Consent: true,
		Level: 2, Identity: testIdentity, Full: func() *domain.FullForm { f := level2Form(co); return &f }(),
		Status: domain.StatusFullForm, CreatedAt: now, StatusChangedAt: now})
	if _, err := repo.Update(c, a.ID, func(x *domain.Application) error {
		return x.Transition(domain.StatusSubmitted, "client", "", time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := repo.Get(c, a.ID)
		if got.Status == domain.StatusApproved {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := repo.Get(c, a.ID)
	t.Fatalf("event-driven pipeline stuck at %s", got.Status)
}

// Повторная доставка того же события (at-least-once) не продвигает заявку дважды.
func TestDuplicateEventIsHarmless(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	w := &Worker{Repo: repo, Registry: integrations.MockRegistry{}, Bureau: integrations.MockBureau{},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: time.Now}
	a := submitted(t, repo, "7701234567", 3_000_000, 84_000_000, 0)
	e := events.FromChange(&domain.Application{ID: a.ID, Status: domain.StatusFullForm}, a, "")[0]
	if err := w.Handle(ctx, e); err != nil {
		t.Fatal(err)
	}
	after1, _ := repo.Get(ctx, a.ID)
	if err := w.Handle(ctx, e); err != nil {
		t.Fatal(err)
	}
	after2, _ := repo.Get(ctx, a.ID)
	if after1.Status != domain.StatusDataCheck || after2.Status != domain.StatusDataCheck || len(after2.History) != len(after1.History) {
		t.Fatalf("duplicate moved application: %s → %s", after1.Status, after2.Status)
	}
}
