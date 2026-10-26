package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/auth"
	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/pipeline"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

// Сквозной сценарий: предварительный расчёт без входа → вход через Госуслуги → документы → полный скоринг →
// окончательное одобрение → подписание → выдача → платёж.
func TestEndToEnd(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	users, _ := storage.NewUserFileStore("")
	reg := integrations.MockRegistry{}
	svc := &service.Service{Repo: repo, Users: users, Registry: reg, Now: time.Now}
	authSvc := &service.AuthService{Users: users, Registry: reg, ESIA: integrations.MockESIA{}, Now: time.Now, Sessions: auth.NewSessions(time.Hour), Limiter: auth.NewLimiter(5, time.Minute)}
	if err := authSvc.SeedDemo(t.Context()); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer((&Server{Svc: svc, Auth: authSvc, Repo: repo, Log: log, BankToken: "t", Static: fstest.MapFS{}, DemoESIA: true}).Handler())
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	raw := func(method, path string, body any) *http.Response {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, &buf)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	level1 := map[string]any{"product_id": "business-dev", "amount": 3_000_000, "term_months": 24, "inn": "7701234567",
		"financials": map[string]any{"annual_revenue": 80_000_000, "monthly_obligations": 0}}

	// 1-й уровень — без входа: предварительный расчёт по упрощённому скорингу
	r := raw("POST", "/api/v1/prescore", level1)
	var pre service.Prescore
	_ = json.NewDecoder(r.Body).Decode(&pre)
	if r.StatusCode != 200 || pre.Decision.Type != domain.DecisionApproved || pre.Decision.Final || pre.Company == nil {
		t.Fatalf("prescore = %d %+v", r.StatusCode, pre)
	}
	// а заявку без входа не создать
	if r := raw("POST", "/api/v1/applications", level1); r.StatusCode != 401 {
		t.Fatalf("anonymous create = %d, want 401", r.StatusCode)
	}
	// 2-й уровень начинается со входа через Госуслуги
	if r := raw("POST", "/api/v1/auth/esia", map[string]any{"person_id": "esia-1"}); r.StatusCode != 200 {
		t.Fatalf("esia login = %d", r.StatusCode)
	}

	call := func(method, path string, body any) domain.Application {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, &buf)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s %s → %d %s", method, path, resp.StatusCode, b)
		}
		var a domain.Application
		_ = json.NewDecoder(resp.Body).Decode(&a)
		return a
	}

	a := call("POST", "/api/v1/applications", level1)
	if a.Status != domain.StatusFullForm || a.Company == nil || a.Company.INN != "7701234567" || a.Identity == nil || a.Preliminary == nil {
		t.Fatalf("application must start at level 2 with identity and the preliminary decision: %+v", a)
	}
	if a.Full == nil || a.Full.DirectorPassport == nil {
		t.Fatal("director passport must be prefilled from Gosuslugi")
	}
	call("PATCH", "/api/v1/applications/"+a.ID, map[string]any{"full_form": map[string]any{
		"director_passport": a.Full.DirectorPassport, "collateral": map[string]any{"type": "none"}, "consent": true,
	}})
	call("POST", "/api/v1/applications/"+a.ID+"/submit", nil)

	w := &pipeline.Worker{Repo: repo, Registry: reg, Bureau: integrations.MockBureau{}, Log: log, Now: time.Now}
	for i := 0; i < 3; i++ {
		cur, _ := repo.Get(t.Context(), a.ID)
		if err := w.Step(t.Context(), cur); err != nil {
			t.Fatal(err)
		}
	}
	a = call("GET", "/api/v1/applications/"+a.ID, nil)
	if a.Status != domain.StatusApproved || !a.Decision.Final {
		t.Fatalf("full scoring must end with a final approval: %s, decision %+v", a.Status, a.Decision)
	}
	call("POST", "/api/v1/applications/"+a.ID+"/accept", nil)
	a = call("POST", "/api/v1/applications/"+a.ID+"/sign", map[string]any{"confirm": true})
	if a.Status != domain.StatusIssued {
		t.Fatalf("status = %s", a.Status)
	}
	var me domain.User
	_ = json.NewDecoder(raw("GET", "/api/v1/me", nil).Body).Decode(&me)
	if me.Balance != 1_200_000+3_000_000 {
		t.Fatalf("balance after issue = %d", me.Balance)
	}
	a = call("POST", "/api/v1/applications/"+a.ID+"/pay", nil)
	if len(a.Payments) != 1 {
		t.Fatal("payment not recorded")
	}
	raw("POST", "/api/v1/auth/logout", nil)
	if r := raw("POST", "/api/v1/applications/"+a.ID+"/pay", nil); r.StatusCode != 401 {
		t.Fatalf("pay after logout = %d, want 401", r.StatusCode)
	}
}
