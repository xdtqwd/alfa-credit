// Package domain содержит сущности кредитного конвейера и правила их изменения.
// Пакет не зависит от инфраструктуры (БД, HTTP), поэтому его легко покрыть тестами
// и вынести в отдельный сервис при переходе от модульного монолита к микросервисам.
package domain

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrCompanyNotFound   = errors.New("company not found in registry")
)

// ValidationError — ошибка входных данных с привязкой к полю анкеты.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// Status — состояние заявки в кредитном конвейере.
type Status string

const (
	StatusDraft        Status = "draft"         // черновик быстрой анкеты прошлой версии: теперь 1-й уровень живёт без заявки
	StatusFullForm     Status = "full_form"     // вход через Госуслуги выполнен, клиент прикладывает документы (2-й уровень)
	StatusSubmitted    Status = "submitted"     // 2-й уровень отправлен, ждёт обработки
	StatusDataCheck    Status = "data_check"    // проверка данных: ЕГРЮЛ/ЕГРИП, БКИ, паспорт, протокол, поручитель
	StatusScoring      Status = "scoring"       // полный скоринг и окончательное решение
	StatusApproved     Status = "approved"      // одобрено на запрошенных условиях
	StatusCounterOffer Status = "counter_offer" // одобрено на иных условиях
	StatusNeedInfo     Status = "need_info"     // нужно уточнить документы или обеспечение
	StatusRejected     Status = "rejected"      // отказ
	StatusSigning      Status = "signing"       // клиент принял условия, подписание документов
	StatusIssued       Status = "issued"        // средства выданы (симуляция)
	StatusCancelled    Status = "cancelled"     // отменена клиентом
	StatusClosed       Status = "closed"        // кредит полностью погашен
)

var transitions = map[Status][]Status{
	StatusDraft:        {StatusCancelled},
	StatusFullForm:     {StatusSubmitted, StatusCancelled},
	StatusSubmitted:    {StatusDataCheck, StatusCancelled},
	StatusDataCheck:    {StatusScoring, StatusNeedInfo, StatusRejected},
	StatusScoring:      {StatusApproved, StatusCounterOffer, StatusNeedInfo, StatusRejected},
	StatusApproved:     {StatusSigning, StatusCancelled},
	StatusCounterOffer: {StatusSigning, StatusCancelled},
	StatusNeedInfo:     {StatusSubmitted, StatusCancelled},
	StatusSigning:      {StatusIssued, StatusCancelled},
	StatusIssued:       {StatusClosed},
}

// KnownStatus — существует ли такой статус (валидация фильтров API).
func KnownStatus(s Status) bool {
	if s == StatusClosed || s == StatusRejected || s == StatusCancelled {
		return true
	}
	_, ok := transitions[s]
	return ok
}

// CanTransition проверяет переход по диаграмме состояний (docs/ARCHITECTURE.md).
func CanTransition(from, to Status) bool {
	return slices.Contains(transitions[from], to)
}

// InPipeline — заявка обрабатывается банком автоматически.
func (s Status) InPipeline() bool {
	return s == StatusSubmitted || s == StatusDataCheck || s == StatusScoring
}

type Company struct {
	INN             string `json:"inn"`
	OGRN            string `json:"ogrn"`
	Type            string `json:"type"` // ООО | ИП
	Name            string `json:"name"`
	OKVED           string `json:"okved"`
	OKVEDName       string `json:"okved_name"`
	Address         string `json:"address"`
	Director        string `json:"director"`
	RegDate         string `json:"reg_date"` // YYYY-MM-DD
	Status          string `json:"status"`   // active | liquidated
	RevenueLastYear int64  `json:"revenue_last_year"`
	Source          string `json:"source"`
}

type Financials struct {
	AnnualRevenue      int64 `json:"annual_revenue"`
	MonthlyObligations int64 `json:"monthly_obligations"`
	Employees          int   `json:"employees"`
}

