// Package events — доменные события кредитного процесса и шина доставки.
//
// Поток: изменение заявки → событие пишется в outbox в той же транзакции БД (transactional outbox) →
// relay публикует его в Kafka → потребители (конвейер решения, уведомления) обрабатывают его
// идемпотентно (inbox). Без Kafka (локальный режим) используется in-memory шина с теми же контрактами.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

// Топики. Ключ сообщения — id заявки: все события одной заявки попадают в одну партицию
// и обрабатываются строго по порядку, а разные заявки — параллельно.
const (
	TopicApplications = "credit.applications.v1"
	TopicDLQ          = "credit.applications.dlq.v1"
)

// Типы событий.
const (
	TypeCreated       = "application.created"
	TypeStatusChanged = "application.status_changed"
	TypeLoanIssued    = "loan.issued"
	TypePaymentMade   = "loan.payment_received"
)

// Event — конверт события (по мотивам CloudEvents).
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Key       string            `json:"key"`
	Source    string            `json:"source"`
	At        time.Time         `json:"at"`
	Headers   map[string]string `json:"headers,omitempty"` // request_id и т.п. — корреляция с HTTP
	Payload   json.RawMessage   `json:"payload"`
	Attempt   int               `json:"-"`
	Topic     string            `json:"-"`
	Partition int32             `json:"-"`
	Offset    int64             `json:"-"`
}

// ApplicationPayload — полезная нагрузка событий заявки. Персональных данных нет:
// только идентификаторы, статусы и суммы (минимизация ПДн в шине).
type ApplicationPayload struct {
	ApplicationID string `json:"application_id"`
	Number        string `json:"number"`
	ClientID      string `json:"client_id"`
	ProductID     string `json:"product_id"`
	Amount        int64  `json:"amount"`
	TermMonths    int    `json:"term_months"`
	From          string `json:"from,omitempty"`
	To            string `json:"to"`
	Score         *int   `json:"score,omitempty"`
	Decision      string `json:"decision,omitempty"`
	// Preliminary — решение предварительное (по быстрой анкете), дальше клиент заполняет полную анкету.
	Preliminary   bool      `json:"preliminary,omitempty"`
	PaymentN      int       `json:"payment_n,omitempty"`
	PaymentAmount int64     `json:"payment_amount,omitempty"`
	StatusAt      time.Time `json:"status_at"`
}

// Publisher — отправка событий (Kafka-продюсер или in-memory шина).
type Publisher interface {
	Publish(ctx context.Context, evs ...Event) error
}

// Handler обрабатывает событие. Ошибка → повтор; после исчерпания попыток — DLQ.
type Handler func(ctx context.Context, e Event) error

// Subscriber — consumer group: каждое событие топика обрабатывается одним экземпляром группы.
type Subscriber interface {
	Subscribe(ctx context.Context, topic, group string, h Handler) error // блокирует до отмены ctx
}

func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// FromChange вычисляет события по разнице состояний заявки «до» и «после» изменения.
// before == nil — заявка создана. Используется хранилищем внутри транзакции (outbox).
func FromChange(before, after *domain.Application, rid string) []Event {
	if after == nil {
		return nil
	}
	base := ApplicationPayload{ApplicationID: after.ID, Number: after.Number, ClientID: after.ClientID,
		ProductID: after.ProductID, Amount: after.Amount, TermMonths: after.TermMonths,
		To: string(after.Status), Score: after.Score, StatusAt: after.StatusChangedAt}
	if after.Decision != nil {
		base.Decision = string(after.Decision.Type)
		base.Preliminary = !after.Decision.Final
	}
	var out []Event
	mk := func(typ string, p ApplicationPayload, at time.Time) {
		b, _ := json.Marshal(p)
		h := map[string]string{}
		if rid != "" {
			h["request_id"] = rid
		}
		out = append(out, Event{ID: NewID(), Type: typ, Key: after.ID, Source: "credit-api", At: at, Headers: h, Payload: b, Topic: TopicApplications})
	}
	if before == nil {
		mk(TypeCreated, base, after.CreatedAt)
		return out
	}
	if before.Status != after.Status {
		p := base
		p.From = string(before.Status)
		mk(TypeStatusChanged, p, after.StatusChangedAt)
		if after.Status == domain.StatusIssued {
			mk(TypeLoanIssued, base, after.StatusChangedAt)
		}
	}
	for i := len(before.Payments); i < len(after.Payments); i++ {
		p := base
		p.PaymentN, p.PaymentAmount = after.Payments[i].N, after.Payments[i].Amount
		mk(TypePaymentMade, p, after.Payments[i].At)
	}
	return out
}

// DecodeApplication достаёт полезную нагрузку события заявки.
func DecodeApplication(e Event) (ApplicationPayload, error) {
	var p ApplicationPayload
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}
