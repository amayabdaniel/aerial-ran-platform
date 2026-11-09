// svc-aerial-iam — identity / OIDC-lite / users / devices / JWT.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/config"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/handler"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/repository"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-iam",
		Port:         cfg.Port,
		DatabaseURL:  cfg.DatabaseURL,
		JWTSecret:    cfg.JWTSecret,
		JWTIssuer:    cfg.JWTIssuer,
		JWTAudience:  cfg.JWTAudience,
		OTelEndpoint: cfg.OTelEndpoint,
		CORSOrigins:  cfg.CORSOrigins,
		// iam is the token issuer: it signs access tokens with the configured
		// lifetime, so this must reach the runner-built issuer (see the TTL
		// equivalence test in lib-aerial-go/runner).
		AccessTokenTTL: cfg.AccessTokenTTL,
		// Public auth endpoints must not be guarded by the bearer-token check;
		// runner adds the always-public /metrics, /v1/health, /v1/ready.
		AuthSkipPaths: []string{
			"/v1/auth/signup",
			"/v1/auth/login",
			"/v1/auth/refresh",
			"/v1/auth/logout",
		},
		Mount: func(_ context.Context, mux *http.ServeMux, pool *pgxpool.Pool, issuer *jwtlib.Issuer) {
			repo := repository.New(pool)
			svc := service.New(repo, issuer, cfg.RefreshTokenTTL)
			handler.New(svc).Mount(mux)
		},
	})
}
