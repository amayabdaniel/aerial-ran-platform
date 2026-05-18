// Package model holds domain types and sentinel errors for the IAM service.
package model

import (
	"errors"
	"time"
)

// Sentinel errors — handlers map these to HTTP codes.
var (
	ErrUserNotFound    = errors.New("user not found")
	ErrUserExists      = errors.New("user already exists")
	ErrOrgNotFound     = errors.New("org not found")
	ErrBadCredentials  = errors.New("invalid email or password")
	ErrTokenNotFound   = errors.New("refresh token not found")
	ErrTokenRevoked    = errors.New("refresh token revoked")
	ErrTokenExpired    = errors.New("refresh token expired")
	ErrTokenReuse      = errors.New("refresh token reuse detected; family revoked")
	ErrInvalidArgument = errors.New("invalid argument")
)

// Role values mapped to JWT `role` claim.
const (
	RoleSuperadmin = "superadmin"
	RoleAdmin      = "admin"
	RoleUser       = "user"
)

// Organization (tenant).
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Modules   []string  `json:"modules"`
	CreatedAt time.Time `json:"created_at"`
}

// User account.
type User struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	IsActive     bool      `json:"is_active"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Device represents a user agent bound to refresh-token families.
type Device struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	DeviceName  string     `json:"device_name"`
	Fingerprint string     `json:"fingerprint"`
	LastSeen    *time.Time `json:"last_seen,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// RefreshToken is stored hashed; the plaintext only lives in the response body.
type RefreshToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	DeviceID   string     `json:"device_id"`
	FamilyID   string     `json:"family_id"`
	TokenHash  string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// SignupRequest from the API.
type SignupRequest struct {
	Email       string `json:"email"       validate:"required,email"`
	Password    string `json:"password"    validate:"required,min=8"`
	DisplayName string `json:"display_name"`
	OrgName     string `json:"org_name"`
	OrgSlug     string `json:"org_slug"`
}

// LoginRequest from the API.
type LoginRequest struct {
	Email             string `json:"email"`
	Password          string `json:"password"`
	DeviceFingerprint string `json:"device_fingerprint"`
	DeviceName        string `json:"device_name"`
}

// RefreshRequest carries the plaintext refresh token.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// TokenPair returned by login/refresh.
type TokenPair struct {
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	User             *User     `json:"user"`
}
