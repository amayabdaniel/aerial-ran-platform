// Package runner reduces boilerplate in each svc-aerial-* main:
// pgx pool, OTel, JWT middleware, observability mux, signal handling.
package runner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/bodylimit"
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
	// AccessTokenTTL is the access-token lifetime the JWT issuer signs with.
	// Zero means the historical 15m default (see accessTTL). Only iam, which
	// actually issues tokens, needs to set it (from cfg.AccessTokenTTL); the
	// verify-only services leave it unset.
	AccessTokenTTL time.Duration
	// AuthSkipPaths are service-specific paths the auth middleware must not
	// guard, in addition to the always-public /metrics, /v1/health, /v1/ready.
	// iam adds its public /v1/auth/* endpoints here.
	AuthSkipPaths []string
	// Mount is called after pgxpool is ready; mount your handlers on mux.
	Mount func(ctx context.Context, mux *http.ServeMux, pool *pgxpool.Pool, jwt *jwtlib.Issuer)
}

// Run wires the boilerplate and blocks until SIGTERM/SIGINT.
func Run(opts Opts) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	logger := slog.Default()

	// Refuse to boot on a signing key too short to be a safe HS256 secret. This is
	// the one seam every service's JWT_SECRET passes through, so the floor the
	// config-package services enforced individually now covers the inline ones too.
	// os.Exit(1) rather than panic is safe here: nothing is deferred yet (stop(),
	// the tracer shutdown and pool.Close() are all registered below), so there is
	// no cleanup to skip — the same fail-fast as the pgxpool error further down.
	if err := validateSecret(opts.JWTSecret); err != nil {
		logger.Error("invalid JWT_SECRET", "err", err)
		os.Exit(1)
	}

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
	metrics.RegisterDBPool(opts.ServiceName, func() metrics.DBPoolStats { return pool.Stat() })

	issuer := buildIssuer(opts)

	checker := &healthlib.Checker{}
	checker.Start(ctx, 5*time.Second, func(c context.Context) error { return pool.Ping(c) })

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/v1/health", checker.Handler())
	mux.Handle("/v1/ready", checker.Handler())

	if opts.Mount != nil {
		opts.Mount(ctx, mux, pool, issuer)
	}

	authMW := issuer.Middleware(authSkipPaths(opts)...)
	stack := Handler(logger, opts.ServiceName, opts.CORSOrigins, authMW, mux)

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

// defaultAccessTTL is the access-token lifetime used when Opts.AccessTokenTTL is
// unset — the value every runner-based service signed with before iam's TTL was
// threaded through Opts.
const defaultAccessTTL = 15 * time.Minute

// accessTTL resolves the access-token lifetime: the configured value, or the
// historical 15m default when unset. Kept separate from buildIssuer so the
// default is unit-testable and so the TTL wiring is a single, mutable call site.
func accessTTL(opts Opts) time.Duration {
	if opts.AccessTokenTTL <= 0 {
		return defaultAccessTTL
	}
	return opts.AccessTokenTTL
}

// minJWTSecretLen is the shortest HS256 signing key runner will boot with. It is
// 16 — the same floor the three config-package services (esim, iam, subscriber)
// each enforced in their own Load() with `len(JWTSecret) < 16`. This seam adopts
// that existing bound; it does not loosen or tighten it. billing, provision,
// ran-control and messaging read JWT_SECRET inline and enforced nothing, so a
// too-short key silently signed and verified real tokens there; validating at the
// one seam every service passes through means the floor can no longer be skipped
// by a service that doesn't use a config package. The 32-char dev default clears
// it, so every service still boots unchanged at the default.
const minJWTSecretLen = 16

// validateSecret rejects a JWT signing key shorter than minJWTSecretLen. The
// message matches the one the config packages already return so the boot-time
// failure reads identically wherever it fires.
func validateSecret(secret string) error {
	if len(secret) < minJWTSecretLen {
		return fmt.Errorf("JWT_SECRET must be at least %d chars", minJWTSecretLen)
	}
	return nil
}

// buildIssuer constructs the JWT issuer exactly as Run does. Run and the TTL
// equivalence test both go through this function, so any change to the TTL
// wiring (e.g. reverting to a hardcoded 15m) is caught by the test rather than
// silently logging real users out early. See runner_test.go.
func buildIssuer(opts Opts) *jwtlib.Issuer {
	return jwtlib.New(opts.JWTSecret, opts.JWTIssuer, opts.JWTAudience, accessTTL(opts))
}

// authSkipPaths is the effective set of paths the auth middleware leaves
// unguarded: the three always-public endpoints followed by any service-specific
// additions. A fresh slice is returned each call so the standard set cannot be
// mutated by a caller. iam passes its public /v1/auth/* endpoints via
// Opts.AuthSkipPaths, yielding its historical seven-path set.
func authSkipPaths(opts Opts) []string {
	paths := []string{"/metrics", "/v1/health", "/v1/ready"}
	return append(paths, opts.AuthSkipPaths...)
}

// Handler assembles the standard middleware chain around mux:
// recover → metrics → cors → httplog → auth → bodyLimit → handler. It is
// exported so each service's cmd/server test can drive the real stack and prove
// the request-body limit is enforced; Run uses it too, so the body limit cannot
// be dropped here without those tests failing.
func Handler(logger *slog.Logger, serviceName, corsOrigins string, authMW func(http.Handler) http.Handler, mux http.Handler) http.Handler {
	return chain(
		recoverlib.Middleware(logger),
		metrics.Middleware(serviceName),
		corsMW(corsOrigins),
		httplog.Middleware(logger),
		authMW,
		bodylimit.Middleware(bodylimit.DefaultMaxBytes),
	)(mux)
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
