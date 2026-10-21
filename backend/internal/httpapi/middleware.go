package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/obs"
)

// ---------- request context: request-id, логгер ----------

func newRID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validRID — принимаем входящий X-Request-ID от шлюза, но только безопасного вида (без инъекций в логи).
func validRID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// reqInfo — что узнали о запросе внутри обработчиков (клиент) — для access-лога снаружи.
type reqInfo struct{ client, route string }

const infoKey ctxKey = "reqinfo"

type respWriter struct {
	http.ResponseWriter
	code  int
	bytes int
	wrote bool
}

func (w *respWriter) WriteHeader(c int) {
	if !w.wrote {
		w.code, w.wrote = c, true
	}
	w.ResponseWriter.WriteHeader(c)
}

func (w *respWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.code, w.wrote = http.StatusOK, true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *respWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// captureRoute запоминает шаблон маршрута, который нашёл роутер (r.Pattern). Middleware выше по
// цепочке видят исходный запрос, а не его копию, поэтому шаблон передаётся через reqInfo.
func captureRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if info, ok := r.Context().Value(infoKey).(*reqInfo); ok {
			info.route = r.Pattern
		}
	})
}

// observe — корреляция (X-Request-ID), access-лог и метрики по каждому запросу.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if !validRID(rid) {
			rid = newRID()
		}
		w.Header().Set("X-Request-ID", rid)
		log := s.Log.With("rid", rid)
		info := &reqInfo{}
		ctx := context.WithValue(obs.WithLogger(obs.WithRequestID(r.Context(), rid), log), infoKey, info)
		r = r.WithContext(ctx)
		rw := &respWriter{ResponseWriter: w, code: 200}
		start := time.Now()
		next.ServeHTTP(rw, r)
		dur := time.Since(start)

		route := info.route
		if route == "" {
			route = "unmatched"
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/healthz" && r.URL.Path != "/readyz" {
			route = "static"
		}
		code := strconv.Itoa(rw.code)
		obs.Default.Inc("http_requests_total", "HTTP-запросы", "route", route, "code", code)
		obs.Default.Observe("http_request_duration_seconds", "Длительность HTTP-запросов", dur.Seconds(), "route", route)

		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}
		level := slog.LevelInfo
		switch {
		case rw.code >= 500:
			level = slog.LevelError
		case rw.code >= 400 && rw.code != http.StatusUnauthorized && rw.code != http.StatusNotModified:
			level = slog.LevelWarn
		case route == "static" || rw.code == http.StatusNotModified:
			level = slog.LevelDebug
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method), slog.String("route", route), slog.String("path", r.URL.Path),
			slog.Int("status", rw.code), slog.Int("bytes", rw.bytes), slog.Float64("dur_ms", math.Round(float64(dur.Microseconds()))/1000),
			slog.String("ip", clientIP(r)),
		}
		if info.client != "" {
			attrs = append(attrs, slog.String("client", info.client))
		}
		if v := r.Header.Get("Idempotency-Key"); v != "" {
			attrs = append(attrs, slog.Bool("idempotent", true))
		}
		log.LogAttrs(r.Context(), level, "http", attrs...)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				obs.L(r.Context()).Error("panic", "err", v, "path", r.URL.Path)
				obs.Default.Inc("http_panics_total", "Паники в обработчиках")
				writeErr(w, 500, "internal", "внутренняя ошибка", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// sameOrigin — защита от CSRF: изменяющие запросы принимаются только со своего origin
// (дополнительно к SameSite=Strict у cookie).
func sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || u.Host != r.Host {
					writeErr(w, 403, "forbidden", "запрос с чужого источника", "")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// deadline — у каждого API-запроса есть предел времени: зависшая БД или интеграция не держит
// соединение и горутину бесконечно (обработчики и драйвер БД уважают ctx).
func (s *Server) deadline(next http.Handler) http.Handler {
	d := s.RequestTimeout
	if d <= 0 {
		d = 10 * time.Second
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---------- лимит запросов ----------

// RateLimiter — token bucket на IP: rate токенов в секунду, ёмкость burst.
type RateLimiter struct {
	Rate  float64
	Burst float64
	mu    sync.Mutex
	b     map[string]*bucket
	last  time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{Rate: rate, Burst: burst, b: map[string]*bucket{}}
}

// Allow списывает токен; при отказе возвращает, через сколько появится следующий.
func (l *RateLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.last) > time.Minute { // чистим давно неактивные ключи
		for k, b := range l.b {
			if now.Sub(b.at) > 10*time.Minute {
				delete(l.b, k)
			}
		}
		l.last = now
	}
	b, ok := l.b[key]
	if !ok {
		b = &bucket{tokens: l.Burst, at: now}
		l.b[key] = b
	}
	b.tokens = math.Min(l.Burst, b.tokens+now.Sub(b.at).Seconds()*l.Rate)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.Rate * float64(time.Second))
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Limiter == nil || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		ok, wait := s.Limiter.Allow(clientIP(r), time.Now())
		if !ok {
			secs := int(math.Ceil(wait.Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(max(1, secs)))
			obs.Default.Inc("http_rate_limited_total", "Запросы, отклонённые лимитом")
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "слишком много запросов, повторите чуть позже", "")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- ETag / 304 для GET API ----------

type bufWriter struct {
	header http.Header
	code   int
	buf    bytes.Buffer
}

func (b *bufWriter) Header() http.Header         { return b.header }
func (b *bufWriter) WriteHeader(c int)           { b.code = c }
func (b *bufWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }

// etag — клиент каждые 2,5 с опрашивает статус заявки; если ничего не изменилось, сервер
// отвечает 304 без тела. Браузер сам шлёт If-None-Match и подставляет закэшированный ответ.
func etag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		bw := &bufWriter{header: w.Header().Clone(), code: 200}
		next.ServeHTTP(bw, r)
		for k, v := range bw.header {
			w.Header()[k] = v
		}
		if bw.code == 200 {
			sum := sha256.Sum256(bw.buf.Bytes())
			tag := `"` + hex.EncodeToString(sum[:12]) + `"`
			w.Header().Set("ETag", tag)
			if w.Header().Get("Cache-Control") == "" {
				w.Header().Set("Cache-Control", "private, no-cache")
			}
			if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatch(inm, tag) {
				w.Header().Del("Content-Length")
				w.Header().Del("Content-Type")
				w.WriteHeader(http.StatusNotModified)
				obs.Default.Inc("http_not_modified_total", "Ответы 304 (данные не изменились)")
				return
			}
		}
		w.WriteHeader(bw.code)
		_, _ = w.Write(bw.buf.Bytes())
	})
}

func etagMatch(header, tag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "W/"))
		if t == tag || t == "*" {
			return true
		}
	}
	return false
}

