//go:build integration

package idempotency

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
)

const devTenant = "11111111-1111-1111-1111-111111111111"

func setup(t *testing.T) *db.Pool {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("pix"), postgres.WithUsername("pix"), postgres.WithPassword("pix"),
		tc.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	run(t, "goose", "-dir", "../../../db/migrations", "postgres", dsn, "up")
	run(t, "psql", dsn, "-f", "../../../db/seed/dev.sql")

	t.Setenv("DATABASE_ADMIN_URL", dsn)
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

func TestReserveLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewPg(setup(t))

	r, err := s.Reserve(ctx, devTenant, "k1", "fpA")
	require.NoError(t, err)
	require.Equal(t, "new", r.State)

	r, err = s.Reserve(ctx, devTenant, "k1", "fpA") // still processing
	require.NoError(t, err)
	require.Equal(t, "inflight", r.State)

	r, err = s.Reserve(ctx, devTenant, "k1", "fpB") // different body
	require.NoError(t, err)
	require.Equal(t, "conflict", r.State)

	require.NoError(t, s.SaveResult(ctx, devTenant, "k1", 201, []byte(`{"txid":"x"}`)))
	r, err = s.Reserve(ctx, devTenant, "k1", "fpA")
	require.NoError(t, err)
	require.Equal(t, "replay", r.State)
	require.Equal(t, 201, r.StoredStatus)
	require.JSONEq(t, `{"txid":"x"}`, string(r.StoredBody))
}
