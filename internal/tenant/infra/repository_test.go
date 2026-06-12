//go:build integration

package infra

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/efipix/pix/internal/platform/db"
	apperrs "github.com/efipix/pix/internal/platform/errors"
	tapp "github.com/efipix/pix/internal/tenant/app"
)

func setup(t *testing.T) *db.Pool {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
		tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Run migrations + seed as admin (the superuser created by testcontainers IS the owner).
	run(t, "goose", "-dir", "../../../db/migrations", "postgres", dsn, "up")
	run(t, "psql", dsn, "-f", "../../../db/seed/dev.sql")

	t.Setenv("DATABASE_ADMIN_URL", dsn) // admin path uses the same superuser here
	pool, err := db.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func run(t *testing.T, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "command %s failed", name)
}

func TestResolveChain(t *testing.T) {
	ctx := context.Background()
	pool := setup(t)
	r := New(pool)

	tenantID, err := r.TenantByAPIKeyHash(ctx, tapp.HashAPIKey("pk_dev_secret"))
	require.NoError(t, err)
	require.Equal(t, "11111111-1111-1111-1111-111111111111", tenantID)

	_, err = r.TenantByAPIKeyHash(ctx, tapp.HashAPIKey("wrong"))
	require.Error(t, err)

	acct, err := r.ResolveAccount(ctx, tenantID, "")
	require.NoError(t, err)
	require.Equal(t, "22222222-2222-2222-2222-222222222222", acct.ProviderID)
	require.Equal(t, "dev-pix-key@example.com", acct.PixKey)

	acct2, err := r.ResolveAccount(ctx, tenantID, acct.ProviderID)
	require.NoError(t, err)
	require.Equal(t, acct, acct2)

	_, err = r.ResolveAccount(ctx, tenantID, "00000000-0000-0000-0000-000000000000")
	require.Equal(t, apperrs.KindValidation, apperrs.KindOf(err))
}
