// selfcheck — сквозная проверка работающего стека через публичный API (только stdlib).
// Входит в образ рядом с сервисом, поэтому запускается без Go, Node и curl на машине:
//
//	docker compose exec api /selfcheck                      # полный путь клиента и банка
//	docker compose exec api /selfcheck -phase submit        # только подать заявку (для проверки отказа Kafka)
//	docker compose exec api /selfcheck -phase await -number КР-000001
//
// Проверяет: здоровье и готовность (PostgreSQL, Kafka), вход, создание заявки с Idempotency-Key,
// анкету, подачу, решение конвейера через Kafka, ETag/304, принятие, подписание и выдачу,
// оплату без двойного списания, журнал аудита, метрики, JSON-ошибки.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	base       = flag.String("base", "http://127.0.0.1:8080", "адрес API")
	metrics    = flag.String("metrics", "http://127.0.0.1:9090/metrics", "адрес /metrics (пусто — не проверять)")
	token      = flag.String("token", envOr("BANK_TOKEN", "demo-bank"), "X-Bank-Token кабинета банка")
	phone      = flag.String("phone", "79000000001", "демо-аккаунт клиента")
	staffPhone = flag.String("staff-phone", "79000000005", "демо-аккаунт сотрудника банка")
	pass       = flag.String("password", "demo1234", "пароль демо-аккаунта")
	phase      = flag.String("phase", "all", "all | submit | await")
	number     = flag.String("number", "", "номер заявки для -phase await")
	wait       = flag.Duration("wait", 90*time.Second, "сколько ждать решения конвейера")
	expect     = flag.String("expect-bus", "", "ожидаемая шина: kafka | memory (пусто — любая)")
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type checker struct {
	c           *http.Client
	etagChecked bool
	failed      int
	passed      int
}

func (k *checker) ok(format string, a ...any) { k.passed++; fmt.Printf("  ✓ "+format+"\n", a...) }
func (k *checker) fail(format string, a ...any) {
	k.failed++
	fmt.Printf("  ✗ "+format+"\n", a...)
}

// check печатает okMsg или failMsg (уже отформатированные) и возвращает cond.
func (k *checker) check(cond bool, okMsg, failMsg string) bool {
	if cond {
		k.ok("%s", okMsg)
	} else {
		k.fail("%s", failMsg)
	}
	return cond
}

var f = fmt.Sprintf

type resp struct {
	code int
	hdr  http.Header
	body []byte
	dur  time.Duration
}

func (r resp) json(v any) error { return json.Unmarshal(r.body, v) }

