// Package config loads svc-aerial-esim env settings.
package config

import (
	"fmt"
	"os"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
)

type Config struct {
	Port         string
	DatabaseURL  string
	JWTSecret    string
	JWTIssuer    string
	JWTAudience  string
	OTelEndpoint string
	CORSOrigins  string

	// Provider selection — "mock" when keys are empty, otherwise "airalo".
	AiraloBaseURL      string
	AiraloClientID     string
	AiraloClientSecret string
}

func Load() (*Config, error) {
	c := &Config{
		Port:               runner.EnvOr("PORT", "8083"),
		DatabaseURL:        runner.EnvOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=esim"),
		JWTSecret:          runner.EnvOr("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:          runner.EnvOr("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:        runner.EnvOr("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		CORSOrigins:        runner.EnvOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		AiraloBaseURL:      runner.EnvOr("AIRALO_BASE_URL", "https://sandbox-partners-api.airalo.com"),
		AiraloClientID:     os.Getenv("AIRALO_CLIENT_ID"),
		AiraloClientSecret: os.Getenv("AIRALO_CLIENT_SECRET"),
	}
	if len(c.JWTSecret) < 16 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 16 chars")
	}
	return c, nil
}
