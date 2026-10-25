// Package cache — потокобезопасный in-process кэш: LRU с ограничением размера, TTL на запись,
// отрицательное кэширование и защита от «штормов» (singleflight: на один ключ — один загрузчик).
//
// Кэш на экземпляр сервиса. Для общего кэша между репликами в целевой архитектуре — Redis;
// интерфейс использования (GetOrLoad) при этом не меняется.
package cache

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type entry[K comparable, V any] struct {
	key     K
	val     V
	err     error // закэшированная «ожидаемая» ошибка (например, компания не найдена)
	expires time.Time
}

type call[V any] struct {
	wg  sync.WaitGroup
	val V
	err error
}

// Cache — LRU+TTL кэш. Нулевое значение не используется — создавайте через New.
type Cache[K comparable, V any] struct {
	Name string

	mu        sync.Mutex
	ll        *list.List
	items     map[K]*list.Element
	inflight  map[K]*call[V]
	max       int
	ttl       time.Duration
	negTTL    time.Duration
	cacheable func(error) bool
	now       func() time.Time

	hits, misses, evictions, loads atomic.Int64
}

// Options — настройки кэша. NegativeTTL > 0 включает кэширование ошибок, для которых
// Cacheable(err) == true (например, «компания не найдена» — повторный запрос даст то же).
type Options struct {
	MaxEntries  int
	TTL         time.Duration
	NegativeTTL time.Duration
	Cacheable   func(error) bool
	Now         func() time.Time
}

func New[K comparable, V any](name string, o Options) *Cache[K, V] {
	if o.MaxEntries <= 0 {
		o.MaxEntries = 1000
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	c := &Cache[K, V]{Name: name, ll: list.New(), items: map[K]*list.Element{}, inflight: map[K]*call[V]{},
		max: o.MaxEntries, ttl: o.TTL, negTTL: o.NegativeTTL, cacheable: o.Cacheable, now: o.Now}
	register(c)
	return c
}

// Get возвращает значение, если оно есть и не протухло.
func (c *Cache[K, V]) Get(k K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, err, ok := c.getLocked(k)
	if ok && err == nil {
		c.hits.Add(1)
		return v, true
	}
	c.misses.Add(1)
	var zero V
	return zero, false
}

func (c *Cache[K, V]) getLocked(k K) (V, error, bool) {
	var zero V
	el, ok := c.items[k]
	if !ok {
		return zero, nil, false
	}
	e := el.Value.(*entry[K, V])
	if !e.expires.IsZero() && c.now().After(e.expires) {
		c.removeLocked(el)
		return zero, nil, false
	}
	c.ll.MoveToFront(el)
	return e.val, e.err, true
}

// Set кладёт значение с TTL кэша (ttl == 0 — бессрочно до вытеснения).
func (c *Cache[K, V]) Set(k K, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setLocked(k, v, nil, c.ttl)
}

func (c *Cache[K, V]) setLocked(k K, v V, err error, ttl time.Duration) {
	var exp time.Time
	if ttl > 0 {
		exp = c.now().Add(ttl)
	}
	if el, ok := c.items[k]; ok {
		e := el.Value.(*entry[K, V])
		e.val, e.err, e.expires = v, err, exp
		c.ll.MoveToFront(el)
		return
	}
	c.items[k] = c.ll.PushFront(&entry[K, V]{key: k, val: v, err: err, expires: exp})
	for c.ll.Len() > c.max {
		c.removeLocked(c.ll.Back())
		c.evictions.Add(1)
	}
}

func (c *Cache[K, V]) removeLocked(el *list.Element) {
	c.ll.Remove(el)
	delete(c.items, el.Value.(*entry[K, V]).key)
}

// Delete удаляет ключ (инвалидация после изменения данных).
func (c *Cache[K, V]) Delete(k K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[k]; ok {
		c.removeLocked(el)
	}
}

// Len — число записей (включая ещё не вычищенные протухшие).
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// GetOrLoad возвращает значение из кэша или загружает его. Параллельные запросы одного ключа
// ждут единственный вызов load (singleflight), поэтому холодный кэш не «бьёт» внешний сервис.
func (c *Cache[K, V]) GetOrLoad(ctx context.Context, k K, load func(ctx context.Context) (V, error)) (V, error) {
	c.mu.Lock()
	if v, err, ok := c.getLocked(k); ok {
		c.mu.Unlock()
		c.hits.Add(1)
		return v, err
	}
	c.misses.Add(1)
	if cl, ok := c.inflight[k]; ok {
		c.mu.Unlock()
		cl.wg.Wait()
		return cl.val, cl.err
	}
	cl := &call[V]{}
	cl.wg.Add(1)
	c.inflight[k] = cl
	c.mu.Unlock()

	c.loads.Add(1)
	cl.val, cl.err = load(ctx)

	c.mu.Lock()
	delete(c.inflight, k)
	switch {
	case cl.err == nil:
		c.setLocked(k, cl.val, nil, c.ttl)
	case c.negTTL > 0 && c.cacheable != nil && c.cacheable(cl.err):
		var zero V
		c.setLocked(k, zero, cl.err, c.negTTL)
	}
	c.mu.Unlock()
	cl.wg.Done()
	return cl.val, cl.err
}

// Stats — счётчики для метрик.
type Stats struct {
	Name                           string
	Hits, Misses, Evictions, Loads int64
	Size                           int
}

func (c *Cache[K, V]) Stats() Stats {
	return Stats{Name: c.Name, Hits: c.hits.Load(), Misses: c.misses.Load(), Evictions: c.evictions.Load(), Loads: c.loads.Load(), Size: c.Len()}
}

// ---------- реестр кэшей для /metrics ----------

type statser interface{ Stats() Stats }

var (
	regMu sync.Mutex
	reg   = map[string]statser{}
)

func register(s statser) {
	regMu.Lock()
	defer regMu.Unlock()
	reg[s.Stats().Name] = s
}

// All возвращает статистику всех созданных кэшей.
func All() []Stats {
	regMu.Lock()
	defer regMu.Unlock()
	out := make([]Stats, 0, len(reg))
	for _, s := range reg {
		out = append(out, s.Stats())
	}
	return out
}
