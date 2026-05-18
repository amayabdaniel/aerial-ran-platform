// Package config loads service config from env, with safe defaults for dev.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config is everything the service needs at boot.
type Config struct {
	Port             string
	DatabaseURL      string
	JWTSecret        string
	JWTIssuer        string
	JWTAudience      string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	OTelEndpoint     string
	CORSOrigins      string
}

// Load reads from env. Returns an error only on truly invalid input.
func Load() (*Config, error) {
	c := &Config{
		Port:            envOr("PORT", "8081"),
		DatabaseURL:     envOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=iam"),
		JWTSecret:       envOr("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:       envOr("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:     envOr("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint:    os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		CORSOrigins:     envOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
	}
	var err error
	if c.AccessTokenTTL, err = time.ParseDuration(envOr("JWT_ACCESS_TTL", "15m")); err != nil {
		return nil, fmt.Errorf("JWT_ACCESS_TTL: %w", err)
	}
	if c.RefreshTokenTTL, err = time.ParseDuration(envOr("JWT_REFRESH_TTL", "720h")); err != nil {
		return nil, fmt.Errorf("JWT_REFRESH_TTL: %w", err)
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
