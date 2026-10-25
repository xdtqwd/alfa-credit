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

// Роли пользователей. Пустая роль у записей, созданных до появления ролей, — это клиент.
const (
	RoleClient   = "client"   // представитель бизнеса: заявки, кредиты, оплата
	RoleEmployee = "employee" // сотрудник банка: очередь заявок, журнал аудита, состояние платформы
)

// Employee — служебная карточка сотрудника банка. У клиента её нет.
type Employee struct {
	PersonnelNo string   `json:"personnel_no"` // табельный номер
	Department  string   `json:"department"`   // подразделение
	Branch      string   `json:"branch"`       // офис / филиал
	Permissions []string `json:"permissions"`  // что доступно в кабинете банка
}

// Права сотрудника в кабинете банка.
const (
	PermViewApplications = "applications.view" // очередь заявок и решения
	PermViewAudit        = "audit.view"        // журнал аудита по заявке
	PermViewPlatform     = "platform.view"     // состояние платформы
)

// User — пользователь кабинета: представитель бизнеса (клиент) или сотрудник банка.
type User struct {
	ID           string     `json:"id"`
	Role         string     `json:"role"`
	Phone        string     `json:"phone"` // только цифры, 11 знаков, начинается с 7
	FullName     string     `json:"full_name"`
	Position     string     `json:"position"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Company      Company    `json:"company"`
	Requisites   Requisites `json:"requisites"`
	Balance      int64      `json:"balance"` // остаток на расчётном счёте, ₽
	Employee     *Employee  `json:"employee,omitempty"`
	// Identity — личность из Госуслуг (ЕСИА): появляется после входа через Госуслуги, нужна для заявки.
	Identity    *Identity `json:"identity,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

// IsEmployee — сотрудник банка. Роль проверяется на сервере при каждом запросе к разделам.
func (u *User) IsEmployee() bool { return u.Role == RoleEmployee }

// Can — есть ли у сотрудника право perm. У клиента прав кабинета банка нет.
func (u *User) Can(perm string) bool {
	if !u.IsEmployee() || u.Employee == nil {
		return false
	}
	for _, p := range u.Employee.Permissions {
		if p == perm {
			return true
		}
	}
	return false
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
