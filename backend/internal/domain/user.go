package domain

import (
	"errors"
	"time"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTooManyAttempts    = errors.New("too many login attempts")
	ErrUserExists         = errors.New("user already exists")
	ErrInsufficientFunds  = errors.New("insufficient funds")
)

// Requisites — банковские реквизиты компании (синтетические).
type Requisites struct {
	Account     string `json:"account"`      // расчётный счёт, 20 цифр
	BankName    string `json:"bank_name"`    // банк
	BIK         string `json:"bik"`          // БИК
	CorrAccount string `json:"corr_account"` // корр. счёт
	KPP         string `json:"kpp,omitempty"`
}

// User — представитель бизнеса, имеющий доступ к кабинету компании.
type User struct {
	ID           string     `json:"id"`
	Phone        string     `json:"phone"` // только цифры, 11 знаков, начинается с 7
	FullName     string     `json:"full_name"`
	Position     string     `json:"position"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Company      Company    `json:"company"`
	Requisites   Requisites `json:"requisites"`
	Balance      int64      `json:"balance"` // остаток на расчётном счёте, ₽
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  time.Time  `json:"last_login_at"`
}

// Credit/Debit — движения по расчётному счёту (выдача кредита, оплата платежа).
func (u *User) Credit(amount int64) { u.Balance += amount }

func (u *User) Debit(amount int64) error {
	if amount > u.Balance {
		return ErrInsufficientFunds
	}
	u.Balance -= amount
	return nil
}

// NormalizePhone оставляет цифры и приводит 8XXXXXXXXXX → 7XXXXXXXXXX.
func NormalizePhone(s string) string {
	d := make([]rune, 0, 11)
	for _, r := range s {
		if r >= '0' && r <= '9' {
			d = append(d, r)
		}
	}
	if len(d) == 10 {
		d = append([]rune{'7'}, d...)
	}
	if len(d) == 11 && d[0] == '8' {
		d[0] = '7'
	}
	return string(d)
}

// Payment — платёж по выданному кредиту.
type Payment struct {
	N      int       `json:"n"`
	At     time.Time `json:"at"`
	Amount int64     `json:"amount"`
}
