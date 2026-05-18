// svc-aerial-ran-control — observes Open5GS NFs + subscribers via mongo.
package main

import (
	"context"
	"log/slog"
	"net/http"

	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-ran-control/internal/ranctl"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	mongoURI := runner.EnvOr("OPEN5GS_MONGO_URI", "mongodb://localhost:27017")
	mongoDB := runner.EnvOr("OPEN5GS_MONGO_DB", "open5gs")
	plmn := runner.EnvOr("PLMN_MCC", "999") + "/" + runner.EnvOr("PLMN_MNC", "70")
	nfList := runner.EnvOr("OPEN5GS_NF_METRICS",
		"amf=http://localhost:9091/metrics,smf=http://localhost:9092/metrics,upf=http://localhost:9093/metrics")

	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-ran-control",
		Port:         runner.EnvOr("PORT", "8085"),
		DatabaseURL:  runner.EnvOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=ranctl"),
		JWTSecret:    runner.EnvOr("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:    runner.EnvOr("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:  runner.EnvOr("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint: runner.EnvOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		CORSOrigins:  runner.EnvOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		Mount: func(ctx context.Context, mux *http.ServeMux, _ *pgxpool.Pool, _ *jwtlib.Issuer) {
			svc, err := ranctl.New(ctx, mongoURI, mongoDB, plmn, ranctl.ParseNFList(nfList))
			if err != nil {
				slog.Error("ranctl init", "err", err)
				return
			}
			ranctl.NewHandler(svc).Mount(mux)
		},
	})
}
