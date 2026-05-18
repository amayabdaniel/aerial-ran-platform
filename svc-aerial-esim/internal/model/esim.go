// Package model holds eSIM domain types + sentinel errors.
package model

import (
	"errors"
	"time"
)

var (
	ErrPackageNotFound = errors.New("package not found")
	ErrESIMNotFound    = errors.New("esim not found")
	ErrProvider        = errors.New("provider error")
	ErrBadInput        = errors.New("bad input")
)

// Package is a provider's eSIM package SKU (5GB/30d for Colombia, etc.).
type Package struct {
	ID            string    `json:"id"`
	Provider      string    `json:"provider"`
	Label         string    `json:"label"`
	Region        string    `json:"region"`
	DataMB        int       `json:"data_mb"`
	ValidityDays  int       `json:"validity_days"`
	PriceUSDCents int       `json:"price_usd_cents"`
	FetchedAt     time.Time `json:"fetched_at"`
}

// ESIM is an issued eSIM.
type ESIM struct {
	ID            string     `json:"id"`
	OrgID         string     `json:"org_id"`
	OwnerUserID   *string    `json:"owner_user_id,omitempty"`
	Provider      string     `json:"provider"`
	ProviderRef   *string    `json:"provider_ref,omitempty"`
	ICCID         *string    `json:"iccid,omitempty"`
	PackageID     *string    `json:"package_id,omitempty"`
	PackageLabel  *string    `json:"package_label,omitempty"`
	DataMB        *int       `json:"data_mb,omitempty"`
	ValidityDays  *int       `json:"validity_days,omitempty"`
	LPAString     *string    `json:"lpa_string,omitempty"`
	QRPNGBase64   *string    `json:"qr_png_b64,omitempty"`
	InstallURL    *string    `json:"install_url,omitempty"`
	Status        string     `json:"status"`
	ActivatedAt   *time.Time `json:"activated_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	LastUsageMB   int        `json:"last_usage_mb"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// OrderRequest from the API.
type OrderRequest struct {
	OwnerUserID *string `json:"owner_user_id,omitempty"`
	PackageID   string  `json:"package_id"`
}
