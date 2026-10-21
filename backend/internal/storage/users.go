package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

type UserRepository interface {
	CreateUser(ctx context.Context, u *domain.User) error
	GetUser(ctx context.Context, id string) (*domain.User, error)
	FindByPhone(ctx context.Context, phone string) (*domain.User, error)
	// UpdateUser — атомарное изменение (баланс, профиль); при ошибке fn изменения не применяются.
	UpdateUser(ctx context.Context, id string, fn func(u *domain.User) error) (*domain.User, error)
	CountUsers(ctx context.Context) int
}

// userRecord нужен, чтобы хэш пароля сохранялся в файл, но никогда не уходил в API.
type userRecord struct {
	domain.User
	PasswordHash string `json:"password_hash"`
}

type UserFileStore struct {
	mu    sync.RWMutex
	path  string
	users map[string]*domain.User
}

func NewUserFileStore(path string) (*UserFileStore, error) {
	s := &UserFileStore{path: path, users: map[string]*domain.User{}}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	var recs []userRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	for _, r := range recs {
		u := r.User
		u.PasswordHash = r.PasswordHash
		s.users[u.ID] = &u
	}
	return s, nil
}

func (s *UserFileStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	recs := make([]userRecord, 0, len(s.users))
	for _, u := range s.users {
		recs = append(recs, userRecord{User: *u, PasswordHash: u.PasswordHash})
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func cloneUser(u *domain.User) *domain.User { c := *u; return &c }

func (s *UserFileStore) CreateUser(_ context.Context, u *domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.users {
		if x.Phone == u.Phone {
			return domain.ErrUserExists
		}
	}
	s.users[u.ID] = cloneUser(u)
	if err := s.persistLocked(); err != nil {
		delete(s.users, u.ID)
		return err
	}
	return nil
}

func (s *UserFileStore) GetUser(_ context.Context, id string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return cloneUser(u), nil
}

func (s *UserFileStore) FindByPhone(_ context.Context, phone string) (*domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Phone == phone {
			return cloneUser(u), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *UserFileStore) UpdateUser(_ context.Context, id string, fn func(u *domain.User) error) (*domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	next := cloneUser(cur)
	if err := fn(next); err != nil {
		return nil, err
	}
	s.users[id] = next
	if err := s.persistLocked(); err != nil {
		s.users[id] = cur
		return nil, err
	}
	return cloneUser(next), nil
}

func (s *UserFileStore) CountUsers(context.Context) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}
