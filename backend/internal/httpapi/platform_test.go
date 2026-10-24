package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/idempotency"
	"github.com/xdtqwd/alfa-credit/internal/obs"
)

// issued — выданный кредит у клиента (минуя конвейер).
func (h *harness) issued(c *http.Client) string {
	h.t.Helper()
	_, b := h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "working-capital", "amount": 300_000, "term_months": 3}, nil)
	var a domain.Application
	_ = json.Unmarshal(b, &a)
	_, _ = h.repo.Update(h.t.Context(), a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusApproved} {
			_ = x.Transition(s, "test", "", time.Now())
		}
		d := domain.Decision{Type: domain.DecisionApproved, Final: true}
		d.SetOffer(domain.NewQuote(300_000, 21.5, 3))
		x.Decision = &d
		return nil
	})
	h.do(c, "POST", "/api/v1/applications/"+a.ID+"/accept", nil, nil)
	h.do(c, "POST", "/api/v1/applications/"+a.ID+"/sign", map[string]bool{"confirm": true}, nil)
	return a.ID
}

func withIdem(s *Server) { s.Idem = idempotency.NewMemory(time.Hour) }

func TestIdempotencyKeyPreventsDoublePayment(t *testing.T) {
	h := newHarness(t, withIdem)
	c := h.login("79000000001")
	id := h.issued(c)
	u, _ := h.users.FindByPhone(t.Context(), "79000000001")
	before := u.Balance

	key := map[string]string{"Idempotency-Key": "pay-2026-10-21-1"}
	r1, b1 := h.do(c, "POST", "/api/v1/applications/"+id+"/pay", nil, key)
	r2, b2 := h.do(c, "POST", "/api/v1/applications/"+id+"/pay", nil, key) // повтор: обрыв сети, двойной клик
	if r1.StatusCode != 200 || r2.StatusCode != 200 {
		t.Fatalf("statuses %d %d: %s", r1.StatusCode, r2.StatusCode, b2)
	}
	if r2.Header.Get("Idempotent-Replayed") != "true" || !bytes.Equal(b1, b2) {
		t.Fatal("повтор должен вернуть сохранённый ответ")
	}
	got, _ := h.repo.Get(t.Context(), id)
	after, _ := h.users.GetUser(t.Context(), u.ID)
	if len(got.Payments) != 1 || before-after.Balance != got.Payments[0].Amount {
		t.Fatalf("платёж должен пройти ровно один раз: payments=%d debited=%d", len(got.Payments), before-after.Balance)
	}
	// тот же ключ с другим запросом — ошибка, а не чужой ответ
	if r, b := h.do(c, "POST", "/api/v1/applications/"+id+"/cancel", nil, key); r.StatusCode != 422 || !strings.Contains(string(b), "idempotency_key_reused") {
		t.Fatalf("reused key: %d %s", r.StatusCode, b)
	}
	// новый ключ — новый платёж
	if r, _ := h.do(c, "POST", "/api/v1/applications/"+id+"/pay", nil, map[string]string{"Idempotency-Key": "pay-2"}); r.StatusCode != 200 {
		t.Fatalf("second payment: %d", r.StatusCode)
	}
	if got, _ := h.repo.Get(t.Context(), id); len(got.Payments) != 2 {
		t.Fatalf("payments = %d, want 2", len(got.Payments))
	}
}

func TestIdempotencyKeysAreScopedPerClient(t *testing.T) {
	h := newHarness(t, withIdem)
	a, b := h.login("79000000001"), h.login("79000000002")
	k := map[string]string{"Idempotency-Key": "same"}
	r1, b1 := h.do(a, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev"}, k)
	r2, b2 := h.do(b, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev"}, k)
	if r1.StatusCode != 201 || r2.StatusCode != 201 || bytes.Equal(b1, b2) || r2.Header.Get("Idempotent-Replayed") != "" {
		t.Fatal("ключ одного клиента не должен отдавать ответ другому")
	}
}

func TestETagNotModified(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev"}, nil)
	r1, _ := h.do(c, "GET", "/api/v1/applications", nil, nil)
	tag := r1.Header.Get("ETag")
	if tag == "" || r1.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("etag=%q cache-control=%q", tag, r1.Header.Get("Cache-Control"))
	}
	r2, b2 := h.do(c, "GET", "/api/v1/applications", nil, map[string]string{"If-None-Match": tag})
	if r2.StatusCode != 304 || len(b2) != 0 {
		t.Fatalf("unchanged data must give 304 without body, got %d (%d bytes)", r2.StatusCode, len(b2))
	}
	h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "overdraft"}, nil)
	if r3, _ := h.do(c, "GET", "/api/v1/applications", nil, map[string]string{"If-None-Match": tag}); r3.StatusCode != 200 {
		t.Fatalf("changed data must give 200, got %d", r3.StatusCode)
	}
	// справочники кэшируются публично
	if r, _ := h.do(c, "GET", "/api/v1/products", nil, nil); r.Header.Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("products cache-control: %q", r.Header.Get("Cache-Control"))
	}
}

