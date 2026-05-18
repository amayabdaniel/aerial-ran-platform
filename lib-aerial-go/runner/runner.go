// Package runner reduces boilerplate in each svc-aerial-* main:
// pgx pool, OTel, JWT middleware, observability mux, signal handling.
package runner

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
	"github.com/jackc/pgx/v5/pgxpool"
)

// Opts is the runtime configuration for any aerial service.
type Opts struct {
	ServiceName  string
	Port         string
	DatabaseURL  string
	JWTSecret    string
	JWTIssuer    string
	JWTAudience  string
	OTelEndpoint string
	CORSOrigins  string
	// Mount is called after pgxpool is ready; mount your handlers on mux.
	Mount func(ctx context.Context, mux *http.ServeMux, pool *pgxpool.Pool, jwt *jwtlib.Issuer)
}

// Run wires the boilerplate and blocks until SIGTERM/SIGINT.
func Run(opts Opts) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	logger := slog.Default()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracer, err := tracing.Setup(ctx, opts.ServiceName, opts.OTelEndpoint)
	if err != nil {
		logger.Warn("tracing setup failed", "err", err)
	}
	defer func() { _ = shutdownTracer(context.Background()) }()

	pool, err := pgxpool.New(ctx, opts.DatabaseURL)
	if err != nil {
		logger.Error("pgx pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	issuer := jwtlib.New(opts.JWTSecret, opts.JWTIssuer, opts.JWTAudience, 15*time.Minute)

	checker := &healthlib.Checker{}
	checker.Start(ctx, 5*time.Second, func(c context.Context) error { return pool.Ping(c) })

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/v1/health", checker.Handler())
	mux.Handle("/v1/ready", checker.Handler())

	if opts.Mount != nil {
		opts.Mount(ctx, mux, pool, issuer)
	}

	authMW := issuer.Middleware("/metrics", "/v1/health", "/v1/ready")
	stack := chain(
		recoverlib.Middleware(logger),
		metrics.Middleware(opts.ServiceName),
		corsMW(opts.CORSOrigins),
		httplog.Middleware(logger),
		authMW,
	)(mux)

	srv := &http.Server{
		Addr:              ":" + opts.Port,
		Handler:           stack,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("listen", "service", opts.ServiceName, "addr", srv.Addr)
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
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

// EnvOr returns env(k) or def.
func EnvOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
