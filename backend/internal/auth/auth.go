// Package auth — примитивы аутентификации: хэширование паролей, сессии, защита от перебора.
// В проде: OIDC (Альфа ID) на API Gateway, сессии в Redis.
package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const iterations = 100_000

// HashPassword — PBKDF2-SHA256 с солью. Формат: pbkdf2$<iter>$<salt hex>$<hash hex>.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	h, err := pbkdf2.Key(sha256.New, pw, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", iterations, hex.EncodeToString(salt), hex.EncodeToString(h)), nil
}

// VerifyPassword сравнивает пароль с хэшем за постоянное время.
func VerifyPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err1 := hex.DecodeString(parts[2])
	want, err2 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// ---------- сессии ----------

// SessionStore — хранилище сессий. В PostgreSQL-режиме сессии лежат в БД (pgstore.Sessions),
// поэтому любой экземпляр API принимает cookie, выданную другим, — сервис горизонтально масштабируется.
type SessionStore interface {
	Create(ctx context.Context, userID string) (string, error)
	Get(ctx context.Context, token string) (string, bool)
	Delete(ctx context.Context, token string)
	Lifetime() time.Duration
}

// NewToken — случайный 256-битный токен сессии.
func NewToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Sessions — сессии в памяти процесса (локальный режим и тесты).
type session struct {
	userID  string
	expires time.Time
}

type Sessions struct {
	mu  sync.Mutex
	m   map[string]session
	TTL time.Duration
	Now func() time.Time
}

func NewSessions(ttl time.Duration) *Sessions {
	return &Sessions{m: map[string]session{}, TTL: ttl, Now: time.Now}
}

func (s *Sessions) Lifetime() time.Duration { return s.TTL }

// Create выпускает случайный 256-битный токен сессии.
func (s *Sessions) Create(_ context.Context, userID string) (string, error) {
	tok := NewToken()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[tok] = session{userID: userID, expires: s.Now().Add(s.TTL)}
	return tok, nil
}

func (s *Sessions) Get(_ context.Context, token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.m[token]
	if !ok {
		return "", false
	}
	if s.Now().After(ss.expires) {
		delete(s.m, token)
		return "", false
	}
	return ss.userID, true
}

func (s *Sessions) Delete(_ context.Context, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, token)
}

// ---------- лимитер попыток входа ----------

type attempt struct {
	n     int
	until time.Time
}

// Limiter блокирует ключ (телефон или IP) на window после max неудачных попыток.
type Limiter struct {
	mu     sync.Mutex
	m      map[string]*attempt
	max    int
	window time.Duration
	Now    func() time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{m: map[string]*attempt{}, max: max, window: window, Now: time.Now}
}

func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[key]
	return a != nil && l.Now().Before(a.until)
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[key]
	if a == nil {
		a = &attempt{}
		l.m[key] = a
	}
	a.n++
	if a.n >= l.max {
		a.n, a.until = 0, l.Now().Add(l.window)
	}
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}
