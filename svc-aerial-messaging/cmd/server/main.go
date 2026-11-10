// svc-aerial-messaging — user-to-user messaging on NATS JetStream + WS push.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/env"
	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-messaging/internal/messaging"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	natsURL := env.Or("NATS_URL", "nats://localhost:14222")
	allowedOrigins := env.Or("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080")
	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-messaging",
		Port:         env.Or("PORT", "8087"),
		DatabaseURL:  env.Or("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=messaging"),
		JWTSecret:    env.Or("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:    env.Or("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:  env.Or("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint: env.Or("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		CORSOrigins:  env.Or("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		Mount: func(ctx context.Context, mux *http.ServeMux, pool *pgxpool.Pool, _ *jwtlib.Issuer) {
			svc, err := messaging.New(ctx, pool, natsURL)
			if err != nil {
				// Exit non-zero so Kubernetes restarts the pod until NATS is
				// reachable. Returning here would leave a half-mounted server
				// that passes health checks but 404s on /v1/messages.
				slog.Error("messaging init failed; exiting so the pod restarts", "err", err)
				os.Exit(1)
			}
			messaging.NewHandler(svc, messaging.OriginHostsFromCSV(allowedOrigins)...).Mount(mux)
		},
	})
}
