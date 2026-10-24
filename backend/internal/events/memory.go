package events

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/obs"
)

// Memory — in-process шина с семантикой Kafka для локального запуска и тестов:
// топики, consumer groups, «партиции» по ключу (порядок внутри ключа, параллелизм между ключами),
// повторы с экспоненциальной задержкой и DLQ. Доставка at-least-once в пределах процесса.
type Memory struct {
	Partitions  int
	MaxAttempts int
	Backoff     time.Duration
	Log         *slog.Logger

	mu     sync.Mutex
	groups map[string][]*memGroup // topic → группы
}

type memGroup struct {
	name  string
	parts []chan Event
	h     Handler
}

func NewMemory(log *slog.Logger) *Memory {
	return &Memory{Partitions: 4, MaxAttempts: 5, Backoff: 100 * time.Millisecond, Log: log, groups: map[string][]*memGroup{}}
}

func partitionOf(key string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(n))
}

func (m *Memory) Publish(_ context.Context, evs ...Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range evs {
		if e.Topic == "" {
			e.Topic = TopicApplications
		}
		for _, g := range m.groups[e.Topic] {
			p := partitionOf(e.Key, len(g.parts))
			e.Partition = int32(p)
			select {
			case g.parts[p] <- e:
			default:
				// очередь переполнена — не блокируем продюсера; outbox/sweeper доведут заявку
				obs.Default.Inc("events_dropped_total", "События, не поместившиеся в очередь in-memory шины", "group", g.name)
			}
		}
		obs.Default.Inc("events_published_total", "Опубликованные события", "topic", e.Topic, "type", e.Type)
	}
	return nil
}

// Subscribe регистрирует группу и обрабатывает события до отмены ctx.
func (m *Memory) Subscribe(ctx context.Context, topic, group string, h Handler) error {
	g := &memGroup{name: group, h: h, parts: make([]chan Event, max(1, m.Partitions))}
	for i := range g.parts {
		g.parts[i] = make(chan Event, 1024)
	}
	m.mu.Lock()
	m.groups[topic] = append(m.groups[topic], g)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for i := range g.parts {
		wg.Add(1)
		go func(ch chan Event) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case e := <-ch:
					m.handle(ctx, g, e)
				}
			}
		}(g.parts[i])
	}
	<-ctx.Done()
	wg.Wait()
	return nil
}

func (m *Memory) handle(ctx context.Context, g *memGroup, e Event) {
	attempts := max(1, m.MaxAttempts)
	for e.Attempt = 1; e.Attempt <= attempts; e.Attempt++ {
		err := g.h(ctx, e)
		if err == nil {
			obs.Default.Inc("events_consumed_total", "Обработанные события", "group", g.name, "type", e.Type, "result", "ok")
			return
		}
		if ctx.Err() != nil {
			return
		}
		if m.Log != nil {
			m.Log.Warn("event handler failed", "group", g.name, "type", e.Type, "key", e.Key, "attempt", e.Attempt, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.Backoff << (e.Attempt - 1)):
		}
	}
	obs.Default.Inc("events_consumed_total", "Обработанные события", "group", g.name, "type", e.Type, "result", "dlq")
	dlq := e
	dlq.Topic = TopicDLQ
	dlq.Headers = map[string]string{"original_topic": e.Topic, "group": g.name}
	_ = m.Publish(ctx, dlq)
}
