package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/auth"
	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

type harness struct {
	t     *testing.T
	ts    *httptest.Server
	repo  storage.Repository
	users storage.UserRepository
	svc   *service.Service
}

func newHarness(t *testing.T, opts ...func(*Server)) *harness {
	t.Helper()
	repo, _ := storage.NewFileStore("")
	users, _ := storage.NewUserFileStore("")
	reg := integrations.MockRegistry{}
	svc := &service.Service{Repo: repo, Users: users, Registry: reg, Now: time.Now}
	a := &service.AuthService{Users: users, Registry: reg, Now: time.Now, Sessions: auth.NewSessions(time.Hour), Limiter: auth.NewLimiter(5, time.Minute)}
	if err := a.SeedDemo(t.Context()); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	srv := &Server{Svc: svc, Auth: a, Repo: repo, Log: log, BankToken: "bank-secret", Static: static}
	for _, o := range opts {
		o(srv)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &harness{t: t, ts: ts, repo: repo, users: users, svc: svc}
}

func (h *harness) client() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func (h *harness) do(c *http.Client, method, path string, body any, hdr map[string]string) (*http.Response, []byte) {
	h.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		r = strings.NewReader(b)
	default:
		buf, _ := json.Marshal(b)
		r = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest(method, h.ts.URL+path, r)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func (h *harness) login(phone string) *http.Client {
	c := h.client()
	if r, b := h.do(c, "POST", "/api/v1/auth/login", map[string]string{"phone": phone, "password": service.DemoPassword}, nil); r.StatusCode != 200 {
		h.t.Fatalf("login %s: %d %s", phone, r.StatusCode, b)
	}
	return c
}

func TestProtectedEndpointsRequireLogin(t *testing.T) {
	h := newHarness(t)
	anon := h.client()
	for _, ep := range [][2]string{
		{"GET", "/api/v1/me"}, {"GET", "/api/v1/applications"}, {"POST", "/api/v1/applications"},
		{"GET", "/api/v1/applications/x"}, {"PATCH", "/api/v1/applications/x"}, {"POST", "/api/v1/applications/x/submit"},
		{"POST", "/api/v1/applications/x/accept"}, {"POST", "/api/v1/applications/x/sign"}, {"POST", "/api/v1/applications/x/pay"},
		{"POST", "/api/v1/applications/x/cancel"},
	} {
		if r, _ := h.do(anon, ep[0], ep[1], map[string]any{}, nil); r.StatusCode != 401 {
			t.Fatalf("%s %s without session = %d, want 401", ep[0], ep[1], r.StatusCode)
		}
	}
	// публичное — доступно
	for _, p := range []string{"/api/v1/products", "/healthz", "/readyz"} {
		if r, _ := h.do(anon, "GET", p, nil, nil); r.StatusCode != 200 {
			t.Fatalf("GET %s = %d", p, r.StatusCode)
		}
	}
	// поддельная cookie
	req, _ := http.NewRequest("GET", h.ts.URL+"/api/v1/me", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: strings.Repeat("a", 64)})
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 401 {
		t.Fatal("forged session must be rejected")
	}
}

func TestSessionCookieFlags(t *testing.T) {
	h := newHarness(t)
	r, body := h.do(h.client(), "POST", "/api/v1/auth/login", map[string]string{"phone": "79000000001", "password": service.DemoPassword}, nil)
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	sc := r.Header.Get("Set-Cookie")
	for _, flag := range []string{"HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(sc, flag) {
			t.Fatalf("session cookie must be %s: %s", flag, sc)
		}
	}
	if strings.Contains(string(body), "pbkdf2") || strings.Contains(string(body), "password") {
		t.Fatal("login response leaks password data")
	}
}

func TestCannotAccessForeignApplication(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.login("79000000001"), h.login("79000000002")
	_, b := h.do(alice, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev"}, nil)
	var a domain.Application
	json.Unmarshal(b, &a)
	for _, ep := range [][2]string{{"GET", ""}, {"PATCH", ""}, {"POST", "/submit"}, {"POST", "/cancel"}, {"POST", "/pay"}} {
		if r, _ := h.do(bob, ep[0], "/api/v1/applications/"+a.ID+ep[1], map[string]any{}, nil); r.StatusCode != 404 {
			t.Fatalf("bob %s %s = %d, want 404 (no existence leak)", ep[0], ep[1], r.StatusCode)
		}
	}
	_, list := h.do(bob, "GET", "/api/v1/applications", nil, nil)
	if strings.Contains(string(list), a.ID) {
		t.Fatal("foreign application in list")
	}
}

func TestCSRFForeignOriginRejected(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	if r, _ := h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev"}, map[string]string{"Origin": "https://evil.example"}); r.StatusCode != 403 {
		t.Fatalf("cross-origin POST = %d, want 403", r.StatusCode)
	}
	if r, _ := h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev"}, map[string]string{"Origin": h.ts.URL}); r.StatusCode != 201 {
		t.Fatalf("same-origin POST = %d, want 201", r.StatusCode)
	}
}

func TestInputHardening(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	cases := []struct {
		name string
		body any
		want int
	}{
		{"broken json", "{", 400},
		{"unknown field", map[string]any{"product_id": "business-dev", "is_admin": true}, 400},
		{"huge body", `{"product_id":"` + strings.Repeat("x", 70<<10) + `"}`, 400},
		{"unknown product", map[string]any{"product_id": "nope"}, 422},
	}
	for _, tc := range cases {
		if r, b := h.do(c, "POST", "/api/v1/applications", tc.body, nil); r.StatusCode != tc.want {
			t.Fatalf("%s: %d %s, want %d", tc.name, r.StatusCode, b, tc.want)
		}
	}
	if r, _ := h.do(c, "POST", "/api/v1/calc", map[string]any{"product_id": "overdraft", "amount": 1, "term_months": 12}, nil); r.StatusCode != 422 {
		t.Fatal("calc out of range must be 422")
	}
}