func TestPaginationAndFilters(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	for i := 0; i < 5; i++ {
		h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev"}, nil)
	}
	r, b := h.do(c, "GET", "/api/v1/applications?limit=2&offset=0", nil, nil)
	var page []domain.Application
	_ = json.Unmarshal(b, &page)
	if r.StatusCode != 200 || len(page) != 2 || r.Header.Get("X-Total-Count") != "5" || !strings.Contains(r.Header.Get("Link"), `rel="next"`) {
		t.Fatalf("page: %d len=%d total=%s link=%s", r.StatusCode, len(page), r.Header.Get("X-Total-Count"), r.Header.Get("Link"))
	}
	_, b = h.do(c, "GET", "/api/v1/applications?limit=2&offset=4", nil, nil)
	_ = json.Unmarshal(b, &page)
	if len(page) != 1 {
		t.Fatalf("last page len = %d", len(page))
	}
	if _, b := h.do(c, "GET", "/api/v1/applications?status=issued", nil, nil); string(bytes.TrimSpace(b)) != "[]" {
		t.Fatalf("status filter: %s", b)
	}
	for _, q := range []string{"limit=0", "limit=1000", "offset=-1", "status=bogus", "limit=x"} {
		if r, b := h.do(c, "GET", "/api/v1/applications?"+q, nil, nil); r.StatusCode != 422 || !strings.Contains(string(b), "validation") {
			t.Fatalf("%s: %d %s", q, r.StatusCode, b)
		}
	}
	// кабинет банка: поиск и страницы
	bank := map[string]string{"X-Bank-Token": "bank-secret"}
	r, b = h.do(h.client(), "GET", "/api/v1/bank/applications?q=КР-000003", nil, bank)
	var rows []map[string]any
	_ = json.Unmarshal(b, &rows)
	if r.StatusCode != 200 || len(rows) != 1 || rows[0]["number"] != "КР-000003" {
		t.Fatalf("bank search: %d %s", r.StatusCode, b)
	}
}

func TestRateLimit(t *testing.T) {
	h := newHarness(t, func(s *Server) { s.Limiter = NewRateLimiter(1, 3) })
	c := h.client()
	codes := map[int]int{}
	var last *http.Response
	for i := 0; i < 6; i++ {
		r, _ := h.do(c, "GET", "/api/v1/products", nil, nil)
		codes[r.StatusCode]++
		last = r
	}
	if codes[200] != 3 || codes[429] != 3 || last.Header.Get("Retry-After") == "" {
		t.Fatalf("codes=%v retry-after=%q", codes, last.Header.Get("Retry-After"))
	}
	// статика лимитом не режется
	if r, _ := h.do(c, "GET", "/", nil, nil); r.StatusCode != 200 {
		t.Fatalf("static: %d", r.StatusCode)
	}
}

func TestRateLimiterRefills(t *testing.T) {
	l := NewRateLimiter(10, 1)
	now := time.Now()
	if ok, _ := l.Allow("ip", now); !ok {
		t.Fatal("first")
	}
	if ok, wait := l.Allow("ip", now); ok || wait <= 0 {
		t.Fatal("bucket must be empty")
	}
	if ok, _ := l.Allow("ip", now.Add(150*time.Millisecond)); !ok {
		t.Fatal("token must refill (10/s)")
	}
}

func TestAPIErrorsAreJSONWithRequestID(t *testing.T) {
	h := newHarness(t)
	c := h.client()
	r, b := h.do(c, "GET", "/api/v1/nope", nil, nil)
	var e struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	_ = json.Unmarshal(b, &e)
	if r.StatusCode != 404 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || e.Error.RequestID != r.Header.Get("X-Request-ID") || e.Error.RequestID == "" {
		t.Fatalf("404: %d %s rid=%s", r.StatusCode, b, r.Header.Get("X-Request-ID"))
	}
	r, b = h.do(c, "DELETE", "/api/v1/products", nil, nil)
	if r.StatusCode != 405 || r.Header.Get("Allow") != "GET" || !strings.Contains(string(b), "method_not_allowed") {
		t.Fatalf("405: %d allow=%q %s", r.StatusCode, r.Header.Get("Allow"), b)
	}
	// входящий X-Request-ID от шлюза сохраняется, мусорный — заменяется
	r, _ = h.do(c, "GET", "/healthz", nil, map[string]string{"X-Request-ID": "gw-123"})
	if r.Header.Get("X-Request-ID") != "gw-123" {
		t.Fatal("valid incoming request id must be kept")
	}
	r, _ = h.do(c, "GET", "/healthz", nil, map[string]string{"X-Request-ID": "bad id <script>"})
	if strings.ContainsAny(r.Header.Get("X-Request-ID"), " <>") || r.Header.Get("X-Request-ID") == "" {
		t.Fatal("unsafe request id must be replaced")
	}
}

