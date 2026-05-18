// Package service holds IAM business logic: signup, login, token issuance,
// refresh-token rotation with reuse detection, and logout.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/model"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/password"
	"github.com/amayabdaniel/aerial-ran-platform/svc-aerial-iam/internal/repository"
	"github.com/google/uuid"
)

// IAM orchestrates auth flows over the repository and the JWT issuer.
type IAM struct {
	repo       *repository.Postgres
	jwt        *jwt.Issuer
	refreshTTL time.Duration
}

// New wires the service.
func New(repo *repository.Postgres, issuer *jwt.Issuer, refreshTTL time.Duration) *IAM {
	return &IAM{repo: repo, jwt: issuer, refreshTTL: refreshTTL}
}

// Signup creates an org + user pair. First signup becomes superadmin.
func (s *IAM) Signup(ctx context.Context, req model.SignupRequest) (*model.User, error) {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return nil, errors.Join(model.ErrInvalidArgument, errors.New("bad email"))
	}
	if len(req.Password) < 8 {
		return nil, errors.Join(model.ErrInvalidArgument, errors.New("password too short"))
	}

	if existing, _ := s.repo.GetUserByEmail(ctx, req.Email); existing != nil {
		return nil, model.ErrUserExists
	}

	orgName := strings.TrimSpace(req.OrgName)
	if orgName == "" {
		orgName = req.Email
	}
	orgSlug := strings.TrimSpace(req.OrgSlug)
	if orgSlug == "" {
		orgSlug = slugify(orgName)
	}

	org, err := s.repo.GetOrgBySlug(ctx, orgSlug)
	if errors.Is(err, model.ErrOrgNotFound) {
		org, err = s.repo.CreateOrg(ctx, orgName, orgSlug)
	}
	if err != nil {
		return nil, err
	}

	hash, err := password.Hash(req.Password)
	if err != nil {
		return nil, err
	}

	role := model.RoleSuperadmin // first user in the org owns it
	u := &model.User{
		OrgID:        org.ID,
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		PasswordHash: hash,
		Role:         role,
	}
	u, err = s.repo.CreateUser(ctx, u)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Login authenticates and returns a token pair.
func (s *IAM) Login(ctx context.Context, req model.LoginRequest) (*model.TokenPair, error) {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	u, err := s.repo.GetUserByEmail(ctx, req.Email)
	if errors.Is(err, model.ErrUserNotFound) {
		return nil, model.ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	if !u.IsActive {
		return nil, model.ErrBadCredentials
	}
	if err := password.Check(u.PasswordHash, req.Password); err != nil {
		return nil, model.ErrBadCredentials
	}

	deviceID := ""
	if req.DeviceFingerprint != "" {
		d, err := s.repo.UpsertDevice(ctx, u.ID, req.DeviceFingerprint, req.DeviceName)
		if err != nil {
			return nil, err
		}
		deviceID = d.ID
	}

	return s.issueTokens(ctx, u, deviceID, uuid.NewString())
}

// Refresh rotates the refresh token. If the presented token is already-revoked
// (replay), the whole family is revoked.
func (s *IAM) Refresh(ctx context.Context, raw, deviceFingerprint string) (*model.TokenPair, error) {
	h := hashToken(raw)
	rt, err := s.repo.FindRefreshToken(ctx, h)
	if errors.Is(err, model.ErrTokenRevoked) {
		// reuse detection: revoke the entire family
		if rt != nil {
			_ = s.repo.RevokeFamily(ctx, rt.FamilyID)
		}
		return nil, model.ErrTokenReuse
	}
	if err != nil {
		return nil, err
	}

	if err := s.repo.RevokeToken(ctx, h); err != nil {
		return nil, err
	}

	u, err := s.repo.GetUserByID(ctx, rt.UserID)
	if err != nil {
		return nil, err
	}

	// keep the same family across rotation; new device row if fingerprint changed
	deviceID := rt.DeviceID
	if deviceFingerprint != "" {
		d, err := s.repo.UpsertDevice(ctx, u.ID, deviceFingerprint, "")
		if err != nil {
			return nil, err
		}
		deviceID = d.ID
	}

	return s.issueTokens(ctx, u, deviceID, rt.FamilyID)
}

// Logout revokes the presented refresh token.
func (s *IAM) Logout(ctx context.Context, raw string) error {
	return s.repo.RevokeToken(ctx, hashToken(raw))
}

// Me returns the user behind a verified JWT.
func (s *IAM) Me(ctx context.Context, userID string) (*model.User, error) {
	return s.repo.GetUserByID(ctx, userID)
}

func (s *IAM) issueTokens(ctx context.Context, u *model.User, deviceID, familyID string) (*model.TokenPair, error) {
	access, accessExp, err := s.jwt.Sign(jwt.Claims{
		UserID:   u.ID,
		OrgID:    u.OrgID,
		Email:    u.Email,
		Role:     u.Role,
		DeviceID: deviceID,
	})
	if err != nil {
		return nil, err
	}

	refresh, refreshHash := newRefreshToken()
	refreshExp := time.Now().Add(s.refreshTTL)
	if err := s.repo.StoreRefreshToken(ctx, &model.RefreshToken{
		UserID:    u.ID,
		DeviceID:  deviceID,
		FamilyID:  familyID,
		TokenHash: refreshHash,
		ExpiresAt: refreshExp,
	}); err != nil {
		return nil, err
	}

	return &model.TokenPair{
		AccessToken:      access,
		AccessExpiresAt:  accessExp,
		RefreshToken:     refresh,
		RefreshExpiresAt: refreshExp,
		User:             u,
	}, nil
}

// newRefreshToken returns (plaintext, sha256-hex hash).
func newRefreshToken() (string, string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r == ' ', r == '_', r == '-', r == '.':
			out = append(out, '-')
		}
	}
	return strings.Trim(string(out), "-")
}