func (k *checker) do(method, path string, body any, hdr map[string]string) (resp, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	u := path
	if !strings.HasPrefix(path, "http") {
		u = *base + path
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return resp{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	start := time.Now()
	res, err := k.c.Do(req)
	if err != nil {
		return resp{}, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{code: res.StatusCode, hdr: res.Header, body: b, dur: time.Since(start)}, nil
}

type app struct {
	ID      string `json:"id"`
	Number  string `json:"number"`
	Status  string `json:"status"`
	Amount  int64  `json:"amount"`
	Score   *int   `json:"score"`
	Company *struct {
		Name            string `json:"name"`
		RevenueLastYear int64  `json:"revenue_last_year"`
	} `json:"company"`
	Decision *struct {
		Amount int64   `json:"amount"`
		Rate   float64 `json:"rate"`
		Final  bool    `json:"final"`
	} `json:"decision"`
	Payments    []json.RawMessage `json:"payments"`
	NextPayment int64             `json:"next_payment"`
}

var decided = map[string]bool{"approved": true, "counter_offer": true, "rejected": true, "need_info": true}

func main() {
	flag.Parse()
	jar, _ := cookiejar.New(nil)
	k := &checker{c: &http.Client{Jar: jar, Timeout: 15 * time.Second}}
	fmt.Printf("Самопроверка %s (фаза: %s)\n", *base, *phase)

	var err error
	switch *phase {
	case "submit":
		err = k.phaseSubmit()
	case "await":
		err = k.phaseAwait()
	default:
		err = k.phaseAll()
	}
	if err != nil {
		k.fail("прервано: %v", err)
	}
	fmt.Printf("\nИтог: пройдено %d, ошибок %d\n", k.passed, k.failed)
	if k.failed > 0 {
		os.Exit(1)
	}
}

// ---------- шаги ----------

func (k *checker) infra() (bus string, err error) {
	fmt.Println("\nИнфраструктура")
	r, err := k.do("GET", "/healthz", nil, nil)
	if err != nil {
		return "", fmt.Errorf("API недоступен: %w", err)
	}
	k.check(r.code == 200, "/healthz — сервис жив", f("/healthz → %d", r.code))

	r, _ = k.do("GET", "/readyz", nil, nil)
	var rd struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	_ = r.json(&rd)
	k.check(r.code == 200 && rd.Status == "ready", f("/readyz — готов: %s", fmtChecks(rd.Checks)), f("/readyz → %d %s: %s", r.code, rd.Status, fmtChecks(rd.Checks)))

	r, _ = k.do("GET", "/api/v1/bank/platform", nil, map[string]string{"X-Bank-Token": *token})
	var pl map[string]any
	_ = r.json(&pl)
	bus, _ = pl["bus"].(string)
	storage, _ := pl["storage"].(string)
	k.check(r.code == 200, f("платформа: хранилище %s, шина %s, в outbox ждут %v", storage, bus, pl["outbox_pending"]), f("кабинет банка /platform → %d", r.code))
	if *expect != "" {
		k.check(bus == *expect, f("шина событий — %s, как ожидалось", bus), f("шина %q, ожидалась %q", bus, *expect))
	}
	return bus, nil
}

func (k *checker) login() error {
	r, err := k.do("POST", "/api/v1/auth/login", map[string]string{"phone": *phone, "password": *pass}, nil)
	if err != nil {
		return err
	}
	if !k.check(r.code == 200, f("вход клиента %s", *phone), f("вход → %d %s", r.code, r.body)) {
		return errors.New("не удалось войти")
	}
	return nil
}

// newApp создаёт заявку с ключом идемпотентности, заполняет анкету и подаёт её.
func (k *checker) newApp() (app, error) {
	var a app
	key := fmt.Sprintf("selfcheck-%d", time.Now().UnixNano())
	body := map[string]any{"product_id": "business-dev", "amount": 3_000_000, "term_months": 24}
	r1, err := k.do("POST", "/api/v1/applications", body, map[string]string{"Idempotency-Key": key})
	if err != nil {
		return a, err
	}
	_ = r1.json(&a)
	if !k.check(r1.code == 201 && a.ID != "", f("заявка создана: %s (сразу анкета, без «оставить заявку»)", a.Number), f("создание заявки → %d %s", r1.code, r1.body)) {
		return a, errors.New("заявка не создана")
	}
	r2, _ := k.do("POST", "/api/v1/applications", body, map[string]string{"Idempotency-Key": key})
	var a2 app
	_ = r2.json(&a2)
	k.check(r2.hdr.Get("Idempotent-Replayed") == "true" && a2.ID == a.ID,
		"повтор с тем же Idempotency-Key вернул ту же заявку, дубль не создан", f("повтор создал дубль: %s vs %s", a2.ID, a.ID))

	revenue := int64(84_000_000)
	if a.Company != nil && a.Company.RevenueLastYear > 0 {
		revenue = a.Company.RevenueLastYear
	}
	r, _ := k.do("PATCH", "/api/v1/applications/"+a.ID, map[string]any{
		"financials": map[string]any{"annual_revenue": revenue, "monthly_obligations": 0, "employees": 0},
		"consent":    true,
	}, nil)
	k.check(r.code == 200, "анкета заполнена (компания и контакт — из профиля, выручка — как в ФНС)", f("автосохранение → %d %s", r.code, r.body))

	r, _ = k.do("POST", "/api/v1/applications/"+a.ID+"/submit", nil, nil)
	_ = r.json(&a)
	if !k.check(r.code == 200 && a.Status == "submitted", f("заявка подана за %d мс — ответ не ждёт конвейер", r.dur.Milliseconds()), f("подача → %d %s", r.code, r.body)) {
		return a, errors.New("заявка не подана")
	}
	return a, nil
}

// await ждёт решения: его принимает воркер, получив событие из шины (Kafka).
func (k *checker) await(a app) (app, error) {
	start := time.Now()
	var etag string
	notModified := false
	for time.Since(start) < *wait {
		hdr := map[string]string{}
		if etag != "" {
			hdr["If-None-Match"] = etag
		}
		r, err := k.do("GET", "/api/v1/applications/"+a.ID, nil, hdr)
		if err != nil {
			return a, err
		}
		if r.code == http.StatusNotModified {
			notModified = true
		} else if r.code == 200 {
			etag = r.hdr.Get("ETag")
			_ = r.json(&a)
			if decided[a.Status] {
				score := "—"
				if a.Score != nil {
					score = fmt.Sprint(*a.Score)
				}
				kind := "предварительное"
				if a.Decision != nil && a.Decision.Final {
					kind = "окончательное"
				}
				k.ok("%s решение конвейера через %.1f с: %s, балл %s", kind, time.Since(start).Seconds(), a.Status, score)
				if !k.etagChecked {
					k.etagChecked = true
					k.check(notModified, "опрос статуса: пока ничего не изменилось, сервер отвечает 304 без тела (ETag)", "ни одного 304 при опросе: ETag не работает")
				}
				return a, nil
			}
		} else {
			return a, fmt.Errorf("GET заявки → %d", r.code)
		}
		time.Sleep(700 * time.Millisecond)
	}
	k.fail("за %s решение не пришло: статус %s (воркер не получает события из шины?)", *wait, a.Status)
	return a, errors.New("нет решения")
}

func (k *checker) loan(a app) {
	fmt.Println("\nПодписание, выдача, оплата")
	if a.Status != "approved" && a.Status != "counter_offer" {
		k.ok("решение «%s» — подписание не требуется", a.Status)
		return
	}
	var me struct {
		Balance int64 `json:"balance"`
	}
	r, _ := k.do("GET", "/api/v1/me", nil, nil)
	_ = r.json(&me)
	before := me.Balance

	// уровень 2: предварительные условия → полная анкета → окончательное решение
	if a.Decision != nil && !a.Decision.Final {
		r, _ = k.do("POST", "/api/v1/applications/"+a.ID+"/accept", nil, nil)
		_ = r.json(&a)
		if !k.check(r.code == 200 && a.Status == "full_form", "предварительные условия приняты — открыта полная анкета", f("принятие предварительного решения → %d %s", r.code, r.body)) {
			return
		}
		r, _ = k.do("PATCH", "/api/v1/applications/"+a.ID, map[string]any{"full_form": map[string]any{
			"employees": 12, "premises": "rent", "purpose_details": "Самопроверка: развитие бизнеса, закупка оборудования",
			"collateral": map[string]any{"type": "none"}, "statements_consent": true,
		}}, nil)
		k.check(r.code == 200, "полная анкета заполнена: о бизнесе, обеспечение, согласие на выписки", f("полная анкета → %d %s", r.code, r.body))
		r, _ = k.do("POST", "/api/v1/applications/"+a.ID+"/submit", nil, nil)
		_ = r.json(&a)
		if !k.check(r.code == 200 && a.Status == "verification", "полная анкета отправлена на окончательное решение", f("отправка полной анкеты → %d %s", r.code, r.body)) {
			return
		}
		var err error
		if a, err = k.await(a); err != nil {
			return
		}
		if !k.check(a.Status == "approved" && a.Decision != nil && a.Decision.Final, "кредит одобрен окончательно", f("окончательное решение: %s", a.Status)) {
			return
		}
	}
	r, _ = k.do("POST", "/api/v1/applications/"+a.ID+"/accept", nil, nil)
	_ = r.json(&a)
	k.check(r.code == 200 && a.Status == "signing", "условия приняты", f("принятие → %d %s", r.code, r.body))
	r, _ = k.do("POST", "/api/v1/applications/"+a.ID+"/sign", map[string]bool{"confirm": true}, nil)
	_ = r.json(&a)
	k.check(r.code == 200 && a.Status == "issued", "договор подписан, кредит выдан", f("подписание → %d %s", r.code, r.body))
	r, _ = k.do("GET", "/api/v1/me", nil, nil)
	_ = r.json(&me)
	k.check(me.Balance-before == a.Amount, f("на счёт зачислено ровно %d ₽", a.Amount), f("зачислено %d ₽, а выдано %d ₽", me.Balance-before, a.Amount))

	afterIssue, pay := me.Balance, a.NextPayment
	key := fmt.Sprintf("selfcheck-pay-%d", time.Now().UnixNano())
	r1, _ := k.do("POST", "/api/v1/applications/"+a.ID+"/pay", nil, map[string]string{"Idempotency-Key": key})
	r2, _ := k.do("POST", "/api/v1/applications/"+a.ID+"/pay", nil, map[string]string{"Idempotency-Key": key})
	_ = r2.json(&a)
	r, _ = k.do("GET", "/api/v1/me", nil, nil)
	_ = r.json(&me)
	k.check(r1.code == 200 && r2.hdr.Get("Idempotent-Replayed") == "true" && len(a.Payments) == 1 && afterIssue-me.Balance == pay,
		f("платёж %d ₽ списан один раз, хотя запрос отправлен дважды (как при обрыве сети)", pay),
		f("оплата: коды %d/%d, платежей %d, списано %d при платеже %d", r1.code, r2.code, len(a.Payments), afterIssue-me.Balance, pay))
}

func (k *checker) bankSide(a app) {
	fmt.Println("\nСторона банка")
	r, _ := k.do("GET", "/api/v1/bank/applications?q="+url.QueryEscape(a.Number), nil, map[string]string{"X-Bank-Token": *token})
	var rows []app
	_ = r.json(&rows)
	k.check(r.code == 200 && len(rows) == 1 && rows[0].ID == a.ID, f("кабинет банка видит заявку %s (поиск по номеру)", a.Number),
		f("кабинет банка: %d, найдено %d", r.code, len(rows)))
	r, _ = k.do("GET", "/api/v1/bank/applications/"+a.ID+"/audit", nil, map[string]string{"X-Bank-Token": *token})
	if r.code == http.StatusNotImplemented {
		k.ok("журнал аудита — только в режиме PostgreSQL (сейчас файловое хранилище)")
	} else {
		var entries []struct {
			Action string `json:"action"`
		}
		_ = r.json(&entries)
		acts := make([]string, 0, len(entries))
		for _, e := range entries {
			acts = append(acts, e.Action)
		}
		k.check(r.code == 200 && len(entries) >= 3, f("журнал аудита: %s", strings.Join(acts, ", ")), f("аудит → %d, записей %d", r.code, len(entries)))
	}
	r, _ = k.do("GET", "/api/v1/bank/applications", nil, nil)
	k.check(r.code == 403, "клиенту кабинет банка закрыт (403)", f("кабинет банка с сессией клиента → %d", r.code))

	// сотрудник банка входит своей учётной записью — отдельная сессия, токен не нужен
	jar, _ := cookiejar.New(nil)
	staff := &checker{c: &http.Client{Jar: jar, Timeout: k.c.Timeout}}
	r, _ = staff.do("GET", "/api/v1/bank/applications", nil, nil)
	k.check(r.code == 401, "без входа и без X-Bank-Token кабинет банка закрыт (401)", f("кабинет банка без входа → %d", r.code))
	r, _ = staff.do("POST", "/api/v1/auth/login", map[string]string{"phone": *staffPhone, "password": *pass}, nil)
	var me struct {
		Role string `json:"role"`
	}
	_ = r.json(&me)
	if !k.check(r.code == 200 && me.Role == "employee", "вход сотрудника банка (роль employee)", f("вход сотрудника → %d, роль %q", r.code, me.Role)) {
		return
	}
	r, _ = staff.do("GET", "/api/v1/bank/applications?q="+url.QueryEscape(a.Number), nil, nil)
	rows = nil
	_ = r.json(&rows)
	k.check(r.code == 200 && len(rows) == 1, "сотрудник видит заявку в очереди по своей сессии", f("очередь сотрудника → %d, найдено %d", r.code, len(rows)))
	r, _ = staff.do("GET", "/api/v1/applications", nil, nil)
	k.check(r.code == 403, "сотрудник не действует от имени клиента (403)", f("заявки клиента сотруднику → %d", r.code))
}

func (k *checker) api() {
	fmt.Println("\nПоведение API")
	r, _ := k.do("GET", "/api/v1/no-such-endpoint", nil, nil)
	var e struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	_ = r.json(&e)
	k.check(r.code == 404 && e.Error.RequestID != "" && r.hdr.Get("X-Request-ID") == e.Error.RequestID,
		f("неизвестный путь → JSON 404 с request_id (%s)", e.Error.RequestID), f("неизвестный путь → %d %s", r.code, r.body))
	r, _ = k.do("GET", "/api/v1/applications", nil, nil)
	k.check(r.code == 200 && r.hdr.Get("X-Total-Count") != "", f("список заявок с пагинацией (X-Total-Count: %s)", r.hdr.Get("X-Total-Count")), f("список заявок → %d", r.code))
	anon := &checker{c: &http.Client{Timeout: 5 * time.Second}}
	r, _ = anon.do("POST", "/api/v1/applications", map[string]string{"product_id": "business-dev"}, nil)
	k.check(r.code == 401, "без входа оформить нельзя (401)", f("создание без входа → %d", r.code))
	r, _ = k.do("GET", "/api/v1/products", nil, nil)
	k.check(r.hdr.Get("Cache-Control") != "" && r.hdr.Get("ETag") != "", f("справочник продуктов кэшируется (%s)", r.hdr.Get("Cache-Control")), "у продуктов нет Cache-Control/ETag")

	if *metrics == "" {
		return
	}
	r, err := k.do("GET", *metrics, nil, nil)
	if err != nil || r.code != 200 {
		k.fail("метрики %s недоступны: %v", *metrics, err)
		return
	}
	txt := string(r.body)
	for _, m := range []string{"http_requests_total", "http_request_duration_seconds", "events_published_total", "outbox_pending_events", "cache_requests_total"} {
		k.check(strings.Contains(txt, m), f("метрика %s есть", m), f("нет метрики %s", m))
	}
	k.check(strings.Contains(txt, `route="POST /api/v1/applications/{id}/submit"`), "метрики размечены шаблоном маршрута", "в метриках нет шаблонов маршрутов")
}

// ---------- фазы ----------

func (k *checker) phaseAll() error {
	bus, err := k.infra()
	if err != nil {
		return err
	}
	fmt.Println("\nКлиент: от калькулятора до решения")
	if err := k.login(); err != nil {
		return err
	}
	a, err := k.newApp()
	if err != nil {
		return err
	}
	if a, err = k.await(a); err != nil {
		return err
	}
	if bus == "kafka" {
		k.ok("решение принял отдельный воркер по событию из Kafka (outbox → топик → consumer group)")
	}
	k.loan(a)
	k.bankSide(a)
	k.api()
	return nil
}

// phaseSubmit — подать заявку и вывести номер (для проверки при остановленной Kafka).
func (k *checker) phaseSubmit() error {
	if err := k.login(); err != nil {
		return err
	}
	a, err := k.newApp()
	if err != nil {
		return err
	}
	r, _ := k.do("GET", "/readyz", nil, nil)
	k.ok("/readyz → %d %s", r.code, strings.TrimSpace(string(r.body)))
	fmt.Println("NUMBER=" + a.Number)
	return nil
}

// phaseAwait — дождаться решения по номеру (через кабинет банка, без сессии клиента).
func (k *checker) phaseAwait() error {
	if *number == "" {
		return errors.New("нужен -number")
	}
	start := time.Now()
	last := ""
	for time.Since(start) < *wait {
		r, err := k.do("GET", "/api/v1/bank/applications?q="+url.QueryEscape(*number), nil, map[string]string{"X-Bank-Token": *token})
		if err == nil && r.code == 200 {
			var rows []app
			_ = r.json(&rows)
			if len(rows) == 1 {
				last = rows[0].Status
				if decided[last] {
					k.ok("заявка %s дошла до решения (%s) через %.1f с после запуска Kafka — событие не потерялось",
						*number, last, time.Since(start).Seconds())
					return nil
				}
			}
		}
		time.Sleep(time.Second)
	}
	k.fail("заявка %s так и осталась в статусе %q", *number, last)
	return nil
}

func fmtChecks(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ", ")
}
