package domain

import (
	"errors"
	"testing"
	"time"
)

func fullApp(amount int64, f FullForm) *Application {
	return &Application{ProductID: "business-dev", Amount: amount, TermMonths: 24, Level: 2, Status: StatusFullForm, Full: &f}
}

func TestValidateFullRequiresSecurityForBigLoans(t *testing.T) {
	base := FullForm{Employees: 5, Premises: "rent", PurposeDetails: "Закупка оборудования для цеха", Collateral: Collateral{Type: CollateralNone}, StatementsConsent: true}
	if err := fullApp(3_000_000, base).ValidateFull(); err != nil {
		t.Fatalf("small loan needs no security: %v", err)
	}
	var ve *ValidationError
	if err := fullApp(8_000_000, base).ValidateFull(); !errors.As(err, &ve) || ve.Field != "full_form.collateral.type" {
		t.Fatalf("big loan without security: %v", err)
	}
	withG := base
	withG.Guarantor = &Guarantor{FullName: "Иванов Иван", INN: "770123456781"}
	if err := fullApp(8_000_000, withG).ValidateFull(); err != nil {
		t.Fatalf("guarantor is enough to submit: %v", err)
	}
	withG.Guarantor = &Guarantor{FullName: "Иванов Иван", INN: "7701234567"}
	if err := fullApp(8_000_000, withG).ValidateFull(); !errors.As(err, &ve) || ve.Field != "full_form.guarantor.inn" {
		t.Fatalf("guarantor INN must have 12 digits: %v", err)
	}
	noConsent := base
	noConsent.StatementsConsent = false
	if err := fullApp(1_000_000, noConsent).ValidateFull(); !errors.As(err, &ve) || ve.Field != "full_form.statements_consent" {
		t.Fatalf("consent required: %v", err)
	}
}

func TestPatchLevelsAreSeparate(t *testing.T) {
	now := time.Now()
	a := fullApp(3_000_000, FullForm{})
	amount := int64(1)
	if err := a.ApplyPatch(DraftPatch{Amount: &amount}, now); err == nil {
		t.Fatal("level-1 fields must be locked on level 2")
	}
	f := FullForm{Employees: 3, Collateral: Collateral{Type: "gold"}}
	if err := a.ApplyPatch(DraftPatch{FullForm: &f}, now); err == nil {
		t.Fatal("unknown collateral type must be rejected")
	}
	draft := &Application{Status: StatusDraft}
	if err := draft.ApplyPatch(DraftPatch{FullForm: &FullForm{}}, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("full form cannot be filled before a preliminary decision")
	}
	if got := (Collateral{Type: CollateralRealty, Value: 10_000_000}).LiquidValue(); got != 7_000_000 {
		t.Fatalf("realty is taken at 70%%: %d", got)
	}
}
