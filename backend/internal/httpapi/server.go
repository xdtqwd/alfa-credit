// Package httpapi — REST API (контракт: docs/openapi.yaml) и раздача фронтенда.
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type Server struct {
	Svc       *service.Service
	Repo      storage.Repository
	Log       *slog.Logger
	BankToken string
	Static    fs.FS
	Version   string
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": s.Version})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Repo.Ping(r.Context()); err != nil {
			writeJSON(w, 503, map[string]string{"status": "not ready"})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ready"})
	})

	mux.HandleFunc("GET /api/v1/products", s.products)
	mux.HandleFunc("POST /api/v1/calc", s.calc)
	mux.HandleFunc("GET /api/v1/companies/{inn}", s.client(s.company))

	mux.HandleFunc("POST /api/v1/applications", s.client(s.create))
	mux.HandleFunc("GET /api/v1/applications", s.client(s.list))
	mux.HandleFunc("GET /api/v1/applications/{id}", s.client(s.get))
	mux.HandleFunc("PATCH /api/v1/applications/{id}", s.client(s.patch))
	mux.HandleFunc("POST /api/v1/applications/{id}/submit", s.client(s.action(s.Svc.Submit)))
	mux.HandleFunc("POST /api/v1/applications/{id}/accept", s.client(s.action(s.Svc.Accept)))
	mux.HandleFunc("POST /api/v1/applications/{id}/cancel", s.client(s.action(s.Svc.Cancel)))
	mux.HandleFunc("POST /api/v1/applications/{id}/sign", s.client(s.sign))

	mux.HandleFunc("GET /api/v1/bank/applications", s.bank(s.bankList))

	mux.Handle("GET /", http.FileServerFS(s.Static))

	return s.recoverer(s.logging(securityHeaders(mux)))
}

// ---------- middleware ----------

type ctxKey string

const clientKey ctxKey = "client"

var clientIDRe = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// client — демо-аутентификация по X-Client-ID. В проде: OIDC (Альфа ID) + JWT на API Gateway.
func (s *Server) client(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Client-ID")
		if !clientIDRe.MatchString(id) {
			writeErr(w, 401, "unauthorized", "нужен заголовок X-Client-ID", "")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), clientKey, id)))
	}
}

func (s *Server) bank(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Bank-Token")), []byte(s.BankToken)) != 1 {
			writeErr(w, 401, "unauthorized", "неверный X-Bank-Token", "")
			return
		}
		h(w, r)
	}
}

func clientID(r *http.Request) string { return r.Context().Value(clientKey).(string) }

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			b := make([]byte, 6)
			_, _ = rand.Read(b)
			rid = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", rid)
		sw := &statusWriter{ResponseWriter: w, code: 200}
		start := time.Now()
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.Log.Info("http", "rid", rid, "method", r.Method, "path", r.URL.Path, "status", sw.code, "dur_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Log.Error("panic", "err", v, "path", r.URL.Path)
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
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}

// ---------- handlers ----------

func (s *Server) products(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, domain.Products())
}

func (s *Server) calc(w http.ResponseWriter, r *http.Request) {
	var in service.CreateInput
	if !decode(w, r, &in) {
		return
	}
	p, ok := domain.FindProduct(in.ProductID)
	if !ok {
		writeErr(w, 422, "validation", "неизвестный продукт", "product_id")
		return
	}
	res, err := domain.Calculate(p, in.Amount, in.TermMonths)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) company(w http.ResponseWriter, r *http.Request) {
	c, err := s.Svc.LookupCompany(r.Context(), r.PathValue("inn"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, c)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var in service.CreateInput
	if !decode(w, r, &in) {
		return
	}
	a, err := s.Svc.Create(r.Context(), clientID(r), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/applications/"+a.ID)
	writeJSON(w, 201, a)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Svc.List(r.Context(), clientID(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, apps)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	a, err := s.Svc.Get(r.Context(), clientID(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) patch(w http.ResponseWriter, r *http.Request) {
	var p domain.DraftPatch
	if !decode(w, r, &p) {
		return
	}
	a, err := s.Svc.Patch(r.Context(), clientID(r), r.PathValue("id"), p)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) action(fn func(ctx context.Context, clientID, id string) (*domain.Application, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := fn(r.Context(), clientID(r), r.PathValue("id"))
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, 200, a)
	}
}

func (s *Server) sign(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, err := s.Svc.Sign(r.Context(), clientID(r), r.PathValue("id"), in.Confirm)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, 200, a)
}

type bankRow struct {
	*domain.Application
	ClientID string `json:"client_id"`
}

func (s *Server) bankList(w http.ResponseWriter, r *http.Request) {
	apps, err := s.Svc.BankList(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	rows := make([]bankRow, len(apps))
	for i, a := range apps {
		rows[i] = bankRow{a, a.ClientID}
	}
	writeJSON(w, 200, rows)
}

// ---------- helpers ----------

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, 400, "bad_request", "некорректный JSON: "+err.Error(), "")
		return false
	}
	return true
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		writeErr(w, 422, "validation", ve.Message, ve.Field)
	case errors.Is(err, domain.ErrNotFound):
		writeErr(w, 404, "not_found", "заявка не найдена", "")
	case errors.Is(err, domain.ErrCompanyNotFound):
		writeErr(w, 404, "company_not_found", "компания с таким ИНН не найдена", "inn")
	case errors.Is(err, domain.ErrInvalidTransition):
		writeErr(w, 409, "invalid_transition", "действие недоступно в текущем статусе заявки", "")
	default:
		s.Log.Error("request failed", "err", err)
		writeErr(w, 500, "internal", "внутренняя ошибка", "")
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, kind, msg, field string) {
	body := map[string]any{"code": kind, "message": msg}
	if field != "" {
		body["field"] = field
	}
	writeJSON(w, code, map[string]any{"error": body})
}
