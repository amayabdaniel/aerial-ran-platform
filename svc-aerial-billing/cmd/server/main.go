// svc-aerial-billing — usage event ingest + monthly rollups.
package main

import (
	"context"
	"net/http"

	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-billing/internal/billing"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-billing",
		Port:         runner.EnvOr("PORT", "8086"),
		DatabaseURL:  runner.EnvOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=billing"),
		JWTSecret:    runner.EnvOr("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:    runner.EnvOr("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:  runner.EnvOr("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint: runner.EnvOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		CORSOrigins:  runner.EnvOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		Mount: func(_ context.Context, mux *http.ServeMux, pool *pgxpool.Pool, _ *jwtlib.Issuer) {
			billing.NewHandler(billing.New(pool)).Mount(mux)
		},
	})
}
