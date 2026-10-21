package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/xdtqwd/alfa-credit/internal/domain"
	"github.com/xdtqwd/alfa-credit/internal/events"
	"github.com/xdtqwd/alfa-credit/internal/obs"
	"github.com/xdtqwd/alfa-credit/internal/storage"
)

// Apps — репозиторий заявок (storage.Repository).
type Apps struct{ *Store }

func (s *Store) Apps() *Apps { return &Apps{s} }

func companyName(a *domain.Application) string {
	if a.Company != nil {
		return a.Company.Name
	}
	return ""
}

func encodeApp(a *domain.Application) ([]byte, error) {
	c := *a
	c.Quote, c.Schedule, c.NextPayment = nil, nil, 0 // вычисляемые поля не храним
	return json.Marshal(&c)
}

func decodeApp(doc []byte, clientID string) (*domain.Application, error) {
	var a domain.Application
	if err := json.Unmarshal(doc, &a); err != nil {
		return nil, err
	}
	a.ClientID = clientID
	return &a, nil
}

func (r *Apps) Create(ctx context.Context, a *domain.Application) (*domain.Application, error) {
	var out *domain.Application
	err := r.InTx(ctx, func(ctx context.Context) error {
		var seq int64
		if err := r.q(ctx).QueryRowContext(ctx, `SELECT nextval('application_number_seq')`).Scan(&seq); err != nil {
			return err
		}
		c := *a
		c.Number = fmt.Sprintf("КР-%06d", seq)
		doc, err := encodeApp(&c)
		if err != nil {
			return err
		}
		if _, err := r.q(ctx).ExecContext(ctx, `INSERT INTO applications
			(id, number, client_id, status, product_id, inn, company_name, doc, created_at, updated_at, status_changed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			c.ID, c.Number, c.ClientID, string(c.Status), c.ProductID, c.INN, companyName(&c), doc, c.CreatedAt, c.UpdatedAt, c.StatusChangedAt); err != nil {
			return err
		}
		if err := r.writeOutbox(ctx, events.FromChange(nil, &c, obs.RequestID(ctx))); err != nil {
			return err
		}
		out = &c
		return nil
	})
	return out, err
}

func (r *Apps) get(ctx context.Context, id string, lock bool) (*domain.Application, error) {
	q := `SELECT doc, client_id FROM applications WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	var doc []byte
	var client string
	err := r.q(ctx).QueryRowContext(ctx, q, id).Scan(&doc, &client)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return decodeApp(doc, client)
}

func (r *Apps) Get(ctx context.Context, id string) (*domain.Application, error) {
	return r.get(ctx, id, false)
}

// Update: SELECT … FOR UPDATE → fn → UPDATE + события в outbox, всё в одной транзакции.
// Конкурентные изменения одной заявки выстраиваются в очередь на блокировке строки.
func (r *Apps) Update(ctx context.Context, id string, fn func(a *domain.Application) error) (*domain.Application, error) {
	var out *domain.Application
	err := r.InTx(ctx, func(ctx context.Context) error {
		before, err := r.get(ctx, id, true)
		if err != nil {
			return err
		}
		cur, _ := decodeApp(mustJSON(before), before.ClientID)
		if err := fn(cur); err != nil {
			return err
		}
		doc, err := encodeApp(cur)
		if err != nil {
			return err
		}
		if _, err := r.q(ctx).ExecContext(ctx, `UPDATE applications SET status=$2, product_id=$3, inn=$4, company_name=$5, doc=$6,
			version=version+1, updated_at=$7, status_changed_at=$8 WHERE id=$1`,
			id, string(cur.Status), cur.ProductID, cur.INN, companyName(cur), doc, cur.UpdatedAt, cur.StatusChangedAt); err != nil {
			return err
		}
		if err := r.writeOutbox(ctx, events.FromChange(before, cur, obs.RequestID(ctx))); err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, err
}

func mustJSON(a *domain.Application) []byte { b, _ := json.Marshal(a); return b }

func where(f storage.Filter) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if f.ClientID != "" {
		add("client_id = $%d", f.ClientID)
	}
	if len(f.Statuses) > 0 {
		ss := make([]string, len(f.Statuses))
		for i, s := range f.Statuses {
			ss[i] = string(s)
		}
		add("status = ANY($%d)", pq.Array(ss))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		args = append(args, like)
		n := len(args)
		conds = append(conds, fmt.Sprintf("(number ILIKE $%d OR inn ILIKE $%d OR company_name ILIKE $%d)", n, n, n))
	}
	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (r *Apps) List(ctx context.Context, f storage.Filter) ([]*domain.Application, error) {
	w, args := where(f)
	q := `SELECT doc, client_id FROM applications` + w + ` ORDER BY created_at DESC, number DESC`
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		q += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	rows, err := r.q(ctx).QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*domain.Application{}
	for rows.Next() {
		var doc []byte
		var client string
		if err := rows.Scan(&doc, &client); err != nil {
			return nil, err
		}
		a, err := decodeApp(doc, client)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Apps) Count(ctx context.Context, f storage.Filter) (int, error) {
	w, args := where(f)
	var n int
	err := r.q(ctx).QueryRowContext(ctx, `SELECT count(*) FROM applications`+w, args...).Scan(&n)
	return n, err
}

func (r *Apps) Ping(ctx context.Context) error { return r.DB.PingContext(ctx) }
