//go:build integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startPG(t *testing.T) string {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
		tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return dsn
}

func TestWithTenantTxSetsGUC(t *testing.T) {
	ctx := context.Background()
	dsn := startPG(t)
	p, err := New(ctx, dsn)
	require.NoError(t, err)
	defer p.Close()
	require.NoError(t, p.Ping(ctx))

	var got string
	err = p.WithTenantTx(ctx, "11111111-1111-1111-1111-111111111111", func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT current_setting('app.tenant_id', true)").Scan(&got)
	})
	require.NoError(t, err)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", got)
}

func TestWithTenantTxRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	dsn := startPG(t)
	p, err := New(ctx, dsn)
	require.NoError(t, err)
	defer p.Close()

	_, err = p.app.Exec(ctx, "CREATE TABLE t (n int)")
	require.NoError(t, err)
	wantErr := pgx.ErrNoRows
	_ = p.WithTenantTx(ctx, "22222222-2222-2222-2222-222222222222", func(tx pgx.Tx) error {
		_, _ = tx.Exec(ctx, "INSERT INTO t VALUES (1)")
		return wantErr
	})
	var count int
	require.NoError(t, p.app.QueryRow(ctx, "SELECT count(*) FROM t").Scan(&count))
	require.Equal(t, 0, count, "insert must roll back when fn errors")
}
