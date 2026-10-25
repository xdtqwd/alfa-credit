package domain

import (
	"errors"
	"testing"
)

var testIdentity = &Identity{FullName: "Смирнов Алексей Викторович",
	Passport: Passport{Series: "4512", Number: "345678", IssuedAt: "2016-05-24", IssuedBy: "ГУ МВД России по г. Москве", DivisionCode: "770-045"}}

func fullApp(kind string, amount int64, f FullForm) *Application {
	return &Application{ProductID: "business-dev", Amount: amount, TermMonths: 24, Level: 2, Status: StatusFullForm, Full: &f,
		Company: &Company{Type: kind, Director: "Смирнов Алексей Викторович"}, Identity: testIdentity}
}

func TestValidateFullRequiresSecurityForBigLoans(t *testing.T) {
	base := FullForm{Collateral: Collateral{Type: CollateralNone}, Consent: true}
	if err := fullApp("ИП", 3_000_000, base).ValidateFull(t0); err != nil {
		t.Fatalf("small loan needs no security: %v", err)
	}
	var ve *ValidationError
	if err := fullApp("ИП", 8_000_000, base).ValidateFull(t0); !errors.As(err, &ve) || ve.Field != "full_form.collateral.type" {
		t.Fatalf("big loan without security: %v", err)
	}
	withG := base
	withG.Guarantor = &Guarantor{FullName: "Иванов Иван", INN: "770123456781"}
	if err := fullApp("ИП", 8_000_000, withG).ValidateFull(t0); err != nil {
		t.Fatalf("guarantor is enough to submit: %v", err)
	}
	withG.Guarantor = &Guarantor{FullName: "Иванов Иван", INN: "7701234567"}
	if err := fullApp("ИП", 8_000_000, withG).ValidateFull(t0); !errors.As(err, &ve) || ve.Field != "full_form.guarantor.inn" {
		t.Fatalf("guarantor INN must have 12 digits: %v", err)
	}
	noConsent := base
	noConsent.Consent = false
	if err := fullApp("ИП", 1_000_000, noConsent).ValidateFull(t0); !errors.As(err, &ve) || ve.Field != "full_form.consent" {
		t.Fatalf("consent required: %v", err)
	}
	noID := fullApp("ИП", 1_000_000, base)
	noID.Identity = nil
	if err := noID.ValidateFull(t0); !errors.As(err, &ve) || ve.Field != "identity" {
		t.Fatalf("Gosuslugi identity required: %v", err)
	}
}

// ООО: паспорт руководителя всегда, протокол — только для крупной сделки (больше ProtocolFrom).
func TestValidateFullLLCDocuments(t *testing.T) {
	var ve *ValidationError
	base := FullForm{Collateral: Collateral{Type: CollateralNone}, Consent: true}
	if err := fullApp("ООО", 3_000_000, base).ValidateFull(t0); !errors.As(err, &ve) || ve.Field != "full_form.director_passport.series" {
		t.Fatalf("LLC needs director passport: %v", err)
	}
	withPass := base
	p := testIdentity.Passport
	withPass.DirectorPassport = &p
	if err := fullApp("ООО", 3_000_000, withPass).ValidateFull(t0); err != nil {
		t.Fatalf("LLC with passport: %v", err)
	}
	big := withPass
	big.Collateral = Collateral{Type: CollateralRealty, Value: 50_000_000}
	if err := fullApp("ООО", 26_000_000, big).ValidateFull(t0); !errors.As(err, &ve) || ve.Field != "full_form.protocol" {
		t.Fatalf("big LLC loan needs protocol: %v", err)
	}
	big.Protocol = &Document{Name: "protocol.docx", Size: 1000}
	if err := fullApp("ООО", 26_000_000, big).ValidateFull(t0); !errors.As(err, &ve) || ve.Field != "full_form.protocol" {
		t.Fatalf("protocol must be PDF/JPG/PNG: %v", err)
	}
	big.Protocol = &Document{Name: "Протокол №3.pdf", Size: 1000}
	if err := fullApp("ООО", 26_000_000, big).ValidateFull(t0); err != nil {
		t.Fatalf("big LLC loan with protocol: %v", err)
	}
	if err := fullApp("ИП", 26_000_000, FullForm{Collateral: big.Collateral, Consent: true}).ValidateFull(t0); err != nil {
		t.Fatalf("sole proprietor needs neither passport block nor protocol: %v", err)
	}
}

// После входа через Госуслуги паспорт руководителя подставляется, если в анкете руководитель сам.
func TestStartLevel2PrefillsDirectorPassport(t *testing.T) {
	pre := Decision{Type: DecisionCounter, Amount: 2_000_000, TermMonths: 36, Rate: 21.4, Final: true}
	a := &Application{Amount: 3_000_000, TermMonths: 24, Company: &Company{Type: "ООО", Director: "Смирнов Алексей Викторович"}}
	a.StartLevel2(pre, testIdentity)
	if a.Level != 2 || a.Preliminary == nil || a.Preliminary.Final || a.Amount != 2_000_000 || a.TermMonths != 36 {
		t.Fatalf("preliminary terms must be fixed: %+v", a)
	}
	if a.Full == nil || a.Full.DirectorPassport == nil || a.Full.DirectorPassport.Number != "345678" || a.Full.Collateral.Type != CollateralNone {
		t.Fatalf("director passport must be prefilled from Gosuslugi: %+v", a.Full)
	}
	other := &Application{Company: &Company{Type: "ООО", Director: "Кто-то Другой"}}
	other.StartLevel2(pre, testIdentity)
	if other.Full.DirectorPassport != nil {
		t.Fatal("someone else's passport must not be prefilled")
	}
	if got := (Collateral{Type: CollateralRealty, Value: 10_000_000}).LiquidValue(); got != 7_000_000 {
		t.Fatalf("realty is taken at 70%%: %d", got)
	}
}
