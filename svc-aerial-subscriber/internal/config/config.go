// Package config loads svc-aerial-subscriber env settings.
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
	OpenMongoURI string // mongodb://… for Open5GS subscriber DB
	OpenMongoDB  string // typically "open5gs"
	PLMNMcc      string
	PLMNMnc      string
	CORSOrigins  string
}

func Load() (*Config, error) {
	c := &Config{
		Port:         env.Or("PORT", "8082"),
		DatabaseURL:  env.Or("DATABASE_URL", "postgres://aerial_admin:aerial_dev_pass_change_me@localhost:15432/aerial?sslmode=disable&search_path=subscriber"),
		JWTSecret:    env.Or("JWT_SECRET", "dev-secret-change-in-production-32ch"),
		JWTIssuer:    env.Or("JWT_ISSUER", "aerial-ran-platform"),
		JWTAudience:  env.Or("JWT_AUDIENCE", "aerial-clients"),
		OTelEndpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		OpenMongoURI: env.Or("OPEN5GS_MONGO_URI", "mongodb://localhost:27017"),
		OpenMongoDB:  env.Or("OPEN5GS_MONGO_DB", "open5gs"),
		PLMNMcc:      env.Or("PLMN_MCC", "999"),
		PLMNMnc:      env.Or("PLMN_MNC", "70"),
		CORSOrigins:  env.Or("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:8080"),
	}
	if len(c.JWTSecret) < 16 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 16 chars")
	}
	return c, nil
}
