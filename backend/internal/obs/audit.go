package obs

import (
	"context"
	"log/slog"
	"time"
)

// AuditEntry — запись журнала аудита: кто, что, над чем и когда сделал.
// Банковское требование: значимые действия (вход, подача, подписание, оплата) неизменяемо журналируются.
type AuditEntry struct {
	At        time.Time      `json:"at"`
	Actor     string         `json:"actor"`  // id клиента, "bank" или "system"
	Action    string         `json:"action"` // auth.login, application.submit, loan.pay …
	Entity    string         `json:"entity"` // user | application
	EntityID  string         `json:"entity_id"`
	Outcome   string         `json:"outcome"` // ok | denied | error
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}

// AuditSink — долговременное хранилище аудита (таблица audit_log в PostgreSQL).
type AuditSink interface {
	WriteAudit(ctx context.Context, e AuditEntry) error
}

// Auditor пишет запись в лог (component=audit) и, если задан, в долговременный Sink.
// Ошибка записи в Sink не ломает бизнес-операцию, но логируется как error и считается в метриках.
type Auditor struct {
	Log  *slog.Logger
	Sink AuditSink
	Now  func() time.Time
}

func (a *Auditor) Record(ctx context.Context, e AuditEntry) {
	if a == nil {
		return
	}
	if e.At.IsZero() {
		if a.Now != nil {
			e.At = a.Now()
		} else {
			e.At = time.Now()
		}
	}
	if e.Actor == "" {
		e.Actor = Actor(ctx)
	}
	if e.RequestID == "" {
		e.RequestID = RequestID(ctx)
	}
	if e.Outcome == "" {
		e.Outcome = "ok"
	}
	if a.Log != nil {
		attrs := []any{"component", "audit", "action", e.Action, "actor", e.Actor, "entity", e.Entity, "entity_id", e.EntityID, "outcome", e.Outcome, "rid", e.RequestID}
		if len(e.Details) > 0 {
			attrs = append(attrs, "details", e.Details)
		}
		a.Log.Info("audit", attrs...)
	}
	Default.Inc("audit_events_total", "Записи журнала аудита", "action", e.Action, "outcome", e.Outcome)
	if a.Sink != nil {
		if err := a.Sink.WriteAudit(ctx, e); err != nil {
			Default.Inc("audit_sink_errors_total", "Ошибки записи аудита в БД")
			if a.Log != nil {
				a.Log.Error("audit sink failed", "err", err, "action", e.Action)
			}
		}
	}
}
