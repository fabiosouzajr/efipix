package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	chargeapi "github.com/efipix/pix/internal/charge/api"
	chargeapp "github.com/efipix/pix/internal/charge/app"
	chargeinfra "github.com/efipix/pix/internal/charge/infra"
	"github.com/efipix/pix/internal/platform/config"
	"github.com/efipix/pix/internal/platform/db"
	"github.com/efipix/pix/internal/platform/health"
	"github.com/efipix/pix/internal/platform/idempotency"
	"github.com/efipix/pix/internal/platform/logging"
	"github.com/efipix/pix/internal/platform/secrets"
	"github.com/efipix/pix/internal/provider/efi"
	tenantapi "github.com/efipix/pix/internal/tenant/api"
	tenantapp "github.com/efipix/pix/internal/tenant/app"
	tenantinfra "github.com/efipix/pix/internal/tenant/infra"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logging.New(cfg.LogLevel)
	ctx := context.Background()

	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sp, err := secrets.NewEnv()
	if err != nil {
		log.Error("secrets", "err", err)
		os.Exit(1)
	}

	tenantRepo := tenantinfra.New(pool)
	resolver := tenantapp.NewResolver(tenantRepo)
	chargeRepo := chargeinfra.New(pool)
	idem := idempotency.NewPg(pool)
	prov := efi.New(sp, efi.SDKFactory)
	createUC := chargeapp.NewCreateImmediateCharge(chargeRepo, prov)
	chargeHandler := chargeapi.NewHandler(createUC, chargeRepo)

	r := gin.New()
	r.Use(gin.Recovery())
	health.Register(r, pool.Ping)

	v1 := r.Group("/api/v1")
	v1.Use(tenantapi.Middleware(resolver))
	chargeapi.RegisterRoutes(v1, chargeHandler, idem)

	srv := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: r}
	go func() {
		log.Info("listening", "port", cfg.HTTPPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}
