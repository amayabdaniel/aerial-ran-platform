// svc-aerial-provision — plans + subscriptions binding user → sim/esim.
package main

import (
	"context"
	"net/http"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/env"
	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-provision/internal/provision"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-provision",
		Port:         env.Or("PORT", "8084"),
		DatabaseURL:  env.Or("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=provision"),
		JWTSecret:    env.Or("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:    env.Or("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:  env.Or("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint: env.Or("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		CORSOrigins:  env.Or("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		Mount: func(_ context.Context, mux *http.ServeMux, pool *pgxpool.Pool, _ *jwtlib.Issuer) {
			provision.NewHandler(provision.New(pool)).Mount(mux)
		},
	})
}
