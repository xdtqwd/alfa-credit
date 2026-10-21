package integrations

import (
	"context"
	"errors"
	"testing"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

func TestRegistry(t *testing.T) {
	r := MockRegistry{}
	c, err := r.Lookup(context.Background(), "7701234567")
	if err != nil || c.Name != "ООО «Северный ветер»" || c.Status != "active" || c.INN != "7701234567" {
		t.Fatalf("fixture: %+v %v", c, err)
	}
	if c, _ := r.Lookup(context.Background(), "6600000000"); c.Status != "liquidated" {
		t.Fatal("liquidated fixture")
	}
	if _, err := r.Lookup(context.Background(), "0000111122"); !errors.Is(err, domain.ErrCompanyNotFound) {
		t.Fatal("0000… must be not found")
	}
	var ve *domain.ValidationError
	if _, err := r.Lookup(context.Background(), "12x"); !errors.As(err, &ve) {
		t.Fatal("invalid INN must be validation error")
	}
	a, _ := r.Lookup(context.Background(), "5555555555")
	b, _ := r.Lookup(context.Background(), "5555555555")
	if a.Name != b.Name || a.RevenueLastYear != b.RevenueLastYear || a.Type != "ООО" {
		t.Fatal("generated companies must be deterministic")
	}
	if ip, _ := r.Lookup(context.Background(), "555555555555"); ip.Type != "ИП" {
		t.Fatal("12 digits must be ИП")
	}
}

func TestBureau(t *testing.T) {
	b := MockBureau{}
	bad, _ := b.Report(context.Background(), "5401000009")
	if !bad.HasOverdue {
		t.Fatal("INN ending with 9 must have overdue")
	}
	good, _ := b.Report(context.Background(), "7701234567")
	if good.HasOverdue || good.Score < 700 {
		t.Fatalf("good report: %+v", good)
	}
}
