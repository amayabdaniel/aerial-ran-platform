// svc-aerial-subscriber — SIM/SUPI/IMSI registry + Open5GS MongoDB provisioning.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/config"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/handler"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/open5gs"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/repository"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-subscriber/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	// Open5GS MongoDB provisioning is optional: connect once at startup, and on
	// failure start without the hook (mongo may legitimately be absent). The
	// client is created here, not inside Mount, so its graceful Close is deferred
	// for the whole server lifetime — runner.Run blocks until shutdown, after
	// which this defer runs. The connect error is logged inside Mount so it uses
	// the JSON logger runner installs.
	mongoClient, mongoErr := open5gs.New(context.Background(), cfg.OpenMongoURI, cfg.OpenMongoDB)
	if mongoErr == nil {
		defer func() { _ = mongoClient.Close(context.Background()) }()
	}

	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-subscriber",
		Port:         cfg.Port,
		DatabaseURL:  cfg.DatabaseURL,
		JWTSecret:    cfg.JWTSecret,
		JWTIssuer:    cfg.JWTIssuer,
		JWTAudience:  cfg.JWTAudience,
		OTelEndpoint: cfg.OTelEndpoint,
		CORSOrigins:  cfg.CORSOrigins,
		Mount: func(_ context.Context, mux *http.ServeMux, pool *pgxpool.Pool, _ *jwtlib.Issuer) {
			if mongoErr != nil {
				slog.Warn("open5gs mongo connect failed; will start without provisioning hook", "err", mongoErr)
			}
			repo := repository.New(pool)
			svc := service.New(repo, mongoClient, cfg.PLMNMcc, cfg.PLMNMnc)
			handler.New(svc).Mount(mux)
		},
	})
}
