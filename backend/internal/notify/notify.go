// Package notify — сервис уведомлений клиента (отдельная consumer group в Kafka).
// Показывает fan-out: одно событие «решение принято» независимо обрабатывают конвейер,
// уведомления, а в банке — ещё и CRM, аналитика, АБС. Отправка push/SMS симулируется записью в лог.
package notify

import (
	"context"
	"log/slog"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/obs"
)

const Consumer = "credit-notifications"

type Inbox interface {
	MarkProcessed(ctx context.Context, consumer, eventID string) (bool, error)
}

type Service struct {
	Log   *slog.Logger
	Inbox Inbox
}

var texts = map[domain.Status]string{
	domain.StatusApproved:     "Кредит одобрен — примите условия и подпишите договор в кабинете",
	domain.StatusCounterOffer: "Банк предлагает другие условия — посмотрите в кабинете",
	domain.StatusNeedInfo:     "Нужно уточнить данные анкеты",
	domain.StatusRejected:     "Решение по заявке готово — подробности в кабинете",
	domain.StatusIssued:       "Деньги зачислены на расчётный счёт",
	domain.StatusClosed:       "Кредит полностью погашен",
}

func (s *Service) Handle(ctx context.Context, e events.Event) error {
	var text, channel string
	p, err := events.DecodeApplication(e)
	if err != nil {
		return nil
	}
	switch e.Type {
	case events.TypeStatusChanged:
		text, channel = texts[domain.Status(p.To)], "push"
	case events.TypePaymentMade:
		text, channel = "Платёж по кредиту получен", "push"
	}
	if text == "" {
		return nil
	}
	if s.Inbox != nil {
		fresh, err := s.Inbox.MarkProcessed(ctx, Consumer, e.ID)
		if err != nil {
			return err
		}
		if !fresh {
			return nil
		}
	}
	s.Log.Info("notification sent (simulated)", "component", "notify", "channel", channel, "client", p.ClientID,
		"app", p.Number, "event", e.Type, "text", text, "rid", e.Headers["request_id"])
	obs.Default.Inc("notifications_sent_total", "Уведомления клиентам (симуляция)", "channel", channel)
	return nil
}
