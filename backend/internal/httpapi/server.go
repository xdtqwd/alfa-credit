// Package httpapi — REST API (контракт: docs/openapi.yaml) и раздача фронтенда.
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/cache"
	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/idempotency"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/scoring"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type Server struct {
	Svc  *service.Service
	Auth *service.AuthService
	// CookieSecure — флаг Secure для cookie сессии (true за HTTPS).
	CookieSecure bool
	Repo         storage.Repository
	Log          *slog.Logger
	BankToken    string
	// DemoESIA — демо-вход через Госуслуги (мок ЕСИА). Только в APP_MODE=demo: в бою — настоящий OAuth ЕСИА.
	DemoESIA bool
	Static   fs.FS
	Version  string

	Idem           idempotency.Store // nil — Idempotency-Key игнорируется
	Limiter        *RateLimiter      // nil — без лимита
	RequestTimeout time.Duration
	// Ready — обязательные проверки готовности для /readyz: при отказе — 503, экземпляр снимается с трафика.
	Ready map[string]func(ctx context.Context) error
	// Degradable — проверки зависимостей, без которых API продолжает работать (Kafka: события копятся
	// в outbox). Их отказ даёт 200 «degraded», а не 503 — иначе балансировщик снял бы API из-за шины.
	Degradable map[string]func(ctx context.Context) error
	// Platform — сведения об инфраструктуре для кабинета банка (хранилище, шина, outbox).
	Platform func(ctx context.Context) map[string]any
	// AuditLog — журнал аудита по заявке (есть в PostgreSQL-режиме).
	AuditLog func(ctx context.Context, entity, id string) ([]obs.AuditEntry, error)

	calcCache *cache.Cache[string, domain.Quote]
	static    map[string]string // путь → ETag
}

func (s *Server) Handler() http.Handler {
	s.calcCache = cache.New[string, domain.Quote]("calc", cache.Options{MaxEntries: 20_000, TTL: 10 * time.Minute})
	s.static = staticETags(s.Static)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "version": s.Version})
	})
	mux.HandleFunc("GET /readyz", s.readyz)

	mux.HandleFunc("GET /api/v1/products", s.public(s.products))
	mux.HandleFunc("GET /api/v1/scoring/scale", s.public(s.scale))
	mux.HandleFunc("GET /api/v1/security-rules", s.public(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, domain.Security()) }))
	mux.HandleFunc("POST /api/v1/calc", s.calc)
	mux.HandleFunc("GET /api/v1/companies/{inn}", s.company)
	mux.HandleFunc("GET /api/v1/demo/companies", s.public(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, integrations.DemoCompanies()) }))
	mux.HandleFunc("POST /api/v1/prescore", s.prescore)
	mux.HandleFunc("GET /api/v1/esia/persons", s.esiaOnly(s.esiaPersons))
	mux.HandleFunc("POST /api/v1/auth/esia", s.esiaOnly(s.esiaLogin))

	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/register", s.register)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/me", s.signedIn(s.me))
	mux.HandleFunc("PATCH /api/v1/me", s.signedIn(s.updateMe))

	mux.HandleFunc("POST /api/v1/applications", s.client(s.idempotent(s.create)))
	mux.HandleFunc("GET /api/v1/applications", s.client(s.list))
	mux.HandleFunc("GET /api/v1/applications/{id}", s.client(s.get))
	mux.HandleFunc("PATCH /api/v1/applications/{id}", s.client(s.patch))
	mux.HandleFunc("POST /api/v1/applications/{id}/submit", s.client(s.idempotent(s.action(s.Svc.Submit))))
	mux.HandleFunc("POST /api/v1/applications/{id}/accept", s.client(s.idempotent(s.action(s.Svc.Accept))))
	mux.HandleFunc("POST /api/v1/applications/{id}/cancel", s.client(s.idempotent(s.action(s.Svc.Cancel))))
	mux.HandleFunc("POST /api/v1/applications/{id}/sign", s.client(s.idempotent(s.sign)))
	mux.HandleFunc("POST /api/v1/applications/{id}/pay", s.client(s.idempotent(s.action(s.Svc.Pay))))

	mux.HandleFunc("GET /api/v1/bank/applications", s.bank(domain.PermViewApplications, s.bankList))
	mux.HandleFunc("GET /api/v1/bank/applications/{id}/audit", s.bank(domain.PermViewAudit, s.bankAudit))
	mux.HandleFunc("GET /api/v1/bank/platform", s.bank(domain.PermViewPlatform, s.platform))

	// неизвестные пути API — JSON 404/405, а не HTML файлового сервера
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { s.apiNotFound(mux, w, r) })
	mux.Handle("/", s.staticFiles(http.FileServerFS(s.Static)))

	var h http.Handler = mux
	h = captureRoute(h) // вплотную к роутеру: дальше по цепочке запрос копируется (WithContext)
	h = etag(h)
	h = sameOrigin(h)
	h = s.deadline(h)
	h = s.rateLimit(h)
	h = compress(h)
	h = securityHeaders(h)
	h = s.recoverer(h)
	h = s.observe(h)
	return h
}

