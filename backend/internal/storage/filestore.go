// Package storage — порты хранения (Repository, UserRepository, TxRunner) и локальная реализация.
//
// Основное хранилище — PostgreSQL (пакет storage/pgstore): ACID-транзакции, блокировки строк,
// transactional outbox. FileStore — режим без внешних зависимостей для тестов и запуска «одной командой»
// без Docker: in-memory + JSON-файл с атомарной записью через rename.
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
	"strings"
	"sync"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

// Filter — выборка заявок. Сортировка — по дате создания, новые сверху.
type Filter struct {
	ClientID string
	Statuses []domain.Status
	Query    string // поиск по номеру, ИНН или названию компании (кабинет банка)
	Limit    int    // 0 — без ограничения
	Offset   int
}

// TxRunner выполняет fn в одной транзакции: все изменения заявки и счёта клиента внутри fn
// фиксируются или откатываются вместе. Репозитории берут транзакцию из ctx.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// NoTx — TxRunner для файлового режима: транзакций нет, атомарность обеспечивают блокировки
// и компенсации в сервисе.
type NoTx struct{}

func (NoTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

// Match — общая логика фильтра (для FileStore и тестов).
func (f Filter) Match(a *domain.Application) bool {
	if f.ClientID != "" && a.ClientID != f.ClientID {
		return false
	}
	if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, a.Status) {
		return false
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" {
		name := ""
		if a.Company != nil {
			name = a.Company.Name
		}
		hay := strings.ToLower(a.Number + " " + a.INN + " " + name)
		if !strings.Contains(hay, q) {
			return false
		}
	}
	return true
}

// Paginate применяет Limit/Offset.
func Paginate[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return items[:0]
	}
	items = items[offset:]
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items
}

type Repository interface {
	Create(ctx context.Context, a *domain.Application) (*domain.Application, error)
	Get(ctx context.Context, id string) (*domain.Application, error)
	// Update выполняет fn атомарно (под блокировкой); при ошибке изменения откатываются.
	Update(ctx context.Context, id string, fn func(a *domain.Application) error) (*domain.Application, error)
	// List возвращает страницу заявок по фильтру и общее число подходящих (для X-Total-Count).
	List(ctx context.Context, f Filter) ([]*domain.Application, error)
	Count(ctx context.Context, f Filter) (int, error)
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
	matched := make([]*domain.Application, 0, len(s.apps))
	for _, a := range s.apps {
		if f.Match(a) {
			matched = append(matched, a)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].CreatedAt.Equal(matched[j].CreatedAt) {
			return matched[i].Number > matched[j].Number
		}
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})
	page := Paginate(matched, f.Limit, f.Offset)
	out := make([]*domain.Application, len(page))
	for i, a := range page {
		out[i] = clone(a)
	}
	return out, nil
}

func (s *FileStore) Count(_ context.Context, f Filter) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, a := range s.apps {
		if f.Match(a) {
			n++
		}
	}
	return n, nil
}

func (s *FileStore) Ping(context.Context) error { return nil }
