// svc-aerial-esim — Airalo / EMnify / mock adapter; LPA QR issuance; usage polling.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/config"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/handler"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/provider"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/repository"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	runner.Run(runner.Opts{
		ServiceName:  "svc-aerial-esim",
		Port:         cfg.Port,
		DatabaseURL:  cfg.DatabaseURL,
		JWTSecret:    cfg.JWTSecret,
		JWTIssuer:    cfg.JWTIssuer,
		JWTAudience:  cfg.JWTAudience,
		OTelEndpoint: cfg.OTelEndpoint,
		CORSOrigins:  cfg.CORSOrigins,
		Mount: func(_ context.Context, mux *http.ServeMux, pool *pgxpool.Pool, _ *jwtlib.Issuer) {
			svc := service.New(repository.New(pool), selectProvider(cfg))
			handler.New(svc).Mount(mux)
		},
	})
}

// selectProvider returns the real Airalo client when both credentials are set,
// otherwise the mock, logging which was chosen.
func selectProvider(cfg *config.Config) provider.Provider {
	if cfg.AiraloClientID != "" && cfg.AiraloClientSecret != "" {
		p := provider.NewAiralo(cfg.AiraloBaseURL, cfg.AiraloClientID, cfg.AiraloClientSecret)
		slog.Info("provider", "name", p.Name(), "base_url", cfg.AiraloBaseURL)
		return p
	}
	p := provider.NewMock()
	slog.Warn("provider", "name", p.Name(), "reason", "AIRALO_CLIENT_ID/SECRET not set; using mock")
	return p
}