// MetricsHandler — /metrics в формате Prometheus. Отдаётся на отдельном служебном порту
// (METRICS_ADDR), который не публикуется наружу.
func MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		obs.Default.WriteText(w)
		cs := cache.All()
		fmt.Fprintf(w, "# HELP cache_requests_total Обращения к кэшам\n# TYPE cache_requests_total counter\n")
		for _, c := range cs {
			fmt.Fprintf(w, "cache_requests_total{cache=%q,result=\"hit\"} %d\ncache_requests_total{cache=%q,result=\"miss\"} %d\n", c.Name, c.Hits, c.Name, c.Misses)
		}
		fmt.Fprintf(w, "# HELP cache_entries Записей в кэше\n# TYPE cache_entries gauge\n")
		for _, c := range cs {
			fmt.Fprintf(w, "cache_entries{cache=%q} %d\n", c.Name, c.Size)
		}
		fmt.Fprintf(w, "# HELP cache_evictions_total Вытеснения из кэша\n# TYPE cache_evictions_total counter\n")
		for _, c := range cs {
			fmt.Fprintf(w, "cache_evictions_total{cache=%q} %d\n", c.Name, c.Evictions)
		}
	})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	checks := map[string]string{}
	ok := true
	run := func(name string, fn func(context.Context) error) {
		if err := fn(ctx); err != nil {
			checks[name], ok = "fail: "+err.Error(), false
			return
		}
		checks[name] = "ok"
	}
	run("storage", s.Repo.Ping)
	for name, fn := range s.Ready {
		run(name, fn)
	}
	critical := ok
	for name, fn := range s.Degradable {
		run(name, fn)
	}
	code, status := 200, "ready"
	switch {
	case !critical:
		code, status = 503, "not ready"
	case !ok:
		status = "degraded"
	}
	writeJSON(w, code, map[string]any{"status": status, "checks": checks})
}

// apiNotFound различает «нет такого пути» (404) и «путь есть, метод не тот» (405 + Allow).
func (s *Server) apiNotFound(mux *http.ServeMux, w http.ResponseWriter, r *http.Request) {
	var allow []string
	for _, m := range []string{"GET", "POST", "PATCH", "PUT", "DELETE"} {
		if m == r.Method {
			continue
		}
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pat := mux.Handler(probe); pat != "" && pat != "/api/" && pat != "/" {
			allow = append(allow, m)
		}
	}
	if len(allow) > 0 {
		w.Header().Set("Allow", strings.Join(allow, ", "))
		writeErr(w, 405, "method_not_allowed", "метод "+r.Method+" не поддерживается для этого адреса", "")
		return
	}
	writeErr(w, 404, "not_found", "нет такого метода API", "")
}

// public — справочные данные: можно кэшировать в браузере и на CDN.
func (s *Server) public(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		h(w, r)
	}
}

// staticETags считает ETag каждого файла фронтенда один раз при старте (embed FS без дат изменения).
func staticETags(fsys fs.FS) map[string]string {
	out := map[string]string{}
	if fsys == nil {
		return out
	}
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		out["/"+p] = `"` + hex.EncodeToString(sum[:10]) + `"`
		return nil
	})
	if t, ok := out["/index.html"]; ok {
		out["/"] = t
	}
	return out
}

