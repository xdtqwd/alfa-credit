package domain

import (
	"strings"
	"time"
)

// Passport — паспорт гражданина РФ: из Госуслуг (ЕСИА) или введённый в анкете (паспорт руководителя ООО).
type Passport struct {
	Series       string `json:"series"`        // 4 цифры
	Number       string `json:"number"`        // 6 цифр
	IssuedAt     string `json:"issued_at"`     // YYYY-MM-DD
	IssuedBy     string `json:"issued_by"`     // кем выдан
	DivisionCode string `json:"division_code"` // код подразделения, 000-000
}

// Identity — личность, подтверждённая через ЕСИА (Госуслуги). В MVP — мок с демо-людьми.
type Identity struct {
	FullName   string    `json:"full_name"`
	BirthDate  string    `json:"birth_date"` // YYYY-MM-DD
	SNILS      string    `json:"snils"`      // 000-000-000 00
	Passport   Passport  `json:"passport"`
	Source     string    `json:"source"`
	VerifiedAt time.Time `json:"verified_at"`
}

func digitsOnly(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Validate проверяет реквизиты паспорта; field — префикс поля для подсветки в анкете.
func (p Passport) Validate(field string, now time.Time) error {
	code := strings.ReplaceAll(p.DivisionCode, "-", "")
	issued, err := time.Parse("2006-01-02", p.IssuedAt)
	switch {
	case len(p.Series) != 4 || !digitsOnly(p.Series):
		return &ValidationError{field + ".series", "серия паспорта — 4 цифры"}
	case len(p.Number) != 6 || !digitsOnly(p.Number):
		return &ValidationError{field + ".number", "номер паспорта — 6 цифр"}
	case err != nil || issued.After(now) || issued.Year() < 1997:
		return &ValidationError{field + ".issued_at", "укажите дату выдачи паспорта"}
	case len(code) != 6 || !digitsOnly(code):
		return &ValidationError{field + ".division_code", "код подразделения — 6 цифр, например 540-001"}
	case len([]rune(strings.TrimSpace(p.IssuedBy))) < 5:
		return &ValidationError{field + ".issued_by", "укажите, кем выдан паспорт"}
	}
	return nil
}

// Masked — серия и номер с закрытой серединой: «45 12 ••••56».
func (p Passport) Masked() string {
	if len(p.Series) != 4 || len(p.Number) != 6 {
		return "—"
	}
	return p.Series[:2] + " " + p.Series[2:] + " ••••" + p.Number[4:]
}