// ---------- gzip ----------

type gzWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
	on      bool
}

var gzPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return w }}

func compressible(ct string) bool {
	for _, p := range []string{"application/json", "text/", "application/javascript", "image/svg+xml"} {
		if strings.HasPrefix(ct, p) {
			return true
		}
	}
	return false
}

func (g *gzWriter) decide(code int) {
	if g.decided {
		return
	}
	g.decided = true
	h := g.Header()
	if code == http.StatusNoContent || code == http.StatusNotModified || h.Get("Content-Encoding") != "" || !compressible(h.Get("Content-Type")) {
		return
	}
	if n, err := strconv.Atoi(h.Get("Content-Length")); err == nil && n < 1024 {
		return
	}
	g.on = true
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	if et := h.Get("ETag"); et != "" && !strings.HasPrefix(et, "W/") {
		h.Set("ETag", "W/"+et) // тело другое — ETag становится слабым
	}
	g.gz = gzPool.Get().(*gzip.Writer)
	g.gz.Reset(g.ResponseWriter)
}

func (g *gzWriter) WriteHeader(code int) {
	g.decide(code)
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.on {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzWriter) close() {
	if g.on {
		_ = g.gz.Close()
		gzPool.Put(g.gz)
	}
}

func compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		g := &gzWriter{ResponseWriter: w}
		defer g.close()
		next.ServeHTTP(g, r)
	})
}

// ---------- идемпотентность ----------

type captureWriter struct {
	http.ResponseWriter
	code int
	buf  bytes.Buffer
}

func (c *captureWriter) WriteHeader(code int) { c.code = code; c.ResponseWriter.WriteHeader(code) }
func (c *captureWriter) Write(b []byte) (int, error) {
	if c.code == 0 {
		c.code = 200
	}
	c.buf.Write(b)
	return c.ResponseWriter.Write(b)
}

// idempotent — Idempotency-Key для изменяющих запросов клиента (оплата, подписание, подача…).
// Повтор с тем же ключом возвращает сохранённый ответ (Idempotent-Replayed: true) и не выполняет
// действие второй раз; тот же ключ с другим телом — 422; ещё выполняется — 409.
func (s *Server) idempotent(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || s.Idem == nil {
			h(w, r)
			return
		}
		if len(key) > 128 {
			writeErr(w, 400, "bad_request", "Idempotency-Key длиннее 128 символов", "Idempotency-Key")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil {
			writeErr(w, 400, "bad_request", "не удалось прочитать тело запроса", "")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\n"), body...))
		hash := hex.EncodeToString(sum[:])
		scope := clientID(r)
		rec, owned, err := s.Idem.Begin(r.Context(), scope, key, hash)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if !owned {
			switch {
			case rec.RequestHash != hash:
				writeErr(w, 422, "idempotency_key_reused", "этот Idempotency-Key уже использован для другого запроса", "Idempotency-Key")
			case rec.Status == 0:
				w.Header().Set("Retry-After", "1")
				writeErr(w, 409, "request_in_progress", "запрос с этим ключом ещё выполняется", "")
			default:
				obs.Default.Inc("http_idempotent_replays_total", "Повторы запросов, отвеченные из сохранённого результата")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(rec.Status)
				_, _ = w.Write(rec.Body)
			}
			return
		}
		cw := &captureWriter{ResponseWriter: w}
		h(cw, r)
		// отдельный контекст: запись результата не должна сорваться из-за отмены запроса клиентом
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
		defer cancel()
		if cw.code >= 500 || cw.code == 0 {
			_ = s.Idem.Release(ctx, scope, key)
			return
		}
		if err := s.Idem.Complete(ctx, scope, key, cw.code, cw.buf.Bytes()); err != nil {
			obs.L(r.Context()).Error("idempotency complete failed", "err", err)
		}
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
