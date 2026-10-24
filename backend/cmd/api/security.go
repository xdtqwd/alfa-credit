package main

import (
	"errors"
	"fmt"
	"strings"
)

// Режим запуска. По умолчанию — production: секреты обязательны, cookie только по HTTPS.
// Демо-настройки (публичный токен кабинета банка, cookie без Secure для http://localhost)
// включаются только явно через APP_MODE=demo — так прототип не уедет в облако с ними случайно.
const (
	modeProduction = "production"
	modeDemo       = "demo"

	demoBankToken     = "demo-bank"
	minBankTokenBytes = 16
)

type securityConfig struct {
	Mode         string
	CookieSecure bool
	BankToken    string
	Warnings     []string // что настроено слабее рекомендуемого, но допустимо — пишется в лог при старте
}

// loadSecurity читает APP_MODE, COOKIE_SECURE и BANK_TOKEN и отказывает в запуске с небезопасной
// конфигурацией вне демо-режима. getenv — os.Getenv (параметр — для тестов).
func loadSecurity(getenv func(string) string) (securityConfig, error) {
	mode := strings.ToLower(strings.TrimSpace(getenv("APP_MODE")))
	if mode == "" {
		mode = modeProduction
	}
	if mode != modeProduction && mode != modeDemo {
		return securityConfig{}, fmt.Errorf("APP_MODE=%q: допустимо %s или %s", mode, modeProduction, modeDemo)
	}
	cfg := securityConfig{Mode: mode}

	switch v := strings.ToLower(strings.TrimSpace(getenv("COOKIE_SECURE"))); v {
	case "":
		cfg.CookieSecure = mode == modeProduction
	case "true", "1":
		cfg.CookieSecure = true
	case "false", "0":
		cfg.CookieSecure = false
	default:
		return securityConfig{}, fmt.Errorf("COOKIE_SECURE=%q: допустимо true или false", v)
	}

	cfg.BankToken = getenv("BANK_TOKEN")
	switch {
	case mode == modeDemo && cfg.BankToken == "":
		cfg.BankToken = demoBankToken
		cfg.Warnings = append(cfg.Warnings, "демо-режим: кабинет банка открыт публичным токеном demo-bank — только для локального показа")
	case mode == modeProduction && cfg.BankToken == "":
		return securityConfig{}, errors.New("BANK_TOKEN не задан: задайте секрет кабинета банка (от 16 символов) или запустите с APP_MODE=demo")
	case mode == modeProduction && cfg.BankToken == demoBankToken:
		return securityConfig{}, errors.New("BANK_TOKEN=demo-bank — публичное демо-значение: задайте свой секрет или запустите с APP_MODE=demo")
	case mode == modeProduction && len(cfg.BankToken) < minBankTokenBytes:
		return securityConfig{}, fmt.Errorf("BANK_TOKEN короче %d символов", minBankTokenBytes)
	}

	if mode == modeProduction && !cfg.CookieSecure {
		cfg.Warnings = append(cfg.Warnings, "COOKIE_SECURE=false в production: сессионная cookie уйдёт и по HTTP — допустимо только за доверенным HTTPS-прокси внутри сети")
	}
	return cfg, nil
}