type Contact struct {
	FullName string `json:"full_name"`
	Position string `json:"position"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
}

type BureauReport struct {
	Score          int    `json:"score"`
	ActiveLoans    int    `json:"active_loans"`
	HasOverdue     bool   `json:"has_overdue"`
	MaxOverdueDays int    `json:"max_overdue_days"`
	Source         string `json:"source"`
}

type Check struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Result  string `json:"result"` // ok | warn | fail
	Details string `json:"details"`
}

type DecisionType string

const (
	DecisionApproved DecisionType = "approved"
	DecisionCounter  DecisionType = "counter_offer"
	DecisionNeedInfo DecisionType = "need_info"
	DecisionRejected DecisionType = "rejected"
)

type Decision struct {
	Type           DecisionType `json:"type"`
	Amount         int64        `json:"amount,omitempty"`
	TermMonths     int          `json:"term_months,omitempty"`
	Rate           float64      `json:"rate,omitempty"`
	MonthlyPayment int64        `json:"monthly_payment,omitempty"`
	Total          int64        `json:"total,omitempty"`
	Overpayment    int64        `json:"overpayment,omitempty"`
	// Requested — расчёт по тому, что просил клиент (для сравнения «было/стало»).
	Requested *Quote `json:"requested,omitempty"`
	// Reasons — объяснение простыми словами на данных анкеты.
	Reasons []string `json:"reasons"`
	// Advice — что можно сделать дальше (для отказа и уточнения).
	Advice []string `json:"advice,omitempty"`
	// Final — окончательное решение (полный скоринг). false — предварительный расчёт 1-го уровня.
	Final bool `json:"final"`
}

// SetOffer заполняет условия решения из единого расчёта.
func (d *Decision) SetOffer(q Quote) {
	d.Amount, d.TermMonths, d.Rate = q.Amount, q.TermMonths, q.Rate
	d.MonthlyPayment, d.Total, d.Overpayment = q.MonthlyPayment, q.Total, q.Overpayment
}

// ScoreFactor — один фактор балльной модели: сколько баллов дал и почему (простыми словами).
type ScoreFactor struct {
	Name    string `json:"name"`
	Impact  int    `json:"impact"`
	Explain string `json:"explain"`
}

type Event struct {
	Status  Status    `json:"status"`
	At      time.Time `json:"at"`
	Actor   string    `json:"actor"` // client | system | bank
	Comment string    `json:"comment"`
}

type Application struct {
	ID         string     `json:"id"`
	Number     string     `json:"number"`
	ClientID   string     `json:"-"`
	ProductID  string     `json:"product_id"`
	Amount     int64      `json:"amount"`
	TermMonths int        `json:"term_months"`
	Purpose    string     `json:"purpose"`
	INN        string     `json:"inn"`
	Company    *Company   `json:"company,omitempty"`
	Financials Financials `json:"financials"`
	Contact    Contact    `json:"contact"`
	Consent    bool       `json:"consent"`
	// Level — уровень анкеты: 2 у заявок новой схемы (заявка создаётся после входа через Госуслуги).
	Level int       `json:"level"`
	Full  *FullForm `json:"full_form,omitempty"`
	// Identity — личность заявителя из Госуслуг (ЕСИА) на момент подачи.
	Identity *Identity `json:"identity,omitempty"`
	// Preliminary — предварительное решение 1-го уровня (упрощённый скоринг без кредитной истории).
	Preliminary *Decision     `json:"preliminary,omitempty"`
	Status      Status        `json:"status"`
	Bureau      *BureauReport `json:"bureau,omitempty"`
	Checks      []Check       `json:"checks"`
	Score       *int          `json:"score,omitempty"`
	Factors     []ScoreFactor `json:"factors,omitempty"` // из чего сложился балл — для клиента и банка
	Decision    *Decision     `json:"decision,omitempty"`
	History     []Event       `json:"history"`
	Payments    []Payment     `json:"payments"`
	// Вычисляемые поля (Enrich) — считаются на сервере, клиент их только показывает.
	Quote           *Quote        `json:"quote,omitempty"`
	Schedule        []Installment `json:"schedule,omitempty"`
	NextPayment     int64         `json:"next_payment,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	StatusChangedAt time.Time     `json:"status_changed_at"`
}

