package domain

import (
	"fmt"
	"strings"
)

// Двухуровневая анкета.
//
//	Уровень 1 — быстрая анкета: сумма, срок, выручка, текущие платежи, контакт, согласие на БКИ.
//	            По ней конвейер проверяет компанию, считает балл и выносит ПРЕДВАРИТЕЛЬНОЕ решение.
//	Уровень 2 — полная анкета: о бизнесе, обеспечение (залог, поручительство), согласие на выписки.
//	            Открывается, только когда клиент принял предварительные условия, — подробности
//	            спрашиваем у того, кому уже готовы дать деньги. По ней банк выносит ОКОНЧАТЕЛЬНОЕ решение.
//
// Продукты с флагом Express (овердрафт) — одноуровневые: решение по быстрой анкете сразу окончательное.

// SecurityRequiredFrom — с какой суммы кредита нужно обеспечение: залог или поручительство.
const SecurityRequiredFrom int64 = 5_000_000

// Виды залога и доля оценочной стоимости, которую банк принимает в зачёт (дисконт на ликвидность).
const (
	CollateralNone      = "none"
	CollateralRealty    = "realty"
	CollateralVehicle   = "vehicle"
	CollateralEquipment = "equipment"
	CollateralDeposit   = "deposit"
)

var collateralKinds = map[string]struct {
	Name     string
	Discount float64 // доля стоимости в зачёт
}{
	CollateralRealty:    {"Недвижимость", 0.7},
	CollateralVehicle:   {"Транспорт и спецтехника", 0.6},
	CollateralEquipment: {"Оборудование", 0.5},
	CollateralDeposit:   {"Депозит в Альфа-Банке", 0.95},
}

// CollateralKind — вид залога для справочника интерфейса.
type CollateralKind struct {
	Type     string  `json:"type"`
	Name     string  `json:"name"`
	Discount float64 `json:"discount"`
}

// SecurityRules — правила обеспечения для полной анкеты (единый источник для интерфейса).
type SecurityRules struct {
	RequiredFrom int64            `json:"required_from"`
	Kinds        []CollateralKind `json:"kinds"`
	Premises     []CollateralKind `json:"premises"` // type + name, discount не используется
}

func Security() SecurityRules {
	r := SecurityRules{RequiredFrom: SecurityRequiredFrom}
	for _, t := range []string{CollateralRealty, CollateralVehicle, CollateralEquipment, CollateralDeposit} {
		k := collateralKinds[t]
		r.Kinds = append(r.Kinds, CollateralKind{Type: t, Name: k.Name, Discount: k.Discount})
	}
	for _, t := range []string{"own", "rent", "none"} {
		r.Premises = append(r.Premises, CollateralKind{Type: t, Name: premisesKinds[t]})
	}
	return r
}

// Помещение, где работает бизнес.
var premisesKinds = map[string]string{"own": "Собственное", "rent": "Аренда", "none": "Без помещения"}

type Collateral struct {
	Type        string `json:"type"`  // none | realty | vehicle | equipment | deposit
	Value       int64  `json:"value"` // оценочная стоимость, ₽
	Description string `json:"description"`
}

// Guarantor — поручитель (физическое лицо, обычно собственник бизнеса).
type Guarantor struct {
	FullName string `json:"full_name"`
	INN      string `json:"inn"` // ИНН физлица, 12 цифр
}

// FullForm — полная анкета (уровень 2).
type FullForm struct {
	Employees         int        `json:"employees"`
	Premises          string     `json:"premises"` // own | rent | none
	PurposeDetails    string     `json:"purpose_details"`
	Collateral        Collateral `json:"collateral"`
	Guarantor         *Guarantor `json:"guarantor,omitempty"`
	StatementsConsent bool       `json:"statements_consent"` // согласие на запрос выписок по счетам и отчётности в ФНС
}

// LiquidValue — сколько стоимости залога банк принимает в зачёт.
func (c Collateral) LiquidValue() int64 {
	k, ok := collateralKinds[c.Type]
	if !ok || c.Value <= 0 {
		return 0
	}
	return int64(float64(c.Value) * k.Discount)
}

