// Package kafkabus — шина событий на Apache Kafka (клиент franz-go, BSD-3).
//
// Гарантии:
//   - продюсер идемпотентный (без дублей при ретраях), acks=all — запись подтверждают все ISR-реплики;
//   - ключ сообщения — id заявки: события одной заявки строго упорядочены внутри партиции;
//   - consumer group с ручным коммитом ПОСЛЕ обработки → at-least-once; обработчики идемпотентны (inbox);
//   - партиции обрабатываются параллельно; ошибка → повторы с backoff → DLQ-топик, offset сдвигается,
//     «ядовитое» сообщение не блокирует партицию.
package kafkabus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/obs"
)

type Config struct {
	Brokers     []string
	ClientID    string
	Partitions  int32
	Replication int16
	MaxAttempts int
	Backoff     time.Duration
	Log         *slog.Logger
}

type Bus struct {
	cfg  Config
	prod *kgo.Client
}

func New(ctx context.Context, cfg Config) (*Bus, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka: no brokers")
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "credit-api"
	}
	if cfg.Partitions <= 0 {
		cfg.Partitions = 6
	}
	if cfg.Replication <= 0 {
		cfg.Replication = 1
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = 200 * time.Millisecond
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	prod, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.RecordRetries(10),
		kgo.ProduceRequestTimeout(10*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}
	b := &Bus{cfg: cfg, prod: prod}
	if err := b.EnsureTopics(ctx); err != nil {
		prod.Close()
		return nil, err
	}
	return b, nil
}

// EnsureTopics создаёт топики, если их нет (идемпотентно).
func (b *Bus) EnsureTopics(ctx context.Context) error {
	req := kmsg.NewPtrCreateTopicsRequest()
	req.TimeoutMillis = 10_000
	for _, name := range []string{events.TopicApplications, events.TopicDLQ} {
		t := kmsg.NewCreateTopicsRequestTopic()
		t.Topic = name
		t.NumPartitions = b.cfg.Partitions
		t.ReplicationFactor = b.cfg.Replication
		for k, v := range map[string]string{"retention.ms": "604800000", "cleanup.policy": "delete"} {
			c := kmsg.NewCreateTopicsRequestTopicConfig()
			c.Name, c.Value = k, kmsg.StringPtr(v)
			t.Configs = append(t.Configs, c)
		}
		req.Topics = append(req.Topics, t)
	}
	var last error
	for attempt := 0; attempt < 30; attempt++ { // брокер может ещё подниматься
		resp, err := req.RequestWith(ctx, b.prod)
		if err == nil {
			for _, t := range resp.Topics {
				if e := kerr.ErrorForCode(t.ErrorCode); e != nil && !errors.Is(e, kerr.TopicAlreadyExists) {
					return fmt.Errorf("create topic %s: %w", t.Topic, e)
				}
			}
			return b.waitLeaders(ctx, events.TopicApplications, events.TopicDLQ)
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("kafka not reachable: %w", last)
}

// waitLeaders ждёт, пока у всех партиций топиков появится лидер. Сразу после создания топика брокер
// ещё выбирает лидеров: запись в этот момент получает повторяемые ошибки и, если выборы затянулись
// (холодный брокер в CI), исчерпывает повторы продюсера.
func (b *Bus) waitLeaders(ctx context.Context, topics ...string) error {
	req := kmsg.NewPtrMetadataRequest()
	for _, name := range topics {
		t := kmsg.NewMetadataRequestTopic()
		t.Topic = kmsg.StringPtr(name)
		req.Topics = append(req.Topics, t)
	}
	var last error
	for attempt := 0; attempt < 100; attempt++ {
		resp, err := req.RequestWith(ctx, b.prod)
		last = err
		if err == nil {
			last = topicsReady(resp)
			if last == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("kafka topics not ready: %w", last)
}

// topicsReady — у каждого топика есть партиции, и у каждой партиции выбран лидер.
func topicsReady(resp *kmsg.MetadataResponse) error {
	for _, t := range resp.Topics {
		name := ""
		if t.Topic != nil {
			name = *t.Topic
		}
		if e := kerr.ErrorForCode(t.ErrorCode); e != nil {
			return fmt.Errorf("topic %s: %w", name, e)
		}
		if len(t.Partitions) == 0 {
			return fmt.Errorf("topic %s: no partitions yet", name)
		}
		for _, p := range t.Partitions {
			if e := kerr.ErrorForCode(p.ErrorCode); e != nil || p.Leader < 0 {
				return fmt.Errorf("topic %s partition %d: leader not elected (%v)", name, p.Partition, e)
			}
		}
	}
	return nil
}

func (b *Bus) Ping(ctx context.Context) error { return b.prod.Ping(ctx) }

func toRecord(e events.Event) (*kgo.Record, error) {
	topic := e.Topic
	if topic == "" {
		topic = events.TopicApplications
	}
	val, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	r := &kgo.Record{Topic: topic, Key: []byte(e.Key), Value: val, Timestamp: e.At}
	r.Headers = append(r.Headers, kgo.RecordHeader{Key: "type", Value: []byte(e.Type)}, kgo.RecordHeader{Key: "id", Value: []byte(e.ID)})
	for k, v := range e.Headers {
		r.Headers = append(r.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	return r, nil
}

// Publish синхронно отправляет события и ждёт подтверждения брокера (нужно outbox-relay,
// чтобы пометить запись отправленной только после успешной доставки).
func (b *Bus) Publish(ctx context.Context, evs ...events.Event) error {
	recs := make([]*kgo.Record, 0, len(evs))
	for _, e := range evs {
		r, err := toRecord(e)
		if err != nil {
			return err
		}
		recs = append(recs, r)
	}
	if err := b.prod.ProduceSync(ctx, recs...).FirstErr(); err != nil {
		obs.Default.Inc("events_publish_errors_total", "Ошибки публикации в Kafka")
		return err
	}
	for _, e := range evs {
		obs.Default.Inc("events_published_total", "Опубликованные события", "topic", e.Topic, "type", e.Type)
	}
	return nil
}

// Subscribe запускает consumer group и блокирует до отмены ctx.
func (b *Bus) Subscribe(ctx context.Context, topic, group string, h events.Handler) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(b.cfg.Brokers...),
		kgo.ClientID(b.cfg.ClientID+"-"+group),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topic),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.FetchMaxWait(500*time.Millisecond),
		kgo.BlockRebalanceOnPoll(), // ребаланс не отнимет партицию посреди обработки
	)
	if err != nil {
		return err
	}
	defer cl.Close()
	log := b.cfg.Log.With("group", group, "topic", topic)
	log.Info("kafka consumer started")
	for {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			cl.AllowRebalance()
			return nil
		}
		fetches.EachError(func(t string, p int32, err error) {
			log.Warn("kafka fetch error", "partition", p, "err", err)
		})
		var wg sync.WaitGroup
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for _, r := range p.Records {
					b.process(ctx, log, group, r, h)
				}
			}()
		})
		wg.Wait()
		if err := cl.CommitUncommittedOffsets(ctx); err != nil && ctx.Err() == nil {
			log.Warn("kafka commit failed", "err", err)
		}
		cl.AllowRebalance()
	}
}

