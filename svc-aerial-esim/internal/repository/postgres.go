// Package repository persists eSIMs and the cached provider catalog.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-esim/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

// UpsertPackages stores the latest catalog snapshot.
func (p *Postgres) UpsertPackages(ctx context.Context, pkgs []model.Package) error {
	if len(pkgs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, k := range pkgs {
		batch.Queue(
			`INSERT INTO esim.packages(id, provider, label, region, data_mb, validity_days, price_usd_cents, fetched_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (id) DO UPDATE SET label=EXCLUDED.label, region=EXCLUDED.region,
			   data_mb=EXCLUDED.data_mb, validity_days=EXCLUDED.validity_days,
			   price_usd_cents=EXCLUDED.price_usd_cents, fetched_at=EXCLUDED.fetched_at`,
			k.ID, k.Provider, k.Label, k.Region, k.DataMB, k.ValidityDays, k.PriceUSDCents, k.FetchedAt,
		)
	}
	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range pkgs {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// ListPackages returns the cached catalog (optionally filtered).
func (p *Postgres) ListPackages(ctx context.Context, region string) ([]model.Package, error) {
	q := `SELECT id, provider, label, COALESCE(region,''), data_mb, validity_days, price_usd_cents, fetched_at
	        FROM esim.packages`
	args := []any{}
	if region != "" {
		q += ` WHERE region = $1`
		args = append(args, region)
	}
	q += ` ORDER BY price_usd_cents`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.Package, 0, 16)
	for rows.Next() {
		var k model.Package
		if err := rows.Scan(&k.ID, &k.Provider, &k.Label, &k.Region, &k.DataMB, &k.ValidityDays, &k.PriceUSDCents, &k.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// GetPackage fetches one package.
func (p *Postgres) GetPackage(ctx context.Context, id string) (*model.Package, error) {
	k := &model.Package{}
	err := p.pool.QueryRow(ctx,
		`SELECT id, provider, label, COALESCE(region,''), data_mb, validity_days, price_usd_cents, fetched_at
		   FROM esim.packages WHERE id = $1`, id,
	).Scan(&k.ID, &k.Provider, &k.Label, &k.Region, &k.DataMB, &k.ValidityDays, &k.PriceUSDCents, &k.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrPackageNotFound
	}
	return k, err
}

// CreateESIM inserts a freshly-issued eSIM.
func (p *Postgres) CreateESIM(ctx context.Context, e *model.ESIM) (*model.ESIM, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO esim.esims(org_id, owner_user_id, provider, provider_ref, iccid, package_id, package_label,
		                         data_mb, validity_days, lpa_string, qr_png_b64, install_url, status, expires_at)
		 VALUES ($1::uuid, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		 RETURNING id::text, created_at, updated_at`,
		e.OrgID, deref(e.OwnerUserID), e.Provider, deref(e.ProviderRef), deref(e.ICCID),
		deref(e.PackageID), deref(e.PackageLabel), derefInt(e.DataMB), derefInt(e.ValidityDays),
		deref(e.LPAString), deref(e.QRPNGBase64), deref(e.InstallURL), e.Status, e.ExpiresAt,
	).Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

// GetESIM by id.
func (p *Postgres) GetESIM(ctx context.Context, id string) (*model.ESIM, error) {
	e := &model.ESIM{}
	err := p.pool.QueryRow(ctx,
		`SELECT id::text, org_id::text, owner_user_id::text, provider, provider_ref, iccid, package_id, package_label,
		        data_mb, validity_days, lpa_string, qr_png_b64, install_url, status,
		        activated_at, expires_at, last_usage_mb, created_at, updated_at
		   FROM esim.esims WHERE id = $1::uuid`, id,
	).Scan(&e.ID, &e.OrgID, &e.OwnerUserID, &e.Provider, &e.ProviderRef, &e.ICCID, &e.PackageID, &e.PackageLabel,
		&e.DataMB, &e.ValidityDays, &e.LPAString, &e.QRPNGBase64, &e.InstallURL, &e.Status,
		&e.ActivatedAt, &e.ExpiresAt, &e.LastUsageMB, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrESIMNotFound
	}
	return e, err
}

// ListByOrg returns the org's eSIMs.
func (p *Postgres) ListByOrg(ctx context.Context, orgID string) ([]*model.ESIM, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id::text, org_id::text, owner_user_id::text, provider, provider_ref, iccid, package_id, package_label,
		        data_mb, validity_days, lpa_string, qr_png_b64, install_url, status,
		        activated_at, expires_at, last_usage_mb, created_at, updated_at
		   FROM esim.esims WHERE org_id = $1::uuid ORDER BY created_at DESC LIMIT 200`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*model.ESIM, 0, 16)
	for rows.Next() {
		e := &model.ESIM{}
		if err := rows.Scan(&e.ID, &e.OrgID, &e.OwnerUserID, &e.Provider, &e.ProviderRef, &e.ICCID, &e.PackageID, &e.PackageLabel,
			&e.DataMB, &e.ValidityDays, &e.LPAString, &e.QRPNGBase64, &e.InstallURL, &e.Status,
			&e.ActivatedAt, &e.ExpiresAt, &e.LastUsageMB, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpdateUsage stores the latest usage_mb (idempotent).
func (p *Postgres) UpdateUsage(ctx context.Context, id string, usageMB int) error {
	res, err := p.pool.Exec(ctx,
		`UPDATE esim.esims SET last_usage_mb = $2, updated_at = now() WHERE id = $1::uuid`, id, usageMB)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return model.ErrESIMNotFound
	}
	return nil
}

// SetStatus changes the lifecycle status.
func (p *Postgres) SetStatus(ctx context.Context, id, status string) error {
	res, err := p.pool.Exec(ctx,
		`UPDATE esim.esims SET status = $2, updated_at = now(),
		    activated_at = CASE WHEN $2 = 'active' AND activated_at IS NULL THEN now() ELSE activated_at END
		   WHERE id = $1::uuid`, id, status)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return model.ErrESIMNotFound
	}
	return nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// quiet unused imports if helpers are skipped
var _ = time.Second
