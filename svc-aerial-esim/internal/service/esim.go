// Package service holds eSIM business logic: catalog refresh, order, usage poll.
package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/provider"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/repository"
	qrcode "github.com/skip2/go-qrcode"
)

// ESIM is the service.
type ESIM struct {
	repo *repository.Postgres
	prov provider.Provider
}

// New wires the service.
func New(repo *repository.Postgres, prov provider.Provider) *ESIM {
	return &ESIM{repo: repo, prov: prov}
}

// RefreshCatalog fetches the provider catalog for a region and upserts it.
func (s *ESIM) RefreshCatalog(ctx context.Context, region string) ([]model.Package, error) {
	offers, err := s.prov.Catalog(ctx, region)
	if err != nil {
		return nil, errors.Join(model.ErrProvider, err)
	}
	now := time.Now()
	pkgs := make([]model.Package, 0, len(offers))
	for _, o := range offers {
		pkgs = append(pkgs, model.Package{
			ID: o.ID, Provider: s.prov.Name(), Label: o.Label, Region: o.Region,
			DataMB: o.DataMB, ValidityDays: o.ValidityDays, PriceUSDCents: o.PriceUSDCents,
			FetchedAt: now,
		})
	}
	if err := s.repo.UpsertPackages(ctx, pkgs); err != nil {
		return nil, err
	}
	return pkgs, nil
}

// ListPackages returns the cached catalog.
func (s *ESIM) ListPackages(ctx context.Context, region string) ([]model.Package, error) {
	return s.repo.ListPackages(ctx, region)
}

// Order creates an eSIM via the provider, stores it, renders a QR PNG, returns it.
func (s *ESIM) Order(ctx context.Context, orgID string, req model.OrderRequest) (*model.ESIM, error) {
	if strings.TrimSpace(orgID) == "" {
		return nil, errors.Join(model.ErrBadInput, errors.New("org_id required"))
	}
	pkg, err := s.repo.GetPackage(ctx, req.PackageID)
	if err != nil {
		return nil, err
	}

	externalRef := "aerial-" + orgID
	res, err := s.prov.Order(ctx, pkg.ID, externalRef)
	if err != nil {
		return nil, errors.Join(model.ErrProvider, err)
	}

	// Render QR PNG for the LPA string so the UI can show a scannable code.
	var qrB64 string
	if res.LPAString != "" {
		if png, err := qrcode.Encode(res.LPAString, qrcode.Medium, 320); err == nil {
			qrB64 = base64.StdEncoding.EncodeToString(png)
		}
	}

	e := &model.ESIM{
		OrgID:        orgID,
		OwnerUserID:  req.OwnerUserID,
		Provider:     s.prov.Name(),
		ProviderRef:  strPtr(res.ProviderRef),
		ICCID:        strPtr(res.ICCID),
		PackageID:    strPtr(pkg.ID),
		PackageLabel: strPtr(pkg.Label),
		DataMB:       intPtr(pkg.DataMB),
		ValidityDays: intPtr(pkg.ValidityDays),
		LPAString:    strPtr(res.LPAString),
		QRPNGBase64:  strPtr(qrB64),
		InstallURL:   strPtr(res.InstallURL),
		Status:       "ordered",
		ExpiresAt:    res.ExpiresAt,
	}
	return s.repo.CreateESIM(ctx, e)
}

// Get returns one eSIM.
func (s *ESIM) Get(ctx context.Context, id string) (*model.ESIM, error) {
	return s.repo.GetESIM(ctx, id)
}

// ListByOrg returns the org's eSIMs.
func (s *ESIM) ListByOrg(ctx context.Context, orgID string) ([]*model.ESIM, error) {
	return s.repo.ListByOrg(ctx, orgID)
}

// RefreshUsage queries the provider and persists the latest usage_mb.
func (s *ESIM) RefreshUsage(ctx context.Context, id string) (*model.ESIM, error) {
	e, err := s.repo.GetESIM(ctx, id)
	if err != nil {
		return nil, err
	}
	// Some providers key usage by ICCID, some by providerRef. Mock uses providerRef.
	key := ""
	if e.ProviderRef != nil {
		key = *e.ProviderRef
	}
	if e.ICCID != nil && s.prov.Name() == "airalo" {
		key = *e.ICCID
	}
	usage, err := s.prov.UsageMB(ctx, key)
	if err != nil {
		return e, errors.Join(model.ErrProvider, err)
	}
	if err := s.repo.UpdateUsage(ctx, id, usage); err != nil {
		return nil, err
	}
	return s.repo.GetESIM(ctx, id)
}

// MarkActivated flips status to active (called when the user reports the QR scanned).
func (s *ESIM) MarkActivated(ctx context.Context, id string) error {
	return s.repo.SetStatus(ctx, id, "active")
}

// Cancel calls the provider Cancel + marks cancelled locally.
func (s *ESIM) Cancel(ctx context.Context, id string) error {
	e, err := s.repo.GetESIM(ctx, id)
	if err != nil {
		return err
	}
	if e.ProviderRef != nil {
		_ = s.prov.Cancel(ctx, *e.ProviderRef)
	}
	return s.repo.SetStatus(ctx, id, "cancelled")
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func intPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}
