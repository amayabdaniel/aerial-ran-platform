// Package config loads svc-aerial-subscriber env settings.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port         string
	DatabaseURL  string
	JWTSecret    string
	JWTIssuer    string
	JWTAudience  string
	OTelEndpoint string
	OpenMongoURI string // mongodb://… for Open5GS subscriber DB
	OpenMongoDB  string // typically "open5gs"
	PLMNMcc      string
	PLMNMnc      string
	CORSOrigins  string
}

func Load() (*Config, error) {
	c := &Config{
		Port:         envOr("PORT", "8082"),
		DatabaseURL:  envOr("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=subscriber"),
		JWTSecret:    envOr("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:    envOr("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:  envOr("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OpenMongoURI: envOr("OPEN5GS_MONGO_URI", "mongodb://localhost:27017"),
		OpenMongoDB:  envOr("OPEN5GS_MONGO_DB", "open5gs"),
		PLMNMcc:      envOr("PLMN_MCC", "999"),
		PLMNMnc:      envOr("PLMN_MNC", "70"),
		CORSOrigins:  envOr("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
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
