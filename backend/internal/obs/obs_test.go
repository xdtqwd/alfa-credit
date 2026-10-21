package obs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestMaskPII(t *testing.T) {
	if got := MaskPhone("+7 (900) 000-00-01"); got != "7900***0001" {
		t.Fatalf("MaskPhone = %q", got)
	}
	if MaskPhone("12") != "***" || MaskAccount("40702810900000003724") != "••3724" {
		t.Fatal("masking")
	}
}

func TestMetricsTextFormat(t *testing.T) {
	r := NewRegistry()
	r.Inc("http_requests_total", "Запросы", "route", "GET /x", "code", "200")
	r.Inc("http_requests_total", "Запросы", "code", "200", "route", "GET /x") // порядок меток не важен
	r.Observe("dur_seconds", "Длительность", 0.03, "route", "a")
	r.Gauge("queue", "Очередь", func() []Sample { return []Sample{{Value: 7}} })
	var b bytes.Buffer
	r.WriteText(&b)
	out := b.String()
	for _, want := range []string{
		`# TYPE http_requests_total counter`, `http_requests_total{code="200",route="GET /x"} 2`,
		`dur_seconds_bucket{route="a",le="0.05"} 1`, `dur_seconds_bucket{route="a",le="0.025"} 0`, `dur_seconds_count{route="a"} 1`,
		"queue 7",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("metrics output lacks %q:\n%s", want, out)
		}
	}
	if r.CounterValue("http_requests_total", "route", "GET /x", "code", "200") != 2 {
		t.Fatal("CounterValue")
	}
}

type sink struct {
	got []AuditEntry
	err error
}

func (s *sink) WriteAudit(_ context.Context, e AuditEntry) error {
	s.got = append(s.got, e)
	return s.err
}

func TestAuditorFillsContextAndSurvivesSinkFailure(t *testing.T) {
	var logs bytes.Buffer
	s := &sink{}
	a := &Auditor{Log: slog.New(slog.NewJSONHandler(&logs, nil)), Sink: s}
	ctx := WithActor(WithRequestID(context.Background(), "rid-7"), "client-1")
	a.Record(ctx, AuditEntry{Action: "loan.pay", Entity: "application", EntityID: "a1"})
	if len(s.got) != 1 || s.got[0].Actor != "client-1" || s.got[0].RequestID != "rid-7" || s.got[0].Outcome != "ok" || s.got[0].At.IsZero() {
		t.Fatalf("entry: %+v", s.got)
	}
	if !strings.Contains(logs.String(), `"component":"audit"`) {
		t.Fatal("audit must be logged")
	}
	s.err = errors.New("db down")
	a.Record(ctx, AuditEntry{Action: "loan.pay"}) // не паникует и не ломает операцию
	var nilAuditor *Auditor
	nilAuditor.Record(ctx, AuditEntry{Action: "x"})
}

func TestLoggerLevelAndFormat(t *testing.T) {
	var b bytes.Buffer
	l := NewLogger(&b, "warn", "text")
	l.Info("hidden")
	l.Warn("shown")
	if strings.Contains(b.String(), "hidden") || !strings.Contains(b.String(), "shown") || strings.HasPrefix(b.String(), "{") {
		t.Fatalf("logger: %q", b.String())
	}
	ctx := WithLogger(context.Background(), l)
	if L(ctx) != l || L(context.Background()) == nil || Actor(context.Background()) != "anonymous" {
		t.Fatal("ctx helpers")
	}
}
