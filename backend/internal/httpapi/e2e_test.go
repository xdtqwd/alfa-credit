package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/integrations"
	"github.com/xdtqwd/alfa-credit/internal/pipeline"
	"github.com/xdtqwd/alfa-credit/internal/service"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

// Сквозной сценарий: калькулятор → анкета → отправка → конвейер → одобрение → подписание → выдача.
func TestEndToEnd(t *testing.T) {
	repo, _ := storage.NewFileStore("")
	reg := integrations.MockRegistry{}
	svc := &service.Service{Repo: repo, Registry: reg, Now: time.Now}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer((&Server{Svc: svc, Repo: repo, Log: log, BankToken: "t", Static: fstest.MapFS{}}).Handler())
	defer ts.Close()

	call := func(method, path string, body any) domain.Application {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, ts.URL+path, &buf)
		req.Header.Set("X-Client-ID", "test-client-1")
		resp, err := http.DefaultClient.Do(req)
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

	a := call("POST", "/api/v1/applications", map[string]any{"product_id": "business-dev", "amount": 3_000_000, "term_months": 24})
	a = call("PATCH", "/api/v1/applications/"+a.ID, map[string]any{
		"inn":        "7701234567",
		"financials": map[string]any{"annual_revenue": 80_000_000, "monthly_obligations": 0, "employees": 25},
		"contact":    map[string]any{"full_name": "Смирнов Алексей", "position": "Директор", "phone": "+79990001122", "email": "a@b.ru"},
		"consent":    true,
	})
	if a.Company == nil || a.Company.Name == "" {
		t.Fatal("company must be autofilled from registry")
	}
	call("POST", "/api/v1/applications/"+a.ID+"/submit", nil)

	w := &pipeline.Worker{Repo: repo, Registry: reg, Bureau: integrations.MockBureau{}, Log: log, Now: time.Now}
	for i := 0; i < 3; i++ {
		cur, _ := repo.Get(t.Context(), a.ID)
		if err := w.Step(t.Context(), cur); err != nil {
			t.Fatal(err)
		}
	}
	a = call("GET", "/api/v1/applications/"+a.ID, nil)
	if a.Status != domain.StatusApproved {
		t.Fatalf("status = %s, decision %+v", a.Status, a.Decision)
	}
	call("POST", "/api/v1/applications/"+a.ID+"/accept", nil)
	a = call("POST", "/api/v1/applications/"+a.ID+"/sign", map[string]any{"confirm": true})
	if a.Status != domain.StatusIssued {
		t.Fatalf("status = %s", a.Status)
	}
}