func (b *Bus) process(ctx context.Context, log *slog.Logger, group string, r *kgo.Record, h events.Handler) {
	var e events.Event
	if err := json.Unmarshal(r.Value, &e); err != nil {
		log.Error("kafka: undecodable message → DLQ", "offset", r.Offset, "err", err)
		b.toDLQ(ctx, log, group, r, err)
		return
	}
	e.Topic, e.Partition, e.Offset = r.Topic, r.Partition, r.Offset
	for e.Attempt = 1; e.Attempt <= b.cfg.MaxAttempts; e.Attempt++ {
		err := h(ctx, e)
		if err == nil {
			obs.Default.Inc("events_consumed_total", "Обработанные события", "group", group, "type", e.Type, "result", "ok")
			return
		}
		if ctx.Err() != nil {
			return
		}
		log.Warn("event handler failed", "type", e.Type, "key", e.Key, "attempt", e.Attempt, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(b.cfg.Backoff << (e.Attempt - 1)):
		}
	}
	obs.Default.Inc("events_consumed_total", "Обработанные события", "group", group, "type", e.Type, "result", "dlq")
	b.toDLQ(ctx, log, group, r, fmt.Errorf("handler failed %d times", b.cfg.MaxAttempts))
}

func (b *Bus) toDLQ(ctx context.Context, log *slog.Logger, group string, r *kgo.Record, cause error) {
	d := &kgo.Record{Topic: events.TopicDLQ, Key: r.Key, Value: r.Value, Headers: append(append([]kgo.RecordHeader{}, r.Headers...),
		kgo.RecordHeader{Key: "dlq_group", Value: []byte(group)},
		kgo.RecordHeader{Key: "dlq_error", Value: []byte(cause.Error())},
		kgo.RecordHeader{Key: "dlq_source", Value: []byte(fmt.Sprintf("%s/%d/%d", r.Topic, r.Partition, r.Offset))})}
	if err := b.prod.ProduceSync(ctx, d).FirstErr(); err != nil {
		log.Error("kafka: DLQ publish failed", "err", err)
	}
}

// Close дожидается отправки буфера продюсера и закрывает соединения.
func (b *Bus) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = b.prod.Flush(ctx)
	b.prod.Close()
}
