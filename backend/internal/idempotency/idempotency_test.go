package idempotency

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStore(t *testing.T) {
	m := NewMemory(time.Hour)
	ctx := context.Background()
	if _, owned, _ := m.Begin(ctx, "c", "k", "h"); !owned {
		t.Fatal("new key")
	}
	if r, owned, _ := m.Begin(ctx, "c", "k", "h"); owned || r.Status != 0 {
		t.Fatal("in progress")
	}
	_ = m.Complete(ctx, "c", "k", 201, []byte("x"))
	if r, _, _ := m.Begin(ctx, "c", "k", "h"); r.Status != 201 || string(r.Body) != "x" {
		t.Fatalf("replay %+v", r)
	}
	_ = m.Release(ctx, "c", "k") // завершённый ключ не освобождается
	if _, owned, _ := m.Begin(ctx, "c", "k", "h"); owned {
		t.Fatal("completed key must stay")
	}
	_, _, _ = m.Begin(ctx, "c", "k2", "h")
	_ = m.Release(ctx, "c", "k2")
	if _, owned, _ := m.Begin(ctx, "c", "k2", "h"); !owned {
		t.Fatal("released key is free again")
	}
	m.TTL = 0
	if _, owned, _ := m.Begin(ctx, "c", "k", "h"); !owned {
		t.Fatal("expired keys are cleaned")
	}
}
