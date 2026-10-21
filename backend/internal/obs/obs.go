// Package obs — наблюдаемость: структурные логи с корреляцией по request-id, маскирование ПДн,
// метрики в формате Prometheus и аудит действий. Только стандартная библиотека.
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger — JSON (по умолчанию, для сбора в ELK/Loki) или text (для локальной отладки).
func NewLogger(w io.Writer, level, format string) *slog.Logger {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(strings.ToUpper(strings.TrimSpace(level)))); err != nil {
		lv = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lv}
	if w == nil {
		w = os.Stdout
	}
	if strings.EqualFold(format, "text") {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

type ctxKey int

const (
	ridKey ctxKey = iota
	logKey
	actorKey
)

// WithRequestID кладёт идентификатор запроса (X-Request-ID) — он же уходит в заголовки событий Kafka,
// поэтому путь заявки прослеживается от HTTP-запроса до решения конвейера.
func WithRequestID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, ridKey, rid)
}

func RequestID(ctx context.Context) string {
	s, _ := ctx.Value(ridKey).(string)
	return s
}

// WithLogger / L — логгер запроса с уже проставленными rid и клиентом.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, logKey, l)
}

func L(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(logKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// WithActor / Actor — кто выполняет действие (id клиента, "bank", "system").
func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}

func Actor(ctx context.Context) string {
	if s, ok := ctx.Value(actorKey).(string); ok {
		return s
	}
	return "anonymous"
}

// MaskPhone: 79000000001 → 7900***0001. В логах и аудите телефон не хранится целиком.
func MaskPhone(p string) string {
	d := make([]rune, 0, len(p))
	for _, r := range p {
		if r >= '0' && r <= '9' {
			d = append(d, r)
		}
	}
	if len(d) < 8 {
		return "***"
	}
	return string(d[:4]) + "***" + string(d[len(d)-4:])
}

// MaskAccount: 40702810900000003724 → ••3724.
func MaskAccount(a string) string {
	if len(a) <= 4 {
		return "••"
	}
	return "••" + a[len(a)-4:]
}