// Describe — залог одной строкой для проверок и объяснений.
func (c Collateral) Describe() string {
	k, ok := collateralKinds[c.Type]
	if !ok {
		return "без залога"
	}
	return fmt.Sprintf("%s на %s, в зачёт %s (%.0f%%)", strings.ToLower(k.Name), Money(c.Value), Money(c.LiquidValue()), k.Discount*100)
}

// TwoLevel — проходит ли заявка через полную анкету.
func (a *Application) TwoLevel() bool {
	p, ok := FindProduct(a.ProductID)
	return ok && !p.Express
}

// AtFullForm — заявка на втором уровне (полная анкета заполняется, проверяется или уточняется).
func (a *Application) AtFullForm() bool { return a.Level == 2 }

// EditableQuick — можно править быструю анкету (уровень 1).
func (a *Application) EditableQuick() bool {
	return a.Status == StatusDraft || (a.Status == StatusNeedInfo && !a.AtFullForm())
}

// EditableFull — можно править полную анкету (уровень 2).
func (a *Application) EditableFull() bool {
	return a.Status == StatusFullForm || (a.Status == StatusNeedInfo && a.AtFullForm())
}

// StartFullForm — клиент принял предварительные условия: фиксируем их и открываем полную анкету.
// Поля, которые банк уже знает, подставляются сразу.
func (a *Application) StartFullForm(director string) {
	d := *a.Decision
	a.Preliminary = &d
	if d.Amount > 0 && d.TermMonths > 0 {
		a.Amount, a.TermMonths = d.Amount, d.TermMonths
	}
	a.Level = 2
	if a.Full == nil {
		a.Full = &FullForm{Employees: a.Financials.Employees, Collateral: Collateral{Type: CollateralNone}}
		if a.Amount > SecurityRequiredFrom && director != "" {
			// для крупной суммы предлагаем самый частый вариант — поручительство собственника
			a.Full.Guarantor = &Guarantor{FullName: director}
		}
	}
}

func validFullForm(f *FullForm) error {
	if f.Employees < 0 || f.Collateral.Value < 0 {
		return &ValidationError{"full_form", "значения не могут быть отрицательными"}
	}
	if f.Collateral.Type == "" {
		f.Collateral.Type = CollateralNone
	}
	if _, ok := collateralKinds[f.Collateral.Type]; !ok && f.Collateral.Type != CollateralNone {
		return &ValidationError{"full_form.collateral.type", "неизвестный вид залога"}
	}
	if f.Premises != "" {
		if _, ok := premisesKinds[f.Premises]; !ok {
			return &ValidationError{"full_form.premises", "неизвестный вид помещения"}
		}
	}
	return nil
}

// ValidateFull — полная анкета готова к окончательному решению.
func (a *Application) ValidateFull() error {
	f := a.Full
	switch {
	case f == nil:
		return &ValidationError{"full_form", "заполните полную анкету"}
	case f.Employees <= 0:
		return &ValidationError{"full_form.employees", "укажите число сотрудников, включая руководителя"}
	case f.Premises == "":
		return &ValidationError{"full_form.premises", "укажите, где работает бизнес"}
	case len([]rune(strings.TrimSpace(f.PurposeDetails))) < 20:
		return &ValidationError{"full_form.purpose_details", "опишите, на что пойдут деньги, — хотя бы одно предложение"}
	case f.Collateral.Type != CollateralNone && f.Collateral.Value <= 0:
		return &ValidationError{"full_form.collateral.value", "укажите оценочную стоимость залога"}
	}
	if g := f.Guarantor; g != nil {
		if len([]rune(strings.TrimSpace(g.FullName))) < 5 {
			return &ValidationError{"full_form.guarantor.full_name", "укажите ФИО поручителя"}
		}
		if len(g.INN) != 12 || !ValidINN(g.INN) {
			return &ValidationError{"full_form.guarantor.inn", "ИНН поручителя — 12 цифр"}
		}
	}
	if a.Amount > SecurityRequiredFrom && f.Collateral.Type == CollateralNone && f.Guarantor == nil {
		return &ValidationError{"full_form.collateral.type", fmt.Sprintf("для суммы больше %s нужен залог или поручительство", Money(SecurityRequiredFrom))}
	}
	if !f.StatementsConsent {
		return &ValidationError{"full_form.statements_consent", "нужно согласие на запрос выписок и отчётности"}
	}
	return nil
}