// staticFiles — ETag и revalidate для статики: после деплоя браузер сразу получает новую версию,
// а без изменений — 304 без тела.
func (s *Server) staticFiles(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if tag, ok := s.static[r.URL.Path]; ok {
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- middleware ----------

type ctxKey string

const clientKey ctxKey = "client"

const sessionCookie = "sid"

const userKey ctxKey = "user"

// sessionUser — пользователь по cookie сессии; nil и false, если сессии нет или она истекла.
// Роль читается из хранилища на каждом запросе: снятый с сотрудника доступ действует сразу.
func (s *Server) sessionUser(w http.ResponseWriter, r *http.Request) (*domain.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil, false
	}
	uid, ok := s.Auth.Sessions.Get(r.Context(), c.Value)
	if !ok {
		s.clearCookie(w)
		return nil, false
	}
	u, err := s.Auth.Me(r.Context(), uid)
	if err != nil {
		s.clearCookie(w)
		return nil, false
	}
	return u, true
}

func withUser(r *http.Request, u *domain.User) *http.Request {
	if info, ok := r.Context().Value(infoKey).(*reqInfo); ok {
		info.client = u.ID
	}
	ctx := obs.WithActor(context.WithValue(context.WithValue(r.Context(), clientKey, u.ID), userKey, u), u.ID)
	ctx = obs.WithLogger(ctx, obs.L(ctx).With("client", u.ID, "role", u.Role))
	return r.WithContext(ctx)
}

// signedIn — любой вошедший пользователь (клиент или сотрудник банка): профиль, выход.
func (s *Server) signedIn(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.sessionUser(w, r)
		if !ok {
			writeErr(w, 401, "unauthorized", "войдите, чтобы продолжить", "")
			return
		}
		h(w, withUser(r, u))
	}
}

// client — разделы представителя бизнеса (HttpOnly cookie). Без входа нельзя оформить,
// подписать или оплатить; сотрудник банка заявок от имени компании не подаёт.
// В проде: OIDC (Альфа ID) на API Gateway.
func (s *Server) client(h http.HandlerFunc) http.HandlerFunc {
	return s.signedIn(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).IsEmployee() {
			writeErr(w, 403, "forbidden", "раздел для клиентов — сотрудник банка работает в кабинете банка", "")
			return
		}
		h(w, r)
	})
}

func (s *Server) setCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: s.CookieSecure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(s.Auth.Sessions.Lifetime().Seconds()),
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteStrictMode})
}

// bank — кабинет банка. Два способа доступа:
//   - сессия сотрудника банка с нужным правом (вход по телефону и паролю, как у клиента);
//   - заголовок X-Bank-Token — для служебных интеграций и скриптов (BANK_TOKEN).
//
// Клиенту с сессией — 403, без сессии и токена — 401.
func (s *Server) bank(perm string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if tok := r.Header.Get("X-Bank-Token"); tok != "" {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(s.BankToken)) != 1 {
				writeErr(w, 401, "unauthorized", "неверный X-Bank-Token", "")
				return
			}
			h(w, r.WithContext(obs.WithActor(r.Context(), "bank")))
			return
		}
		u, ok := s.sessionUser(w, r)
		switch {
		case !ok:
			writeErr(w, 401, "unauthorized", "войдите как сотрудник банка", "")
		case !u.IsEmployee():
			writeErr(w, 403, "forbidden", "кабинет банка доступен только сотрудникам банка", "")
		case !u.Can(perm):
			writeErr(w, 403, "forbidden", "у вашей учётной записи нет доступа к этому разделу", "")
		default:
			h(w, withUser(r, u))
		}
	}
}

func currentUser(r *http.Request) *domain.User { return r.Context().Value(userKey).(*domain.User) }

func clientID(r *http.Request) string { return r.Context().Value(clientKey).(string) }

// ---------- auth handlers ----------

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone    string `json:"phone"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, tok, err := s.Auth.Login(r.Context(), in.Phone, in.Password, clientIP(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setCookie(w, tok)
	writeJSON(w, 200, u)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in service.RegisterInput
	if !decode(w, r, &in) {
		return
	}
	u, tok, err := s.Auth.Register(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setCookie(w, tok)
	writeJSON(w, 201, u)
}

// esiaOnly — демо-вход через Госуслуги есть только в демо-режиме.
func (s *Server) esiaOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.DemoESIA || s.Auth.ESIA == nil {
			writeErr(w, 404, "not_found", "вход через Госуслуги в этой сборке не подключён", "")
			return
		}
		h(w, r)
	}
}

func (s *Server) esiaPersons(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Auth.ESIAPersons(r.Context()))
}

func (s *Server) esiaLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PersonID string `json:"person_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, tok, err := s.Auth.ESIALogin(r.Context(), in.PersonID, clientIP(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setCookie(w, tok)
	writeJSON(w, 200, u)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.Auth.Logout(r.Context(), c.Value)
	}
	s.clearCookie(w)
	w.WriteHeader(204)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, currentUser(r))
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	var p service.ProfilePatch
	if !decode(w, r, &p) {
		return
	}
	u, err := s.Auth.UpdateProfile(r.Context(), clientID(r), p)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, u)
}

