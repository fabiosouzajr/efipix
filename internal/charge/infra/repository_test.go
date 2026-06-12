//go:build integration

package infra

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	chargeapp "github.com/efipix/pix/internal/charge/app"
	"github.com/efipix/pix/internal/charge/domain"
	"github.com/efipix/pix/internal/platform/db"
	"github.com/efipix/pix/internal/platform/money"
)

const (
	devTenant   = "11111111-1111-1111-1111-111111111111"
	devProvider = "22222222-2222-2222-2222-222222222222"
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

func newCharge() *domain.Charge {
	c, _ := domain.NewImmediate(domain.NewImmediateParams{
		TenantID: devTenant, PaymentProviderID: devProvider, PixKey: "k@e.com",
		Amount: money.Centavos(1050), ExpirationSeconds: 3600,
	})
	return c
}

func TestCreateThenActivate(t *testing.T) {
	ctx := context.Background()
	r := New(setup(t))
	c := newCharge()

	require.NoError(t, r.Create(ctx, c))

	got, err := r.FindByID(ctx, devTenant, c.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusCreated, got.Status)
	require.Equal(t, money.Centavos(1050), got.Amount)

	require.NoError(t, got.MarkActive("loc1", "img", "000201..."))
	out := chargeapp.OutboxEvent{ID: uuid.NewString(), TenantID: devTenant, AggregateID: got.ID, Type: "ChargeCreated", Payload: []byte("{}")}
	require.NoError(t, r.Save(ctx, got, out))

	reloaded, err := r.FindByID(ctx, devTenant, c.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusActive, reloaded.Status)
	require.Equal(t, 1, reloaded.Version)
	require.Equal(t, "000201...", reloaded.PixPayload)
}

func TestSaveOptimisticConflict(t *testing.T) {
	ctx := context.Background()
	r := New(setup(t))
	c := newCharge()
	require.NoError(t, r.Create(ctx, c))

	a, _ := r.FindByID(ctx, devTenant, c.ID)
	b, _ := r.FindByID(ctx, devTenant, c.ID)
	require.NoError(t, a.MarkActive("l", "i", "p"))
	require.NoError(t, r.Save(ctx, a)) // version 0 -> 1

	require.NoError(t, b.MarkActive("l", "i", "p")) // b still at version 0
	err := r.Save(ctx, b)
	require.Error(t, err)
}
