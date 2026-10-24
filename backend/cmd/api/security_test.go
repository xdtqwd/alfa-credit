package main

import (
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadSecurity(t *testing.T) {
	strong := "s3cr3t-bank-token-xyz"
	cases := []struct {
		name       string
		env        map[string]string
		wantErr    string
		wantSecure bool
		wantToken  string
		wantWarn   bool
	}{
		{name: "по умолчанию production без секрета — отказ", env: nil, wantErr: "BANK_TOKEN не задан"},
		{name: "production с демо-токеном — отказ", env: map[string]string{"BANK_TOKEN": "demo-bank"}, wantErr: "публичное демо-значение"},
		{name: "production с коротким токеном — отказ", env: map[string]string{"BANK_TOKEN": "short"}, wantErr: "короче"},
		{name: "production с секретом — secure cookie по умолчанию", env: map[string]string{"BANK_TOKEN": strong}, wantSecure: true, wantToken: strong},
		{name: "production без secure cookie — предупреждение", env: map[string]string{"BANK_TOKEN": strong, "COOKIE_SECURE": "false"}, wantToken: strong, wantWarn: true},
		{name: "демо — публичный токен и cookie без Secure явно", env: map[string]string{"APP_MODE": "demo"}, wantToken: "demo-bank", wantWarn: true},
		{name: "демо со своим токеном и secure", env: map[string]string{"APP_MODE": "Demo", "BANK_TOKEN": "x", "COOKIE_SECURE": "true"}, wantSecure: true, wantToken: "x"},
		{name: "неизвестный режим — отказ", env: map[string]string{"APP_MODE": "dev"}, wantErr: "APP_MODE"},
		{name: "кривое COOKIE_SECURE — отказ", env: map[string]string{"APP_MODE": "demo", "COOKIE_SECURE": "yes"}, wantErr: "COOKIE_SECURE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := loadSecurity(envOf(c.env))
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("ждали ошибку с %q, получили %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CookieSecure != c.wantSecure || cfg.BankToken != c.wantToken || (len(cfg.Warnings) > 0) != c.wantWarn {
				t.Fatalf("получили %+v", cfg)
			}
		})
	}
}
