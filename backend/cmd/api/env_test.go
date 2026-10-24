package main

import "testing"

// Числовые настройки: пусто — значение по умолчанию, опечатка — ошибка запуска с именем переменной.
func TestEnvNumbers(t *testing.T) {
	t.Setenv("T_RPS", "")
	if v, err := envFloat("T_RPS", 20); err != nil || v != 20 {
		t.Fatalf("empty float: %v %v", v, err)
	}
	t.Setenv("T_RPS", "2.5")
	if v, err := envFloat("T_RPS", 20); err != nil || v != 2.5 {
		t.Fatalf("float: %v %v", v, err)
	}
	t.Setenv("T_RPS", "20x")
	if _, err := envFloat("T_RPS", 20); err == nil {
		t.Fatal("invalid float must fail")
	}

	t.Setenv("T_PARTS", "")
	if v, err := envInt("T_PARTS", 6, 32); err != nil || v != 6 {
		t.Fatalf("empty int: %v %v", v, err)
	}
	t.Setenv("T_PARTS", "12")
	if v, err := envInt("T_PARTS", 6, 32); err != nil || v != 12 {
		t.Fatalf("int: %v %v", v, err)
	}
	for _, bad := range []string{"six", "70000"} { // 70000 не влезает в int16 (KAFKA_REPLICATION)
		t.Setenv("T_PARTS", bad)
		if _, err := envInt("T_PARTS", 1, 16); err == nil {
			t.Fatalf("envInt(%q, 16 bit) must fail", bad)
		}
	}
}
