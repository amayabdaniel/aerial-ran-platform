// Package provider is the eSIM-vendor adapter interface.
// Real adapters (Airalo, EMnify) implement this; Mock is the no-keys fallback.
package provider

import (
	"context"
	"time"
)

// PackageOffering is one SKU returned by the catalog endpoint.
type PackageOffering struct {
	ID            string
	Label         string
	Region        string
	DataMB        int
	ValidityDays  int
	PriceUSDCents int
	Raw           any
}

// OrderResult is what an adapter returns after issuing an eSIM.
type OrderResult struct {
	ProviderRef string
	ICCID       string
	LPAString   string     // LPA:1$smdp-host$matchingId  → goes into a QR
	QRPNGBase64 string     // optional: pre-rendered PNG; otherwise UI renders LPA
	InstallURL  string     // iOS 17.4+ Universal Link
	ExpiresAt   *time.Time
}

// Provider is the adapter contract.
type Provider interface {
	Name() string
	Catalog(ctx context.Context, region string) ([]PackageOffering, error)
	Order(ctx context.Context, packageID, externalRef string) (*OrderResult, error)
	UsageMB(ctx context.Context, providerRef string) (int, error)
	Cancel(ctx context.Context, providerRef string) error
}
