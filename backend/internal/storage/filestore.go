// Package storage — слой хранения заявок.
// MVP: in-memory + персистентность в JSON-файл на docker volume (атомарная запись через rename).
// Целевое решение: PostgreSQL (см. docs/ARCHITECTURE.md), интерфейс Repository не меняется.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

type Filter struct {
	ClientID string
	Statuses []domain.Status
}

type Repository interface {
	Create(ctx context.Context, a *domain.Application) (*domain.Application, error)
	Get(ctx context.Context, id string) (*domain.Application, error)
	// Update выполняет fn атомарно (под блокировкой); при ошибке изменения откатываются.
	Update(ctx context.Context, id string, fn func(a *domain.Application) error) (*domain.Application, error)
	List(ctx context.Context, f Filter) ([]*domain.Application, error)
	Ping(ctx context.Context) error
}

type FileStore struct {
	mu   sync.RWMutex
	path string
	seq  int64
	apps map[string]*domain.Application
}

type snapshot struct {
	Seq          int64                 `json:"seq"`
	Applications []*domain.Application `json:"applications"`
}

// clientIDs хранится отдельно, т.к. в JSON-модели API ClientID скрыт.
type persisted struct {
	snapshot
	Owners map[string]string `json:"owners"`
}

func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, apps: map[string]*domain.Application{}}
	if path == "" {
		return s, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read store: %w", err)
	}
	var p persisted
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decode store: %w", err)
	}
	s.seq = p.Seq
	for _, a := range p.Applications {
		a.ClientID = p.Owners[a.ID]
		s.apps[a.ID] = a
	}
	return s, nil
}

func (s *FileStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	p := persisted{snapshot: snapshot{Seq: s.seq}, Owners: map[string]string{}}
	for _, a := range s.apps {
		p.Applications = append(p.Applications, a)
		p.Owners[a.ID] = a.ClientID
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func clone(a *domain.Application) *domain.Application {
	b, _ := json.Marshal(a)
	var c domain.Application
	_ = json.Unmarshal(b, &c)
	c.ClientID = a.ClientID
	return &c
}

func (s *FileStore) Create(_ context.Context, a *domain.Application) (*domain.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	a.Number = fmt.Sprintf("КР-%06d", s.seq)
	s.apps[a.ID] = clone(a)
	if err := s.persistLocked(); err != nil {
		delete(s.apps, a.ID)
		s.seq--
		return nil, err
	}
	return clone(a), nil
}

func (s *FileStore) Get(_ context.Context, id string) (*domain.Application, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.apps[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(a), nil
}

func (s *FileStore) Update(_ context.Context, id string, fn func(a *domain.Application) error) (*domain.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.apps[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	next := clone(cur)
	if err := fn(next); err != nil {
		return nil, err
	}
	s.apps[id] = next
	if err := s.persistLocked(); err != nil {
		s.apps[id] = cur
		return nil, err
	}
	return clone(next), nil
}

func (s *FileStore) List(_ context.Context, f Filter) ([]*domain.Application, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.Application, 0, len(s.apps))
	for _, a := range s.apps {
		if f.ClientID != "" && a.ClientID != f.ClientID {
			continue
		}
		if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, a.Status) {
			continue
		}
		out = append(out, clone(a))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *FileStore) Ping(context.Context) error { return nil }
