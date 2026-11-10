// Package config loads svc-aerial-esim env settings.
package config

import (
	"fmt"
	"os"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/env"
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
		Port:               env.Or("PORT", "8083"),
		DatabaseURL:        env.Or("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=esim"),
		JWTSecret:          env.Or("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:          env.Or("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:        env.Or("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		CORSOrigins:        env.Or("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		AiraloBaseURL:      env.Or("AIRALO_BASE_URL", "https://sandbox-partners-api.airalo.com"),
		AiraloClientID:     os.Getenv("AIRALO_CLIENT_ID"),
		AiraloClientSecret: os.Getenv("AIRALO_CLIENT_SECRET"),
	}
	if len(c.JWTSecret) < 16 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 16 chars")
	}
	return c, nil
}
