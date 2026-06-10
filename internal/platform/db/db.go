package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	app   *pgxpool.Pool
	admin *pgxpool.Pool // optional; from DATABASE_ADMIN_URL
}

func New(ctx context.Context, dsn string) (*Pool, error) {
	app, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: connect app pool: %w", err)
	}
	p := &Pool{app: app}
	if adminDSN := os.Getenv("DATABASE_ADMIN_URL"); adminDSN != "" {
		admin, err := pgxpool.New(ctx, adminDSN)
		if err != nil {
			return nil, fmt.Errorf("db: connect admin pool: %w", err)
		}
		p.admin = admin
	}
	return p, nil
}

func (p *Pool) Close() {
	p.app.Close()
	if p.admin != nil {
		p.admin.Close()
	}
}

func (p *Pool) Ping(ctx context.Context) error { return p.app.Ping(ctx) }

// WithTenantTx runs fn inside a tx scoped to tenantID via the app.tenant_id GUC.
func (p *Pool) WithTenantTx(ctx context.Context, tenantID string, fn func(pgx.Tx) error) error {
	return p.runTx(ctx, p.app, tenantID, fn)
}

// WithAdminTx runs fn inside a tx on the BYPASSRLS admin pool (no tenant scoping).
func (p *Pool) WithAdminTx(ctx context.Context, fn func(pgx.Tx) error) error {
	if p.admin == nil {
		return fmt.Errorf("db: admin pool not configured (set DATABASE_ADMIN_URL)")
	}
	return p.runTx(ctx, p.admin, "", fn)
}

func (p *Pool) runTx(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if tenantID != "" {
		if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			return fmt.Errorf("db: set tenant: %w", err)
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit: %w", err)
	}
	return nil
}
