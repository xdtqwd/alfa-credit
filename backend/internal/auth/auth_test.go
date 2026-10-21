package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashing(t *testing.T) {
	h, err := HashPassword("secret-123")
	if err != nil || !strings.HasPrefix(h, "pbkdf2$") {
		t.Fatalf("hash=%q err=%v", h, err)
	}
	if strings.Contains(h, "secret-123") {
		t.Fatal("hash must not contain the password")
	}
	h2, _ := HashPassword("secret-123")
	if h == h2 {
		t.Fatal("salt must make hashes differ")
	}
	if !VerifyPassword(h, "secret-123") || VerifyPassword(h, "secret-124") || VerifyPassword(h, "") {
		t.Fatal("verify mismatch")
	}
	for _, bad := range []string{"", "plain", "pbkdf2$x$00$00", "pbkdf2$1$zz$00", "md5$1$00$00"} {
		if VerifyPassword(bad, "secret-123") {
			t.Fatalf("malformed hash %q accepted", bad)
		}
	}
}

func TestSessionsTTL(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewSessions(time.Hour)
	s.Now = func() time.Time { return now }
	tok := s.Create("u1")
	if len(tok) != 64 {
		t.Fatalf("token must be 256-bit hex, got %d chars", len(tok))
	}
	if uid, ok := s.Get(tok); !ok || uid != "u1" {
		t.Fatal("fresh session must resolve")
	}
	if _, ok := s.Get("forged"); ok {
		t.Fatal("unknown token resolved")
	}
	now = now.Add(61 * time.Minute)
	if _, ok := s.Get(tok); ok {
		t.Fatal("expired session must not resolve")
	}
	tok2 := s.Create("u2")
	s.Delete(tok2)
	if _, ok := s.Get(tok2); ok {
		t.Fatal("deleted session must not resolve")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := NewLimiter(5, time.Minute)
	l.Now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		l.Fail("k")
	}
	if l.Blocked("k") {
		t.Fatal("blocked too early")
	}
	l.Fail("k")
	if !l.Blocked("k") || l.Blocked("other") {
		t.Fatal("must block exactly the key after 5 failures")
	}
	now = now.Add(61 * time.Second)
	if l.Blocked("k") {
		t.Fatal("block must expire")
	}
	l.Fail("k")
	l.Reset("k")
	for i := 0; i < 4; i++ {
		l.Fail("k")
	}
	if l.Blocked("k") {
		t.Fatal("reset must clear the counter")
	}
}