// Transition переводит заявку в новый статус с записью в историю (аудит).
func (a *Application) Transition(to Status, actor, comment string, now time.Time) error {
	if !CanTransition(a.Status, to) {
		return ErrInvalidTransition
	}
	a.Status = to
	a.UpdatedAt = now
	a.StatusChangedAt = now
	a.History = append(a.History, Event{Status: to, At: now, Actor: actor, Comment: comment})
	return nil
}

// NextPaymentAmount — сумма очередного платежа по графику (последний платёж может отличаться).
func (a *Application) NextPaymentAmount() int64 {
	if a.Decision == nil {
		return 0
	}
	s := Schedule(a.Amount, a.Decision.Rate, a.TermMonths)
	if len(a.Payments) >= len(s) {
		return 0
	}
	return s[len(a.Payments)].Payment
}

// Enrich заполняет вычисляемые поля перед отдачей клиенту.
func (a *Application) Enrich() *Application {
	if p, ok := FindProduct(a.ProductID); ok && a.Amount > 0 && a.TermMonths > 0 {
		q := NewQuote(a.Amount, p.Rate, a.TermMonths)
		a.Quote = &q
	}
	a.Schedule, a.NextPayment = nil, 0
	if a.Decision != nil && (a.Status == StatusIssued || a.Status == StatusClosed) {
		a.Schedule = Schedule(a.Amount, a.Decision.Rate, a.TermMonths)
		a.NextPayment = a.NextPaymentAmount()
	}
	return a
}

// DraftPatch — автосохранение 2-го уровня анкеты: условия кредита и данные 1-го уровня уже зафиксированы.
type DraftPatch struct {
	FullForm *FullForm `json:"full_form"`
}

func (a *Application) ApplyPatch(p DraftPatch, now time.Time) error {
	if !a.EditableFull() {
		return ErrInvalidTransition
	}
	if p.FullForm != nil {
		f := *p.FullForm
		if err := validFullForm(&f); err != nil {
			return err
		}
		a.Full = &f
	}
	a.UpdatedAt = now
	return nil
}

// Level1 — данные 1-го уровня: по ним считается предварительный расчёт (без входа),
// а после входа через Госуслуги создаётся заявка.
type Level1 struct {
	ProductID  string     `json:"product_id"`
	Amount     int64      `json:"amount"`
	TermMonths int        `json:"term_months"`
	Purpose    string     `json:"purpose"`
	INN        string     `json:"inn"`
	Financials Financials `json:"financials"`
}

// Validate проверяет 1-й уровень и подставляет цель по умолчанию.
func (in *Level1) Validate() (Product, error) {
	p, ok := FindProduct(in.ProductID)
	switch {
	case !ok:
		return p, &ValidationError{"product_id", "выберите кредит"}
	case in.Amount < p.MinAmount || in.Amount > p.MaxAmount:
		return p, &ValidationError{"amount", fmt.Sprintf("сумма — от %s до %s", Money(p.MinAmount), Money(p.MaxAmount))}
	case in.TermMonths < p.MinTerm || in.TermMonths > p.MaxTerm:
		return p, &ValidationError{"term_months", "срок вне диапазона продукта"}
	case !ValidINN(in.INN):
		return p, &ValidationError{"inn", "ИНН должен содержать 10 или 12 цифр"}
	case in.Financials.AnnualRevenue <= 0:
		return p, &ValidationError{"financials.annual_revenue", "укажите выручку за год"}
	case in.Financials.MonthlyObligations < 0:
		return p, &ValidationError{"financials.monthly_obligations", "платежи не могут быть отрицательными"}
	}
	if in.Purpose == "" || !slices.Contains(p.Purposes, in.Purpose) {
		in.Purpose = p.Purposes[0]
	}
	return p, nil
}

func ValidINN(inn string) bool {
	if len(inn) != 10 && len(inn) != 12 {
		return false
	}
	for _, r := range inn {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
