#!/usr/bin/env bash
# Generate minimal compiling stubs for each service: go.mod, main.go, migrations dir.
set -euo pipefail

SERVICES=("svc-aerial-subscriber:8082:subscriber"
          "svc-aerial-esim:8083:esim"
          "svc-aerial-provision:8084:provision"
          "svc-aerial-ran-control:8085:ranctl"
          "svc-aerial-billing:8086:billing"
          "svc-aerial-messaging:8087:messaging")

for entry in "${SERVICES[@]}"; do
  IFS=':' read -r svc port schema <<<"$entry"
  mkdir -p "$svc/cmd/server" "$svc/migrations" "$svc/internal/config"

  cat > "$svc/go.mod" <<EOF
module github.com/amayabdaniel/aerial-ran-platform/$svc

go 1.26.1

require (
	github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go v0.0.0
	github.com/jackc/pgx/v5 v5.7.1
)
EOF

  cat > "$svc/cmd/server/main.go" <<EOF
// $svc — phase 0 stub: healthz + metrics.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	healthlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/health"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/httplog"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/metrics"
	recoverlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/recover"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/tracing"
	"github.com/jackc/pgx/v5/pgxpool"
)

const serviceName = "$svc"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	port := envOr("PORT", "$port")
	dsn := envOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:5432/aerial?sslmode=disable&search_path=$schema")
	otelEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracer, err := tracing.Setup(ctx, serviceName, otelEndpoint)
	if err != nil {
		logger.Warn("tracing setup failed", "err", err)
	}
	defer func() { _ = shutdownTracer(context.Background()) }()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("pgx pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	checker := &healthlib.Checker{}
	checker.Start(ctx, 5*time.Second, func(c context.Context) error { return pool.Ping(c) })

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/v1/health", checker.Handler())
	mux.Handle("/v1/ready", checker.Handler())

	handler := chain(
		recoverlib.Middleware(logger),
		metrics.Middleware(serviceName),
		httplog.Middleware(logger),
	)(mux)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("listen", "service", serviceName, "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}
EOF

  cat > "$svc/migrations/000001_init.up.sql" <<EOF
SET search_path TO $schema, public;
CREATE TABLE IF NOT EXISTS _bootstrap (
  bootstrapped_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
EOF

  cat > "$svc/migrations/000001_init.down.sql" <<EOF
SET search_path TO $schema, public;
DROP TABLE IF EXISTS _bootstrap;
EOF

  echo "scaffolded $svc on :$port schema=$schema"
done
