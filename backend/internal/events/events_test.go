package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

func TestFromChange(t *testing.T) {
	now := time.Now()
	a := &domain.Application{ID: "a1", Number: "КР-000001", ClientID: "c1", ProductID: "business-dev", Amount: 3_000_000, TermMonths: 24,
		Status: domain.StatusDraft, CreatedAt: now, StatusChangedAt: now}
	if evs := FromChange(nil, a, "rid-1"); len(evs) != 1 || evs[0].Type != TypeCreated || evs[0].Key != "a1" || evs[0].Headers["request_id"] != "rid-1" {
		t.Fatalf("create: %+v", evs)
	}
	b := *a
	b.Status = domain.StatusSubmitted
	evs := FromChange(a, &b, "")
	if len(evs) != 1 || evs[0].Type != TypeStatusChanged {
		t.Fatalf("status change: %+v", evs)
	}
	p, _ := DecodeApplication(evs[0])
	if p.From != "draft" || p.To != "submitted" || p.ApplicationID != "a1" {
		t.Fatalf("payload: %+v", p)
	}
	// изменение без смены статуса (автосохранение анкеты) событий не порождает
	c := b
	c.Purpose = "другое"
	if evs := FromChange(&b, &c, ""); len(evs) != 0 {
		t.Fatalf("no-op change produced events: %+v", evs)
	}
	// выдача → status_changed + loan.issued; платёж → loan.payment_received
	d := b
	d.Status = domain.StatusIssued
	if evs := FromChange(&b, &d, ""); len(evs) != 2 || evs[1].Type != TypeLoanIssued {
		t.Fatalf("issue: %+v", evs)
	}
	e := d
	e.Payments = []domain.Payment{{N: 1, Amount: 100, At: now}}
	if evs := FromChange(&d, &e, ""); len(evs) != 1 || evs[0].Type != TypePaymentMade {
		t.Fatalf("payment: %+v", evs)
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestMemoryBusOrderPerKeyAndGroups(t *testing.T) {
	bus := NewMemory(quiet())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	got := map[string][]string{} // группа → порядок событий ключа k1
	done := make(chan struct{}, 20)
	h := func(group string) Handler {
		return func(_ context.Context, e Event) error {
			mu.Lock()
			if e.Key == "k1" {
				got[group] = append(got[group], e.ID)
			}
			mu.Unlock()
			done <- struct{}{}
			return nil
		}
	}
	go func() { _ = bus.Subscribe(ctx, TopicApplications, "g1", h("g1")) }()
	go func() { _ = bus.Subscribe(ctx, TopicApplications, "g2", h("g2")) }()
	time.Sleep(20 * time.Millisecond)
	for _, id := range []string{"1", "2", "3", "4"} {
		_ = bus.Publish(ctx, Event{ID: id, Key: "k1", Type: TypeStatusChanged, Topic: TopicApplications})
	}
	for i := 0; i < 8; i++ { // 4 события × 2 группы — каждая группа получает всё (fan-out)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("events not delivered")
		}
	}
	for _, g := range []string{"g1", "g2"} {
		if s := got[g]; len(s) != 4 || s[0] != "1" || s[3] != "4" {
			t.Fatalf("group %s order: %v", g, s)
		}
	}
}

func TestMemoryBusRetriesThenDLQ(t *testing.T) {
	bus := NewMemory(quiet())
	bus.Backoff = time.Millisecond
	bus.MaxAttempts = 3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	attempts := 0
	dlq := make(chan Event, 1)
	go func() {
		_ = bus.Subscribe(ctx, TopicApplications, "flaky", func(context.Context, Event) error {
			mu.Lock()
			attempts++
			mu.Unlock()
			return errors.New("boom")
		})
	}()
	go func() {
		_ = bus.Subscribe(ctx, TopicDLQ, "ops", func(_ context.Context, e Event) error { dlq <- e; return nil })
	}()
	time.Sleep(20 * time.Millisecond)
	_ = bus.Publish(ctx, Event{ID: "x", Key: "k", Type: TypeStatusChanged, Topic: TopicApplications})
	select {
	case e := <-dlq:
		if e.ID != "x" || e.Headers["group"] != "flaky" {
			t.Fatalf("dlq event: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("poison event must land in DLQ")
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}