func TestLoginBruteForceLocked(t *testing.T) {
	h := newHarness(t)
	c := h.client()
	for i := 0; i < 5; i++ {
		h.do(c, "POST", "/api/v1/auth/login", map[string]string{"phone": "79000000003", "password": "nope"}, nil)
	}
	if r, _ := h.do(c, "POST", "/api/v1/auth/login", map[string]string{"phone": "79000000003", "password": service.DemoPassword}, nil); r.StatusCode != 429 {
		t.Fatalf("after 5 failures login = %d, want 429", r.StatusCode)
	}
}

func TestBankEndpointToken(t *testing.T) {
	h := newHarness(t)
	c := h.client()
	if r, _ := h.do(c, "GET", "/api/v1/bank/applications", nil, nil); r.StatusCode != 401 {
		t.Fatal("bank without token")
	}
	if r, _ := h.do(c, "GET", "/api/v1/bank/applications", nil, map[string]string{"X-Bank-Token": "wrong"}); r.StatusCode != 401 {
		t.Fatal("bank with wrong token")
	}
	if r, _ := h.do(c, "GET", "/api/v1/bank/applications", nil, map[string]string{"X-Bank-Token": "bank-secret"}); r.StatusCode != 200 {
		t.Fatal("bank with token")
	}
}

func TestSecurityHeadersAndQuoteInResponses(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	r, b := h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev", "amount": 3_000_000, "term_months": 24}, nil)
	for _, hd := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "X-Request-ID"} {
		if r.Header.Get(hd) == "" {
			t.Fatalf("missing header %s", hd)
		}
	}
	var a domain.Application
	json.Unmarshal(b, &a)
	want := domain.NewQuote(3_000_000, 19.9, 24)
	if a.Quote == nil || *a.Quote != want {
		t.Fatalf("server must attach the single-source quote: %+v want %+v", a.Quote, want)
	}
	_, pb := h.do(c, "GET", "/api/v1/products", nil, nil)
	var ps []domain.Product
	json.Unmarshal(pb, &ps)
	if ps[0].DefaultPayment == 0 {
		t.Fatal("products must carry default_payment")
	}
}

// Одновременные оплаты не должны списать больше, чем записано платежей.
func TestConcurrentPaymentsConsistent(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	u, _ := h.users.FindByPhone(t.Context(), "79000000001")
	_, b := h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "working-capital", "amount": 300_000, "term_months": 3}, nil)
	var a domain.Application
	json.Unmarshal(b, &a)
	h.repo.Update(t.Context(), a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusApproved} {
			x.Transition(s, "test", "", time.Now())
		}
		d := domain.Decision{Type: domain.DecisionApproved, Final: true}
		d.SetOffer(domain.NewQuote(300_000, 21.5, 3))
		x.Decision = &d
		return nil
	})
	h.do(c, "POST", "/api/v1/applications/"+a.ID+"/accept", nil, nil)
	h.do(c, "POST", "/api/v1/applications/"+a.ID+"/sign", map[string]bool{"confirm": true}, nil)
	before, _ := h.users.GetUser(t.Context(), u.ID)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); h.do(c, "POST", "/api/v1/applications/"+a.ID+"/pay", nil, nil) }()
	}
	wg.Wait()
	after, _ := h.users.GetUser(t.Context(), u.ID)
	got, _ := h.repo.Get(t.Context(), a.ID)
	var recorded int64
	for _, p := range got.Payments {
		recorded += p.Amount
	}
	if len(got.Payments) > 3 {
		t.Fatalf("more payments than the term: %d", len(got.Payments))
	}
	if before.Balance-after.Balance != recorded {
		t.Fatalf("money moved %d ≠ payments recorded %d", before.Balance-after.Balance, recorded)
	}
	if got.Status == domain.StatusClosed && recorded != domain.NewQuote(300_000, 21.5, 3).Total {
		t.Fatalf("closed loan must be paid exactly the total: %d", recorded)
	}
}

// Параллельные «Подписать» не должны зачислить кредит дважды.
func TestConcurrentSignCreditsOnce(t *testing.T) {
	h := newHarness(t)
	c := h.login("79000000001")
	u, _ := h.users.FindByPhone(t.Context(), "79000000001")
	_, b := h.do(c, "POST", "/api/v1/applications", map[string]any{"product_id": "business-dev", "amount": 1_000_000, "term_months": 12}, nil)
	var a domain.Application
	json.Unmarshal(b, &a)
	h.repo.Update(t.Context(), a.ID, func(x *domain.Application) error {
		for _, s := range []domain.Status{domain.StatusSubmitted, domain.StatusDataCheck, domain.StatusScoring, domain.StatusApproved, domain.StatusSigning} {
			x.Transition(s, "test", "", time.Now())
		}
		d := domain.Decision{Type: domain.DecisionApproved, Final: true}
		d.SetOffer(domain.NewQuote(1_000_000, 19.9, 12))
		x.Decision = &d
		return nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.do(c, "POST", "/api/v1/applications/"+a.ID+"/sign", map[string]bool{"confirm": true}, nil)
		}()
	}
	wg.Wait()
	after, _ := h.users.GetUser(t.Context(), u.ID)
	if after.Balance != u.Balance+1_000_000 {
		t.Fatalf("credited %d, want exactly 1 000 000", after.Balance-u.Balance)
	}
}
