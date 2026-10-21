package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTTLAndLRU(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := New[string, int]("t-ttl", Options{MaxEntries: 2, TTL: time.Minute, Now: func() time.Time { return now }})
	c.Set("a", 1)
	c.Set("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatal("a must be cached")
	}
	c.Set("c", 3) // вытесняет наименее недавно использованный — b
	if _, ok := c.Get("b"); ok {
		t.Fatal("b must be evicted (LRU)")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a was used recently and must stay")
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expired entry must not be returned")
	}
	if s := c.Stats(); s.Evictions != 1 || s.Hits != 2 {
		t.Fatalf("stats: %+v", s)
	}
}

func TestSingleflightLoadsOnce(t *testing.T) {
	c := New[string, int]("t-sf", Options{TTL: time.Minute})
	var calls atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.GetOrLoad(context.Background(), "k", func(context.Context) (int, error) {
				calls.Add(1)
				<-release
				return 42, nil
			})
			if err != nil || v != 42 {
				t.Errorf("got %d %v", v, err)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("loader called %d times, want 1 (защита от шторма запросов)", calls.Load())
	}
}

var errNotFound = errors.New("not found")

func TestNegativeCaching(t *testing.T) {
	c := New[string, int]("t-neg", Options{TTL: time.Minute, NegativeTTL: time.Minute, Cacheable: func(err error) bool { return errors.Is(err, errNotFound) }})
	var calls int
	load := func(context.Context) (int, error) { calls++; return 0, errNotFound }
	for i := 0; i < 3; i++ {
		if _, err := c.GetOrLoad(context.Background(), "x", load); !errors.Is(err, errNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("«не найдено» должно кэшироваться: вызовов %d", calls)
	}
	// прочие ошибки не кэшируются — следующий запрос снова идёт во внешний сервис
	other := errors.New("timeout")
	n := 0
	for i := 0; i < 2; i++ {
		_, _ = c.GetOrLoad(context.Background(), "y", func(context.Context) (int, error) { n++; return 0, other })
	}
	if n != 2 {
		t.Fatalf("transient errors must not be cached: %d", n)
	}
}

func TestDeleteInvalidates(t *testing.T) {
	c := New[string, string]("t-del", Options{TTL: time.Minute})
	c.Set("s", "u1")
	c.Delete("s")
	if _, ok := c.Get("s"); ok {
		t.Fatal("deleted key must be gone")
	}
}
