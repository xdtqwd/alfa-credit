package pgstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/auth"
	"github.com/xdtqwd/alfa-credit/internal/cache"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/obs"
)

// ---------- outbox ----------

func (s *Store) writeOutbox(ctx context.Context, evs []events.Event) error {
	for _, e := range evs {
		env, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := s.q(ctx).ExecContext(ctx, `INSERT INTO outbox (event_id, topic, key, type, envelope) VALUES ($1,$2,$3,$4,$5)`,
			e.ID, e.Topic, e.Key, e.Type, env); err != nil {
			return err
		}
	}
	return nil
}

// RelayOutbox забирает пачку неотправленных событий (FOR UPDATE SKIP LOCKED — несколько реплик
// разбирают outbox без пересечений), публикует и помечает отправленными. При ошибке брокера
// транзакция откатывается целиком — события остаются в outbox и уйдут при следующей попытке.
// Возвращает число отправленных событий.
func (s *Store) RelayOutbox(ctx context.Context, pub events.Publisher, batch int) (int, error) {
	n := 0
	err := s.InTx(ctx, func(ctx context.Context) error {
		ids, evs, err := s.outboxBatch(ctx, batch)
		if err != nil || len(evs) == 0 {
			return err
		}
		if err := pub.Publish(ctx, evs...); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := s.q(ctx).ExecContext(ctx, `UPDATE outbox SET published_at=now(), attempts=attempts+1 WHERE id=$1`, id); err != nil {
				return err
			}
		}
		n = len(evs)
		return nil
	})
	if err != nil {
		// отметка попытки пишется и при остановке сервиса (ctx отменён), но не дольше 3 с —
		// недоступная БД не должна подвешивать цикл доставки
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, _ = s.DB.ExecContext(c, `UPDATE outbox SET attempts=attempts+1, last_error=$1
			WHERE id IN (SELECT id FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT $2)`, truncate(err.Error(), 500), batch)
	}
	return n, err
}

// outboxBatch читает пачку неотправленных событий под блокировкой строк. Курсор закрывается до
// возврата: дальше в той же транзакции идут UPDATE, а драйвер не выполняет их при открытом курсоре.
func (s *Store) outboxBatch(ctx context.Context, batch int) ([]int64, []events.Event, error) {
	rows, err := s.q(ctx).QueryContext(ctx, `SELECT id, envelope FROM outbox WHERE published_at IS NULL ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`, batch)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids []int64
	var evs []events.Event
	for rows.Next() {
		var id int64
		var env []byte
		if err := rows.Scan(&id, &env); err != nil {
			return nil, nil, err
		}
		var e events.Event
		if err := json.Unmarshal(env, &e); err != nil {
			return nil, nil, err
		}
		if e.Topic == "" {
			e.Topic = events.TopicApplications
		}
		ids, evs = append(ids, id), append(evs, e)
	}
	return ids, evs, rows.Err()
}

// OutboxPending — сколько событий ждут отправки (метрика: растёт, если Kafka недоступна).
func (s *Store) OutboxPending(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n)
	return n, err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ---------- inbox (идемпотентные потребители) ----------

// MarkProcessed атомарно отмечает событие обработанным потребителем. false — уже было обработано
// (повторная доставка) и действие выполнять не нужно. Вызывайте внутри InTx вместе с самим действием.
func (s *Store) MarkProcessed(ctx context.Context, consumer, eventID string) (bool, error) {
	res, err := s.q(ctx).ExecContext(ctx, `INSERT INTO processed_events (consumer, event_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, consumer, eventID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ---------- аудит ----------

func (s *Store) WriteAudit(ctx context.Context, e obs.AuditEntry) error {
	var details any
	if len(e.Details) > 0 {
		b, _ := json.Marshal(e.Details)
		details = b
	}
	// аудит пишем вне бизнес-транзакции: отказ в действии (outcome=denied) тоже должен остаться в журнале
	_, err := s.DB.ExecContext(ctx, `INSERT INTO audit_log (at, actor, action, entity, entity_id, outcome, request_id, details) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.At, e.Actor, e.Action, e.Entity, e.EntityID, e.Outcome, e.RequestID, details)
	return err
}

// AuditFor — журнал по сущности (для кабинета банка и тестов).
func (s *Store) AuditFor(ctx context.Context, entity, id string) ([]obs.AuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT at, actor, action, entity, entity_id, outcome, coalesce(request_id,''), details FROM audit_log WHERE entity=$1 AND entity_id=$2 ORDER BY id`, entity, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []obs.AuditEntry
	for rows.Next() {
		var e obs.AuditEntry
		var d []byte
		if err := rows.Scan(&e.At, &e.Actor, &e.Action, &e.Entity, &e.EntityID, &e.Outcome, &e.RequestID, &d); err != nil {
			return nil, err
		}
		if len(d) > 0 {
			_ = json.Unmarshal(d, &e.Details)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------- сессии ----------

// Sessions — сессии в PostgreSQL с коротким кэшем на экземпляре (снимает запрос к БД с каждого
// HTTP-запроса; выход из системы сбрасывает кэш сразу, на других репликах — не позже TTL кэша).
type Sessions struct {
	s     *Store
	ttl   time.Duration
	cache *cache.Cache[string, string]
}

func (s *Store) Sessions(ttl time.Duration) *Sessions {
	return &Sessions{s: s, ttl: ttl, cache: cache.New[string, string]("sessions", cache.Options{MaxEntries: 10_000, TTL: 30 * time.Second})}
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (x *Sessions) Lifetime() time.Duration { return x.ttl }

func (x *Sessions) Create(ctx context.Context, userID string) (string, error) {
	tok := auth.NewToken()
	if _, err := x.s.DB.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		hashToken(tok), userID, time.Now().Add(x.ttl)); err != nil {
		return "", err
	}
	return tok, nil
}

func (x *Sessions) Get(ctx context.Context, token string) (string, bool) {
	if token == "" {
		return "", false
	}
	h := hashToken(token)
	uid, err := x.cache.GetOrLoad(ctx, h, func(ctx context.Context) (string, error) {
		var uid string
		err := x.s.DB.QueryRowContext(ctx, `SELECT user_id FROM sessions WHERE token_hash=$1 AND expires_at > now()`, h).Scan(&uid)
		return uid, err
	})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			obs.L(ctx).Error("session lookup failed", "err", err)
		}
		return "", false
	}
	return uid, true
}

func (x *Sessions) Delete(ctx context.Context, token string) {
	h := hashToken(token)
	x.cache.Delete(h)
	if _, err := x.s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1`, h); err != nil {
		obs.L(ctx).Error("session delete failed", "err", err)
	}
}

// ---------- обслуживание ----------

// Cleanup удаляет протухшие сессии, ключи идемпотентности старше суток и отправленные события outbox
// старше недели (история событий остаётся в Kafka).
func (s *Store) Cleanup(ctx context.Context) error {
	for _, q := range []string{
		`DELETE FROM sessions WHERE expires_at < now()`,
		`DELETE FROM idempotency_keys WHERE created_at < now() - interval '24 hours'`,
		`DELETE FROM outbox WHERE published_at < now() - interval '7 days'`,
		`DELETE FROM processed_events WHERE processed_at < now() - interval '7 days'`,
	} {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
