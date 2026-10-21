package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/xdtqwd/alfa-credit/internal/domain"
)

// Users — репозиторий пользователей (storage.UserRepository). Баланс — отдельная колонка
// с CHECK (balance >= 0): даже ошибка в коде не даст списать больше, чем есть на счёте.
type Users struct{ *Store }

func (s *Store) Users() *Users { return &Users{s} }

func (r *Users) scan(row *sql.Row) (*domain.User, error) {
	var doc []byte
	var hash string
	var bal int64
	if err := row.Scan(&doc, &hash, &bal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	var u domain.User
	if err := json.Unmarshal(doc, &u); err != nil {
		return nil, err
	}
	u.PasswordHash, u.Balance = hash, bal
	return &u, nil
}

func (r *Users) CreateUser(ctx context.Context, u *domain.User) error {
	doc, err := json.Marshal(u)
	if err != nil {
		return err
	}
	_, err = r.q(ctx).ExecContext(ctx, `INSERT INTO users (id, phone, password_hash, doc, balance, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, u.Phone, u.PasswordHash, doc, u.Balance, u.CreatedAt)
	if isUniqueViolation(err) {
		return domain.ErrUserExists
	}
	return err
}

func (r *Users) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return r.scan(r.q(ctx).QueryRowContext(ctx, `SELECT doc, password_hash, balance FROM users WHERE id=$1`, id))
}

func (r *Users) FindByPhone(ctx context.Context, phone string) (*domain.User, error) {
	return r.scan(r.q(ctx).QueryRowContext(ctx, `SELECT doc, password_hash, balance FROM users WHERE phone=$1`, phone))
}

// UpdateUser — изменение под блокировкой строки (SELECT … FOR UPDATE) в транзакции.
func (r *Users) UpdateUser(ctx context.Context, id string, fn func(u *domain.User) error) (*domain.User, error) {
	var out *domain.User
	err := r.InTx(ctx, func(ctx context.Context) error {
		u, err := r.scan(r.q(ctx).QueryRowContext(ctx, `SELECT doc, password_hash, balance FROM users WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if err := fn(u); err != nil {
			return err
		}
		doc, err := json.Marshal(u)
		if err != nil {
			return err
		}
		if _, err := r.q(ctx).ExecContext(ctx, `UPDATE users SET doc=$2, password_hash=$3, balance=$4, updated_at=now() WHERE id=$1`,
			id, doc, u.PasswordHash, u.Balance); err != nil {
			return err
		}
		out = u
		return nil
	})
	return out, err
}

func (r *Users) CountUsers(ctx context.Context) int {
	var n int
	_ = r.q(ctx).QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n
}
