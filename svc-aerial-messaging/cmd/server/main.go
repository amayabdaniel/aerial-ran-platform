// svc-aerial-messaging — user-to-user messaging on NATS JetStream + WS push.
package main

import (
	"context"
	"log/slog"
	"net/http"

	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-messaging/internal/messaging"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	natsURL := runner.EnvOr("NATS_URL", "nats://localhost:14222")
	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-messaging",
		Port:         runner.EnvOr("PORT", "8087"),
		DatabaseURL:  runner.EnvOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=messaging"),
		JWTSecret:    runner.EnvOr("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:    runner.EnvOr("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:  runner.EnvOr("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint: runner.EnvOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		CORSOrigins:  runner.EnvOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		Mount: func(ctx context.Context, mux *http.ServeMux, pool *pgxpool.Pool, _ *jwtlib.Issuer) {
			svc, err := messaging.New(ctx, pool, natsURL)
			if err != nil {
				slog.Error("messaging init", "err", err)
				return
			}
			messaging.NewHandler(svc).Mount(mux)
		},
	})
}
