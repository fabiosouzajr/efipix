package idempotency

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/efipix/pix/internal/platform/db"
)

type Reservation struct {
	State        string // "new" | "replay" | "conflict" | "inflight"
	StoredStatus int
	StoredBody   []byte
}

type Store interface {
	Reserve(ctx context.Context, tenantID, key, fingerprint string) (Reservation, error)
	SaveResult(ctx context.Context, tenantID, key string, status int, body []byte) error
}

type PgStore struct{ pool *db.Pool }

func NewPg(pool *db.Pool) *PgStore { return &PgStore{pool: pool} }

func (s *PgStore) Reserve(ctx context.Context, tenantID, key, fingerprint string) (Reservation, error) {
	var res Reservation
	err := s.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx,
			`INSERT INTO idempotency_keys (tenant_id, key, fingerprint)
			 VALUES ($1,$2,$3) ON CONFLICT (tenant_id, key) DO NOTHING`,
			tenantID, key, fingerprint)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 1 {
			res = Reservation{State: "new"}
			return nil
		}
		var fp string
		var status int
		var body []byte
		if err := tx.QueryRow(ctx,
			`SELECT fingerprint, status, response FROM idempotency_keys
			 WHERE tenant_id=$1 AND key=$2`, tenantID, key,
		).Scan(&fp, &status, &body); err != nil {
			return err
		}
		switch {
		case fp != fingerprint:
			res = Reservation{State: "conflict"}
		case status > 0:
			res = Reservation{State: "replay", StoredStatus: status, StoredBody: body}
		default:
			res = Reservation{State: "inflight"}
		}
		return nil
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, err
	}
	return res, nil
}

func (s *PgStore) SaveResult(ctx context.Context, tenantID, key string, status int, body []byte) error {
	return s.pool.WithTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE idempotency_keys SET status=$1, response=$2 WHERE tenant_id=$3 AND key=$4`,
			status, body, tenantID, key)
		return err
	})
}