// ---------- handlers ----------

// scale — шкала балльной модели: где отказ, где другие условия, где одобрение.
func (s *Server) scale(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]int{"base": scoring.BaseScore, "reject_below": scoring.RejectThreshold, "approve_from": scoring.ApproveThreshold, "max_dti_percent": int(scoring.MaxDTI * 100)})
}

func (s *Server) products(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, domain.Products())
}

// calc — расчёт платежа. Ползунок калькулятора шлёт много одинаковых запросов, поэтому
// результат кэшируется по (продукт, сумма, срок): ставки — часть кода продукта, расчёт детерминирован.
func (s *Server) calc(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ProductID  string `json:"product_id"`
		Amount     int64  `json:"amount"`
		TermMonths int    `json:"term_months"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, ok := domain.FindProduct(in.ProductID)
	if !ok {
		writeErr(w, 422, "validation", "неизвестный продукт", "product_id")
		return
	}
	key := fmt.Sprintf("%s|%d|%d", p.ID, in.Amount, in.TermMonths)
	res, err := s.calcCache.GetOrLoad(r.Context(), key, func(context.Context) (domain.Quote, error) {
		return domain.Calculate(p, in.Amount, in.TermMonths)
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) company(w http.ResponseWriter, r *http.Request) {
	c, err := s.Svc.LookupCompany(r.Context(), r.PathValue("inn"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, c)
}

// prescore — предварительный расчёт 1-го уровня: без входа и без заявки.
func (s *Server) prescore(w http.ResponseWriter, r *http.Request) {
	var in domain.Level1
	if !decode(w, r, &in) {
		return
	}
	res, err := s.Svc.Prescore(r.Context(), &in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, res)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var in domain.Level1
	if !decode(w, r, &in) {
		return
	}
	a, err := s.Svc.Create(r.Context(), clientID(r), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/applications/"+a.ID)
	writeJSON(w, 201, a)
}

// pageParams разбирает ?status=a,b&q=…&limit=&offset= (limit ≤ maxLimit; 0 — по умолчанию def).
func pageParams(r *http.Request, def, maxLimit int) (storage.Filter, *domain.ValidationError) {
	q := r.URL.Query()
	f := storage.Filter{Query: strings.TrimSpace(q.Get("q")), Limit: def}
	if len(f.Query) > 100 {
		return f, &domain.ValidationError{Field: "q", Message: "слишком длинный запрос"}
	}
	for _, st := range strings.Split(q.Get("status"), ",") {
		if st = strings.TrimSpace(st); st == "" {
			continue
		}
		if !domain.KnownStatus(domain.Status(st)) {
			return f, &domain.ValidationError{Field: "status", Message: "неизвестный статус " + st}
		}
		f.Statuses = append(f.Statuses, domain.Status(st))
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			return f, &domain.ValidationError{Field: "limit", Message: fmt.Sprintf("limit — число от 1 до %d", maxLimit)}
		}
		f.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return f, &domain.ValidationError{Field: "offset", Message: "offset — неотрицательное число"}
		}
		f.Offset = n
	}
	return f, nil
}

// setPage — общее число записей и ссылки на соседние страницы (RFC 8288), тело остаётся массивом.
func setPage(w http.ResponseWriter, r *http.Request, f storage.Filter, total int) {
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	if f.Limit <= 0 {
		return
	}
	var links []string
	mk := func(off int, rel string) {
		q := r.URL.Query()
		q.Set("limit", strconv.Itoa(f.Limit))
		q.Set("offset", strconv.Itoa(off))
		links = append(links, fmt.Sprintf(`<%s?%s>; rel="%s"`, r.URL.Path, q.Encode(), rel))
	}
	if f.Offset+f.Limit < total {
		mk(f.Offset+f.Limit, "next")
	}
	if f.Offset > 0 {
		mk(max(0, f.Offset-f.Limit), "prev")
	}
	if len(links) > 0 {
		w.Header().Set("Link", strings.Join(links, ", "))
	}
}

// list — заявки клиента. Без параметров — все (их у клиента немного), с limit/offset — постранично.
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	f, verr := pageParams(r, 0, 100)
	if verr != nil {
		s.fail(w, r, verr)
		return
	}
	apps, total, err := s.Svc.Page(r.Context(), clientID(r), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	setPage(w, r, f, total)
	writeJSON(w, 200, apps)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	a, err := s.Svc.Get(r.Context(), clientID(r), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
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
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) action(fn func(ctx context.Context, clientID, id string) (*domain.Application, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := fn(r.Context(), clientID(r), r.PathValue("id"))
		if err != nil {
			s.fail(w, r, err)
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
		s.fail(w, r, err)
		return
	}
	writeJSON(w, 200, a)
}

type bankRow struct {
	*domain.Application
	ClientID string `json:"client_id"`
}

// bankList — очередь заявок для сотрудника: поиск по номеру/ИНН/компании, фильтр статусов, страницы.
func (s *Server) bankList(w http.ResponseWriter, r *http.Request) {
	f, verr := pageParams(r, 50, 200)
	if verr != nil {
		s.fail(w, r, verr)
		return
	}
	apps, total, err := s.Svc.BankPage(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rows := make([]bankRow, len(apps))
	for i, a := range apps {
		rows[i] = bankRow{a, a.ClientID}
	}
	setPage(w, r, f, total)
	writeJSON(w, 200, rows)
}

// bankAudit — журнал аудита по заявке (кто и когда что делал).
func (s *Server) bankAudit(w http.ResponseWriter, r *http.Request) {
	if s.AuditLog == nil {
		writeErr(w, 501, "not_implemented", "журнал аудита доступен в режиме PostgreSQL", "")
		return
	}
	entries, err := s.AuditLog(r.Context(), "application", r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if entries == nil {
		entries = []obs.AuditEntry{}
	}
	writeJSON(w, 200, entries)
}

// platform — состояние инфраструктуры для кабинета банка: хранилище, шина событий, outbox, кэши.
func (s *Server) platform(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"version": s.Version}
	if s.Platform != nil {
		for k, v := range s.Platform(r.Context()) {
			out[k] = v
		}
	}
	caches := []map[string]any{}
	for _, c := range cache.All() {
		rate := 0.0
		if c.Hits+c.Misses > 0 {
			rate = float64(c.Hits) / float64(c.Hits+c.Misses)
		}
		caches = append(caches, map[string]any{"name": c.Name, "hits": c.Hits, "misses": c.Misses, "size": c.Size, "hit_rate": rate})
	}
	out["caches"] = caches
	out["events_published"] = obs.Default.CounterTotal("events_published_total")
	writeJSON(w, 200, out)
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

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		writeErr(w, 422, "validation", ve.Message, ve.Field)
	case errors.Is(err, domain.ErrNotFound):
		writeErr(w, 404, "not_found", "заявка не найдена", "")
	case errors.Is(err, domain.ErrCompanyNotFound):
		writeErr(w, 404, "company_not_found", "компания с таким ИНН не найдена", "inn")
	case errors.Is(err, domain.ErrUnauthorized):
		writeErr(w, 401, "unauthorized", "войдите, чтобы продолжить", "")
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeErr(w, 401, "invalid_credentials", "неверный телефон или пароль", "password")
	case errors.Is(err, domain.ErrTooManyAttempts):
		writeErr(w, 429, "too_many_attempts", "слишком много попыток входа, подождите минуту", "")
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeErr(w, 422, "insufficient_funds", "недостаточно средств на расчётном счёте", "balance")
	case errors.Is(err, domain.ErrInvalidTransition):
		writeErr(w, 409, "invalid_transition", "действие недоступно в текущем статусе заявки", "")
	case errors.Is(err, context.DeadlineExceeded):
		obs.L(r.Context()).Error("request timeout", "err", err)
		writeErr(w, 503, "timeout", "сервис не успел ответить, повторите запрос", "")
	case errors.Is(err, context.Canceled):
		writeErr(w, 499, "canceled", "запрос отменён", "")
	default:
		obs.L(r.Context()).Error("request failed", "err", err)
		writeErr(w, 500, "internal", "внутренняя ошибка", "")
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	// все деньги по заявке считает сервер (domain.Quote/Schedule) — дополняем перед отдачей
	switch x := v.(type) {
	case *domain.Application:
		x.Enrich()
	case []*domain.Application:
		for _, a := range x {
			a.Enrich()
		}
	case []bankRow:
		for _, r := range x {
			r.Enrich()
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr — единый формат ошибки. request_id совпадает с X-Request-ID: клиент сообщает его
// в поддержку, а по нему находятся все логи запроса.
func writeErr(w http.ResponseWriter, code int, kind, msg, field string) {
	body := map[string]any{"code": kind, "message": msg}
	if field != "" {
		body["field"] = field
	}
	if rid := w.Header().Get("X-Request-ID"); rid != "" {
		body["request_id"] = rid
	}
	writeJSON(w, code, map[string]any{"error": body})
}
