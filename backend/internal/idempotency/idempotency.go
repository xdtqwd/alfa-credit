// Package idempotency — ключи идемпотентности для изменяющих запросов (заголовок Idempotency-Key),
// как в платёжных API банков: повтор запроса с тем же ключом не выполняет действие второй раз,
// а возвращает сохранённый ответ. Тот же ключ с другим запросом — ошибка 422.
package idempotency

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

// Record — сохранённый результат. Status == 0 — запрос с этим ключом ещё выполняется.
type Record struct {
	RequestHash string
	Status      int
	Body        []byte
}

type Store interface {
	// Begin резервирует ключ. owned=true — ключ новый, запрос нужно выполнить и вызвать Complete/Release.
	// owned=false — ключ уже был: вернётся сохранённая запись.
	Begin(ctx context.Context, scope, key, requestHash string) (rec Record, owned bool, err error)
	Complete(ctx context.Context, scope, key string, status int, body []byte) error
	// Release снимает резерв (ошибка сервера — клиент может повторить с тем же ключом).
	Release(ctx context.Context, scope, key string) error
}

// ---------- PostgreSQL ----------

type PG struct{ DB *sql.DB }

func (p PG) Begin(ctx context.Context, scope, key, h string) (Record, bool, error) {
	res, err := p.DB.ExecContext(ctx, `INSERT INTO idempotency_keys (scope, key, request_hash) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, scope, key, h)
	if err != nil {
		return Record{}, false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return Record{RequestHash: h}, true, nil
	}
	var r Record
	var status sql.NullInt64
	err = p.DB.QueryRowContext(ctx, `SELECT request_hash, status_code, response FROM idempotency_keys WHERE scope=$1 AND key=$2`, scope, key).
		Scan(&r.RequestHash, &status, &r.Body)
	if errors.Is(err, sql.ErrNoRows) { // запись успели освободить — пробуем снова
		return p.Begin(ctx, scope, key, h)
	}
	r.Status = int(status.Int64)
	return r, false, err
}

func (p PG) Complete(ctx context.Context, scope, key string, status int, body []byte) error {
	_, err := p.DB.ExecContext(ctx, `UPDATE idempotency_keys SET status_code=$3, response=$4 WHERE scope=$1 AND key=$2`, scope, key, status, body)
	return err
}

func (p PG) Release(ctx context.Context, scope, key string) error {
	_, err := p.DB.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE scope=$1 AND key=$2 AND status_code IS NULL`, scope, key)
	return err
}

// ---------- in-memory (локальный режим) ----------

type Memory struct {
	TTL time.Duration
	mu  sync.Mutex
	m   map[string]memRec
}

type memRec struct {
	Record
	at time.Time
}

func NewMemory(ttl time.Duration) *Memory { return &Memory{TTL: ttl, m: map[string]memRec{}} }

func (m *Memory) Begin(_ context.Context, scope, key, h string) (Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, v := range m.m { // ленивая очистка
		if now.Sub(v.at) > m.TTL {
			delete(m.m, k)
		}
	}
	k := scope + "\x00" + key
	if r, ok := m.m[k]; ok {
		return r.Record, false, nil
	}
	m.m[k] = memRec{Record: Record{RequestHash: h}, at: now}
	return Record{RequestHash: h}, true, nil
}

func (m *Memory) Complete(_ context.Context, scope, key string, status int, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := scope + "\x00" + key
	r := m.m[k]
	r.Status, r.Body = status, body
	m.m[k] = r
	return nil
}

func (m *Memory) Release(_ context.Context, scope, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := scope + "\x00" + key
	if r, ok := m.m[k]; ok && r.Status == 0 {
		delete(m.m, k)
	}
	return nil
}
