// Package config loads svc-aerial-esim env settings.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port              string
	DatabaseURL       string
	JWTSecret         string
	JWTIssuer         string
	JWTAudience       string
	OTelEndpoint      string
	CORSOrigins       string

	// Provider selection — "mock" when keys are empty, otherwise "airalo".
	AiraloBaseURL     string
	AiraloClientID    string
	AiraloClientSecret string
}

func Load() (*Config, error) {
	c := &Config{
		Port:               envOr("PORT", "8083"),
		DatabaseURL:        envOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=esim"),
		JWTSecret:          envOr("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:          envOr("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:        envOr("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		CORSOrigins:        envOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
		AiraloBaseURL:      envOr("AIRALO_BASE_URL", "https://sandbox-partners-api.airalo.com"),
		AiraloClientID:     os.Getenv("AIRALO_CLIENT_ID"),
		AiraloClientSecret: os.Getenv("AIRALO_CLIENT_SECRET"),
	}
	if len(c.JWTSecret) < 16 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 16 chars")
	}
	return c, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