func TestGzipAndStaticETag(t *testing.T) {
	big := strings.Repeat("console.log('кредит');\n", 400)
	h := newHarness(t, func(s *Server) {
		s.Static = fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}, "app.js": {Data: []byte(big)}}
	})
	tr := &http.Transport{DisableCompression: true}
	c := &http.Client{Transport: tr}
	req, _ := http.NewRequest("GET", h.ts.URL+"/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	r, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	if r.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("static js must be gzipped, headers %v", r.Header)
	}
	zr, _ := gzip.NewReader(r.Body)
	plain, _ := io.ReadAll(zr)
	if string(plain) != big {
		t.Fatal("gzip body mismatch")
	}
	tag := r.Header.Get("ETag")
	req2, _ := http.NewRequest("GET", h.ts.URL+"/app.js", nil)
	req2.Header.Set("If-None-Match", strings.TrimPrefix(tag, "W/"))
	r2, _ := c.Do(req2)
	if r2.StatusCode != 304 {
		t.Fatalf("static revalidation must give 304, got %d (etag %s)", r2.StatusCode, tag)
	}
}

func TestReadyzReportsDependencies(t *testing.T) {
	h := newHarness(t, func(s *Server) {
		s.Ready = map[string]func(ctx context.Context) error{"db-replica": func(context.Context) error { return errors.New("broker down") }}
	})
	r, b := h.do(h.client(), "GET", "/readyz", nil, nil)
	if r.StatusCode != 503 || !strings.Contains(string(b), "broker down") || !strings.Contains(string(b), `"storage":"ok"`) {
		t.Fatalf("readyz: %d %s", r.StatusCode, b)
	}
}

// Регрессия: метрики и access-лог должны получать шаблон маршрута, а не «unmatched»
// (middleware выше роутера копирует запрос через WithContext, и r.Pattern снаружи не виден).
func TestMetricsCarryRoutePattern(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	before := obs.Default.CounterValue("http_requests_total", "route", "GET /api/v1/me", "code", "200")
	if r, _ := h.do(c, "GET", "/api/v1/me", nil, nil); r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	if got := obs.Default.CounterValue("http_requests_total", "route", "GET /api/v1/me", "code", "200"); got != before+1 {
		t.Fatalf("route label not recorded: before=%d after=%d", before, got)
	}
}

// Kafka для API — некритичная зависимость: при её отказе /readyz отвечает 200 «degraded»
// (балансировщик не снимает API, заявки копятся в outbox), а отказ хранилища — 503.
func TestReadyzDegradesOnOptionalDependency(t *testing.T) {
	h := newHarness(t, func(s *Server) {
		s.Degradable = map[string]func(context.Context) error{"kafka": func(context.Context) error { return errors.New("broker down") }}
	})
	r, b := h.do(h.client(), "GET", "/readyz", nil, nil)
	if r.StatusCode != 200 || !bytes.Contains(b, []byte(`"degraded"`)) || !bytes.Contains(b, []byte("broker down")) {
		t.Fatalf("optional dependency failure must give 200 degraded, got %d %s", r.StatusCode, b)
	}
	h2 := newHarness(t, func(s *Server) {
		s.Ready = map[string]func(context.Context) error{"db": func(context.Context) error { return errors.New("db down") }}
	})
	if r, _ := h2.do(h2.client(), "GET", "/readyz", nil, nil); r.StatusCode != 503 {
		t.Fatalf("critical dependency failure must give 503, got %d", r.StatusCode)
	}
}

// COOP отдаётся только там, где браузер его применяет: HTTPS (в т.ч. за шлюзом) и localhost.
// На http://<ip|домен> Chrome игнорирует заголовок и пишет ошибку в консоль (так было при открытии
// через host.docker.internal).
func TestCOOPOnlyOnTrustworthyOrigins(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, c := range []struct {
		host, proto string
		want        bool
	}{
		{"localhost:8080", "", true},
		{"127.0.0.1:8080", "", true},
		{"[::1]:8080", "", true},
		{"app.localhost", "", true},
		{"host.docker.internal:8080", "", false},
		{"192.168.1.20:8080", "", false},
		{"credit.example.ru", "https", true},
	} {
		req, _ := http.NewRequest("GET", "/", nil)
		req.Host = c.host
		if c.proto != "" {
			req.Header.Set("X-Forwarded-Proto", c.proto)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Cross-Origin-Opener-Policy") != ""; got != c.want {
			t.Errorf("host %s proto %q: COOP=%v, want %v", c.host, c.proto, got, c.want)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("host %s: остальные заголовки безопасности должны отдаваться всегда", c.host)
		}
	}
}
