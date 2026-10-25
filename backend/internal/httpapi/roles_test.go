package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/service"
)

var bankEndpoints = []string{"/api/v1/bank/applications", "/api/v1/bank/applications/x/audit", "/api/v1/bank/platform"}

func TestEmployeeOpensBankCabinetBySession(t *testing.T) {
	h := newHarness(t)
	emp := h.login(service.DemoEmployee.Phone)

	r, body := h.do(emp, "GET", "/api/v1/me", nil, nil)
	var me domain.User
	_ = json.Unmarshal(body, &me)
	if r.StatusCode != 200 || me.Role != domain.RoleEmployee || me.Employee == nil || me.Employee.PersonnelNo == "" {
		t.Fatalf("me = %d %s", r.StatusCode, body)
	}
	for _, p := range []string{"/api/v1/bank/applications", "/api/v1/bank/platform"} {
		if r, b := h.do(emp, "GET", p, nil, nil); r.StatusCode != 200 {
			t.Fatalf("employee GET %s = %d %s", p, r.StatusCode, b)
		}
	}
	// служебный токен по-прежнему работает, неверный не «перебивается» сессией
	if r, _ := h.do(emp, "GET", "/api/v1/bank/applications", nil, map[string]string{"X-Bank-Token": "wrong"}); r.StatusCode != 401 {
		t.Fatal("wrong token must be rejected even with employee session")
	}
}

func TestClientCannotOpenBankCabinet(t *testing.T) {
	h := newHarness(t)
	cl := h.login("79000000001")
	for _, p := range bankEndpoints {
		if r, _ := h.do(cl, "GET", p, nil, nil); r.StatusCode != 403 {
			t.Fatalf("client GET %s = %d, want 403", p, r.StatusCode)
		}
	}
}

func TestEmployeeCannotActAsClient(t *testing.T) {
	h := newHarness(t)
	emp := h.login(service.DemoEmployee.Phone)
	for _, ep := range [][2]string{
		{"GET", "/api/v1/applications"}, {"POST", "/api/v1/applications"}, {"POST", "/api/v1/applications/x/pay"},
	} {
		if r, _ := h.do(emp, ep[0], ep[1], map[string]any{"product_id": "business-dev", "amount": 1_000_000, "term_months": 12}, nil); r.StatusCode != 403 {
			t.Fatalf("employee %s %s = %d, want 403", ep[0], ep[1], r.StatusCode)
		}
	}
	// профиль: почту можно, должность — нет
	if r, b := h.do(emp, "PATCH", "/api/v1/me", map[string]string{"email": "anna@alfabank.example"}, nil); r.StatusCode != 200 {
		t.Fatalf("patch email = %d %s", r.StatusCode, b)
	}
	if r, _ := h.do(emp, "PATCH", "/api/v1/me", map[string]string{"position": "Председатель правления"}, nil); r.StatusCode != 422 {
		t.Fatalf("employee position change = %d, want 422", r.StatusCode)
	}
}

func TestRevokedPermissionAppliesImmediately(t *testing.T) {
	h := newHarness(t)
	emp := h.login(service.DemoEmployee.Phone)
	u, err := h.users.FindByPhone(t.Context(), service.DemoEmployee.Phone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.users.UpdateUser(t.Context(), u.ID, func(x *domain.User) error {
		x.Employee.Permissions = []string{domain.PermViewApplications}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if r, _ := h.do(emp, "GET", "/api/v1/bank/applications", nil, nil); r.StatusCode != 200 {
		t.Fatal("kept permission must work")
	}
	if r, _ := h.do(emp, "GET", "/api/v1/bank/platform", nil, nil); r.StatusCode != 403 {
		t.Fatal("revoked permission must be denied without re-login")
	}
}
