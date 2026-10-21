package integrations

import (
	"context"
	"errors"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/cache"
	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/obs"
)

// CachedRegistry кэширует ответы ЕГРЮЛ/ФНС: данные реестра меняются редко, а запросы к нему
// идут на каждом вводе ИНН и в конвейере. «Не найдено» тоже кэшируется (короче), чтобы перебор
// несуществующих ИНН не бил во внешний сервис.
type CachedRegistry struct {
	Next  Registry
	cache *cache.Cache[string, *domain.Company]
}

func NewCachedRegistry(next Registry, ttl time.Duration) *CachedRegistry {
	return &CachedRegistry{Next: next, cache: cache.New[string, *domain.Company]("registry", cache.Options{
		MaxEntries: 50_000, TTL: ttl, NegativeTTL: 5 * time.Minute,
		Cacheable: func(err error) bool { return errors.Is(err, domain.ErrCompanyNotFound) },
	})}
}

func (c *CachedRegistry) Lookup(ctx context.Context, inn string) (*domain.Company, error) {
	co, err := c.cache.GetOrLoad(ctx, inn, func(ctx context.Context) (*domain.Company, error) {
		start := time.Now()
		co, err := c.Next.Lookup(ctx, inn)
		obs.Default.Observe("integration_request_seconds", "Время ответа внешних сервисов", time.Since(start).Seconds(), "service", "registry")
		return co, err
	})
	if err != nil || co == nil {
		return nil, err
	}
	cp := *co // вызывающий может менять объект — кэш не должен портиться
	return &cp, nil
}

// CachedBureau кэширует кредитный отчёт БКИ: запрос в бюро платный и медленный, а повторная
// подача той же компании в течение TTL не требует нового отчёта.
type CachedBureau struct {
	Next  CreditBureau
	cache *cache.Cache[string, *domain.BureauReport]
}

func NewCachedBureau(next CreditBureau, ttl time.Duration) *CachedBureau {
	return &CachedBureau{Next: next, cache: cache.New[string, *domain.BureauReport]("bureau", cache.Options{MaxEntries: 50_000, TTL: ttl})}
}

func (c *CachedBureau) Report(ctx context.Context, inn string) (*domain.BureauReport, error) {
	r, err := c.cache.GetOrLoad(ctx, inn, func(ctx context.Context) (*domain.BureauReport, error) {
		start := time.Now()
		r, err := c.Next.Report(ctx, inn)
		obs.Default.Observe("integration_request_seconds", "Время ответа внешних сервисов", time.Since(start).Seconds(), "service", "bureau")
		return r, err
	})
	if err != nil || r == nil {
		return nil, err
	}
	cp := *r
	return &cp, nil
}
