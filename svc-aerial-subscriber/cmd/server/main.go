// svc-aerial-subscriber — SIM/SUPI/IMSI registry + Open5GS MongoDB provisioning.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	healthlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/health"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/httplog"
	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/metrics"
	recoverlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/recover"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/tracing"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/config"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/handler"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/open5gs"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/repository"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

const serviceName = "svc-aerial-subscriber"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	logger := slog.Default()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracer, err := tracing.Setup(ctx, serviceName, cfg.OTelEndpoint)
	if err != nil {
		logger.Warn("tracing setup failed", "err", err)
	}
	defer func() { _ = shutdownTracer(context.Background()) }()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("pgx pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	mongoClient, err := open5gs.New(ctx, cfg.OpenMongoURI, cfg.OpenMongoDB)
	if err != nil {
		logger.Warn("open5gs mongo connect failed; will start without provisioning hook", "err", err)
	} else {
		defer func() { _ = mongoClient.Close(context.Background()) }()
	}

	repo := repository.New(pool)
	svc := service.New(repo, mongoClient, cfg.PLMNMcc, cfg.PLMNMnc)
	h := handler.New(svc)
	issuer := jwtlib.New(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTAudience, 15*time.Minute)

	checker := &healthlib.Checker{}
	checker.Start(ctx, 5*time.Second, func(c context.Context) error { return pool.Ping(c) })

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/v1/health", checker.Handler())
	mux.Handle("/v1/ready", checker.Handler())
	h.Mount(mux)

	authMW := issuer.Middleware("/metrics", "/v1/health", "/v1/ready")
	corsM := corsMW(cfg.CORSOrigins)

	stack := chain(
		recoverlib.Middleware(logger),
		metrics.Middleware(serviceName),
		corsM,
		httplog.Middleware(logger),
		authMW,
	)(mux)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           stack,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("listen", "service", serviceName, "addr", srv.Addr, "plmn", cfg.PLMNMcc+"/"+cfg.PLMNMnc, "open5gs_mongo", cfg.OpenMongoURI)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}

func chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}

func corsMW(originsCSV string) func(http.Handler) http.Handler {
	origins := map[string]struct{}{}
	for _, o := range strings.Split(originsCSV, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			origins[o] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := origins[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Device-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
