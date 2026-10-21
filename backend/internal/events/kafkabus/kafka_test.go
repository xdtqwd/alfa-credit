package kafkabus

// Интеграционные тесты на Kafka-протоколе. Запуск:
//   TEST_KAFKA_BROKERS=localhost:9092 go test ./internal/events/kafkabus/
// (в docker compose — брокер apache/kafka; в CI подойдёт и kfake из franz-go). Без переменной — пропуск.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/events"
)

func newBus(t *testing.T) *Bus {
	t.Helper()
	brokers := os.Getenv("TEST_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("TEST_KAFKA_BROKERS не задан — интеграционные тесты Kafka пропущены")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := New(ctx, Config{Brokers: strings.Split(brokers, ","), Partitions: 3, MaxAttempts: 3, Backoff: 10 * time.Millisecond,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

func uid() string { b := make([]byte, 5); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func TestPublishConsumeOrderAndFanOut(t *testing.T) {
	b := newBus(t)
	run := uid()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var mu sync.Mutex
	got := map[string]map[string][]string{} // группа → ключ → порядок id
	var wg sync.WaitGroup
	wg.Add(2 * 3 * 5)
	sub := func(group string) {
		got[group] = map[string][]string{}
		go func() {
			_ = b.Subscribe(ctx, events.TopicApplications, group, func(_ context.Context, e events.Event) error {
				if !strings.HasPrefix(e.Key, run) {
					return nil // события других прогонов
				}
				mu.Lock()
				got[group][e.Key] = append(got[group][e.Key], e.ID)
				mu.Unlock()
				wg.Done()
				return nil
			})
		}()
	}
	sub("decision-" + run)
	sub("notify-" + run)

	for i := 0; i < 5; i++ {
		for _, k := range []string{"a", "b", "c"} {
			e := events.Event{ID: k + string(rune('0'+i)), Type: events.TypeStatusChanged, Key: run + k, At: time.Now(),
				Headers: map[string]string{"request_id": "rid"}, Payload: []byte(`{}`), Topic: events.TopicApplications}
			if err := b.Publish(ctx, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	waitOrFail(t, &wg, 25*time.Second)
	mu.Lock()
	defer mu.Unlock()
	for g, keys := range got {
		for k, ids := range keys {
			for i, id := range ids {
				if id[1] != byte('0'+i) {
					t.Fatalf("group %s key %s: порядок нарушен %v", g, k, ids)
				}
			}
		}
	}
}

func TestPoisonMessageGoesToDLQ(t *testing.T) {
	b := newBus(t)
	run := uid()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var attempts int
	var mu sync.Mutex
	go func() {
		_ = b.Subscribe(ctx, events.TopicApplications, "flaky-"+run, func(_ context.Context, e events.Event) error {
			if e.Key != run {
				return nil
			}
			mu.Lock()
			attempts++
			mu.Unlock()
			return errors.New("downstream is broken")
		})
	}()
	dlq := make(chan events.Event, 1)
	go func() {
		_ = b.Subscribe(ctx, events.TopicDLQ, "ops-"+run, func(_ context.Context, e events.Event) error {
			if e.Key == run {
				select {
				case dlq <- e:
				default:
				}
			}
			return nil
		})
	}()
	if err := b.Publish(ctx, events.Event{ID: "poison", Type: events.TypeStatusChanged, Key: run, At: time.Now(), Payload: []byte(`{}`), Topic: events.TopicApplications}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-dlq:
		if e.ID != "poison" {
			t.Fatalf("dlq: %+v", e)
		}
	case <-ctx.Done():
		t.Fatal("сообщение, которое не удаётся обработать, должно уйти в DLQ")
	}
	mu.Lock()
	defer mu.Unlock()
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func waitOrFail(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("не все события доставлены")
	}
}
