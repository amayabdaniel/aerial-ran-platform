// Package repository implements PostgreSQL persistence for the IAM service.
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres holds a pgx pool — all repository methods are on this type.
type Postgres struct {
	pool *pgxpool.Pool
}

// New returns a Postgres-backed repository.
func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

// CreateOrg inserts a new organization and returns the row.
func (p *Postgres) CreateOrg(ctx context.Context, name, slug string) (*model.Organization, error) {
	o := &model.Organization{}
	err := p.pool.QueryRow(ctx,
		`INSERT INTO iam.organizations(name, slug) VALUES ($1, $2)
		 RETURNING id::text, name, slug, COALESCE(modules, '[]'::jsonb)::text, created_at`,
		name, slug,
	).Scan(&o.ID, &o.Name, &o.Slug, new(string), &o.CreatedAt)
	if err != nil {
		return nil, err
	}
	o.Modules = []string{}
	return o, nil
}

// GetOrgBySlug looks up by slug.
func (p *Postgres) GetOrgBySlug(ctx context.Context, slug string) (*model.Organization, error) {
	o := &model.Organization{Modules: []string{}}
	err := p.pool.QueryRow(ctx,
		`SELECT id::text, name, slug, created_at FROM iam.organizations WHERE slug = $1`, slug,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrOrgNotFound
	}
	return o, err
}

// CreateUser inserts a user.
func (p *Postgres) CreateUser(ctx context.Context, u *model.User) (*model.User, error) {
	err := p.pool.QueryRow(ctx,
		`INSERT INTO iam.users(org_id, email, display_name, password_hash, role)
		 VALUES ($1::uuid, $2, $3, $4, $5)
		 RETURNING id::text, created_at, updated_at`,
		u.OrgID, u.Email, u.DisplayName, u.PasswordHash, u.Role,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	u.IsActive = true
	return u, nil
}

// GetUserByEmail returns the user or ErrUserNotFound.
func (p *Postgres) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := p.pool.QueryRow(ctx,
		`SELECT id::text, org_id::text, email, COALESCE(display_name, ''), password_hash, role, is_active, created_at, updated_at
		   FROM iam.users WHERE email = $1`, email,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrUserNotFound
	}
	return u, err
}

// GetUserByID returns the user.
func (p *Postgres) GetUserByID(ctx context.Context, id string) (*model.User, error) {
	u := &model.User{}
	err := p.pool.QueryRow(ctx,
		`SELECT id::text, org_id::text, email, COALESCE(display_name, ''), password_hash, role, is_active, created_at, updated_at
		   FROM iam.users WHERE id = $1::uuid`, id,
	).Scan(&u.ID, &u.OrgID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrUserNotFound
	}
	return u, err
}

// UpsertDevice creates or updates a device fingerprint for a user.
func (p *Postgres) UpsertDevice(ctx context.Context, userID, fingerprint, name string) (*model.Device, error) {
	d := &model.Device{}
	now := time.Now()
	err := p.pool.QueryRow(ctx,
		`INSERT INTO iam.devices(user_id, fingerprint, device_name, last_seen)
		 VALUES ($1::uuid, $2, $3, $4)
		 ON CONFLICT (user_id, fingerprint) DO UPDATE SET last_seen = EXCLUDED.last_seen
		 RETURNING id::text, user_id::text, COALESCE(device_name,''), fingerprint, last_seen, created_at`,
		userID, fingerprint, name, now,
	).Scan(&d.ID, &d.UserID, &d.DeviceName, &d.Fingerprint, &d.LastSeen, &d.CreatedAt)
	return d, err
}

// StoreRefreshToken persists a hashed refresh token.
func (p *Postgres) StoreRefreshToken(ctx context.Context, t *model.RefreshToken) error {
	return p.pool.QueryRow(ctx,
		`INSERT INTO iam.refresh_tokens(user_id, device_id, family_id, token_hash, expires_at)
		 VALUES ($1::uuid, NULLIF($2,'')::uuid, $3::uuid, $4, $5)
		 RETURNING id::text, created_at`,
		t.UserID, t.DeviceID, t.FamilyID, t.TokenHash, t.ExpiresAt,
	).Scan(&t.ID, &t.CreatedAt)
}

// FindRefreshToken returns the row matching the hashed token or sentinel errors.
func (p *Postgres) FindRefreshToken(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	rt := &model.RefreshToken{}
	err := p.pool.QueryRow(ctx,
		`SELECT id::text, user_id::text, COALESCE(device_id::text, ''), family_id::text, token_hash, expires_at, revoked_at, created_at
		   FROM iam.refresh_tokens WHERE token_hash = $1`, tokenHash,
	).Scan(&rt.ID, &rt.UserID, &rt.DeviceID, &rt.FamilyID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrTokenNotFound
	}
	if err != nil {
		return nil, err
	}
	if rt.RevokedAt != nil {
		return rt, model.ErrTokenRevoked
	}
	if time.Now().After(rt.ExpiresAt) {
		return rt, model.ErrTokenExpired
	}
	return rt, nil
}

// RevokeFamily revokes all refresh tokens in a family (reuse detection).
func (p *Postgres) RevokeFamily(ctx context.Context, familyID string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE iam.refresh_tokens SET revoked_at = now() WHERE family_id = $1::uuid AND revoked_at IS NULL`,
		familyID,
	)
	return err
}

// RevokeToken revokes a single refresh token (e.g. logout).
func (p *Postgres) RevokeToken(ctx context.Context, tokenHash string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE iam.refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		tokenHash,
	)
	return err
}
