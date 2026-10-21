package storage

import (
	"context"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/obs"
)

// Publishing — декоратор репозитория для файлового режима: после успешного изменения публикует
// доменные события напрямую в шину. В PostgreSQL-режиме вместо него работает transactional outbox
// (событие пишется в той же транзакции, что и изменение, — без риска «изменили, но не сообщили»).
type Publishing struct {
	Repository
	Bus events.Publisher
}

func (p *Publishing) Create(ctx context.Context, a *domain.Application) (*domain.Application, error) {
	out, err := p.Repository.Create(ctx, a)
	if err == nil {
		p.publish(ctx, nil, out)
	}
	return out, err
}

func (p *Publishing) Update(ctx context.Context, id string, fn func(a *domain.Application) error) (*domain.Application, error) {
	var before *domain.Application
	out, err := p.Repository.Update(ctx, id, func(a *domain.Application) error {
		before = clone(a)
		return fn(a)
	})
	if err == nil {
		p.publish(ctx, before, out)
	}
	return out, err
}

func (p *Publishing) publish(ctx context.Context, before, after *domain.Application) {
	if evs := events.FromChange(before, after, obs.RequestID(ctx)); len(evs) > 0 {
		if err := p.Bus.Publish(ctx, evs...); err != nil {
			obs.L(ctx).Error("publish events failed", "err", err, "app", after.Number)
		}
	}
}
