package domain

import (
	"fmt"
	"strings"
	"time"
)

// Двухуровневая анкета.
//
//	Уровень 1 — предварительный расчёт, без входа: кредит, ИНН компании, выручка и текущие платежи.
//	            Упрощённый скоринг (реестр, ФНС, долговая нагрузка — без кредитной истории) сразу даёт
//	            ПРЕДВАРИТЕЛЬНОЕ решение. На сервере заявка ещё не создаётся.
//	Уровень 2 — идентификация через ЕСИА (Госуслуги): это и есть вход. Заявка создаётся в кабинете,
//	            клиент прикладывает документы: паспорт руководителя (ООО), протокол об одобрении крупной
//	            сделки (ООО, больше ProtocolFrom), обеспечение (больше SecurityRequiredFrom) и согласие
//	            на запрос в БКИ. Полный скоринг — с кредитной историей — даёт ОКОНЧАТЕЛЬНОЕ решение.

// SecurityRequiredFrom — с какой суммы кредита нужно обеспечение: залог или поручительство.
const SecurityRequiredFrom int64 = 5_000_000

// ProtocolFrom — с какой суммы кредит для ООО — крупная сделка: нужен протокол (решение) участников о её одобрении.
const ProtocolFrom int64 = 25_000_000

// MaxDocumentSize — предельный размер файла, который принимает анкета.
const MaxDocumentSize int64 = 20 << 20

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

// SecurityRules — пороги и справочники 2-го уровня анкеты (единый источник для интерфейса).
type SecurityRules struct {
	RequiredFrom int64            `json:"required_from"`
	ProtocolFrom int64            `json:"protocol_from"`
	Kinds        []CollateralKind `json:"kinds"`
}

func Security() SecurityRules {
	r := SecurityRules{RequiredFrom: SecurityRequiredFrom, ProtocolFrom: ProtocolFrom}
	for _, t := range []string{CollateralRealty, CollateralVehicle, CollateralEquipment, CollateralDeposit} {
		k := collateralKinds[t]
		r.Kinds = append(r.Kinds, CollateralKind{Type: t, Name: k.Name, Discount: k.Discount})
	}
	return r
}

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

// Document — приложенный файл. В демо файл остаётся у клиента: сохраняются только имя и размер.
type Document struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// FullForm — 2-й уровень анкеты: документы, обеспечение и согласие.
type FullForm struct {
	DirectorPassport *Passport  `json:"director_passport,omitempty"` // паспорт руководителя — только для ООО
	Protocol         *Document  `json:"protocol,omitempty"`          // ООО и сумма больше ProtocolFrom
	Collateral       Collateral `json:"collateral"`
	Guarantor        *Guarantor `json:"guarantor,omitempty"`
	Consent          bool       `json:"consent"` // согласие на запрос кредитной истории в БКИ и выписок
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

// IsLLC — заявка от ООО (а не ИП): для неё нужны паспорт руководителя и, для крупной суммы, протокол.
func (a *Application) IsLLC() bool { return a.Company != nil && a.Company.Type != "ИП" }

// NeedsProtocol — кредит для ООО больше ProtocolFrom: крупная сделка.
func (a *Application) NeedsProtocol() bool { return a.IsLLC() && a.Amount > ProtocolFrom }

// EditableFull — можно править 2-й уровень анкеты.
func (a *Application) EditableFull() bool {
	return a.Status == StatusFullForm || (a.Status == StatusNeedInfo && a.Level == 2)
}

// StartLevel2 — заявка создаётся после входа через Госуслуги: фиксируем предварительное решение
// и заранее подставляем то, что банк уже знает (паспорт руководителя — если в анкете он сам).
func (a *Application) StartLevel2(pre Decision, id *Identity) {
	pre.Final = false
	a.Preliminary = &pre
	if pre.Amount > 0 && pre.TermMonths > 0 {
		a.Amount, a.TermMonths = pre.Amount, pre.TermMonths
	}
	a.Level, a.Identity = 2, id
	a.Full = &FullForm{Collateral: Collateral{Type: CollateralNone}}
	if a.IsLLC() && id != nil && a.Company.Director != "" && strings.EqualFold(id.FullName, a.Company.Director) {
		p := id.Passport
		a.Full.DirectorPassport = &p
	}
}

func validFullForm(f *FullForm) error {
	if f.Collateral.Value < 0 {
		return &ValidationError{"full_form", "значения не могут быть отрицательными"}
	}
	if f.Collateral.Type == "" {
		f.Collateral.Type = CollateralNone
	}
	if _, ok := collateralKinds[f.Collateral.Type]; !ok && f.Collateral.Type != CollateralNone {
		return &ValidationError{"full_form.collateral.type", "неизвестный вид залога"}
	}
	if d := f.Protocol; d != nil && (d.Size < 0 || d.Size > MaxDocumentSize) {
		return &ValidationError{"full_form.protocol", fmt.Sprintf("файл протокола — до %d МБ", MaxDocumentSize>>20)}
	}
	return nil
}

// documentExt — какие файлы принимаем как скан документа.
var documentExt = []string{".pdf", ".jpg", ".jpeg", ".png"}

// ValidateFull — 2-й уровень готов к полному скорингу.
func (a *Application) ValidateFull(now time.Time) error {
	f := a.Full
	if f == nil {
		return &ValidationError{"full_form", "заполните анкету 2-го уровня"}
	}
	if a.Identity == nil {
		return &ValidationError{"identity", "подтвердите личность через Госуслуги"}
	}
	if a.IsLLC() {
		if f.DirectorPassport == nil {
			return &ValidationError{"full_form.director_passport.series", "укажите паспорт руководителя"}
		}
		if err := f.DirectorPassport.Validate("full_form.director_passport", now); err != nil {
			return err
		}
	}
	if a.NeedsProtocol() {
		d := f.Protocol
		if d == nil || strings.TrimSpace(d.Name) == "" || d.Size <= 0 {
			return &ValidationError{"full_form.protocol", fmt.Sprintf("для кредита больше %s приложите протокол или решение участников об одобрении крупной сделки", Money(ProtocolFrom))}
		}
		ok := false
		for _, ext := range documentExt {
			ok = ok || strings.HasSuffix(strings.ToLower(d.Name), ext)
		}
		if !ok {
			return &ValidationError{"full_form.protocol", "протокол — файл PDF, JPG или PNG"}
		}
	}
	if f.Collateral.Type != CollateralNone && f.Collateral.Value <= 0 {
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
	if !f.Consent {
		return &ValidationError{"full_form.consent", "нужно согласие на запрос кредитной истории и выписок"}
	}
	return nil
}
