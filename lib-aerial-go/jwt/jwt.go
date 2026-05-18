// Package jwt issues and verifies HS256 access tokens with our standard claims.
// Verification helpers + middleware are reusable across all aerial services.
package jwt

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	jwt5 "github.com/golang-jwt/jwt/v5"
)

// Claims is the standard claim set carried by aerial JWTs.
type Claims struct {
	UserID   string   `json:"sub"`
	OrgID    string   `json:"org"`
	Email    string   `json:"email"`
	Role     string   `json:"role"`
	DeviceID string   `json:"did,omitempty"`
	Scopes   []string `json:"scp,omitempty"`
	jwt5.RegisteredClaims
}

// Issuer signs and verifies tokens with one HS256 secret + standard issuer/audience.
type Issuer struct {
	secret   []byte
	issuer   string
	audience string
	ttl      time.Duration
}

// New returns an Issuer. ttl is the access token lifetime.
func New(secret, issuer, audience string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), issuer: issuer, audience: audience, ttl: ttl}
}

// Sign returns a signed access token for the given claims.
func (i *Issuer) Sign(c Claims) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(i.ttl)
	c.RegisteredClaims = jwt5.RegisteredClaims{
		Issuer:    i.issuer,
		Audience:  jwt5.ClaimStrings{i.audience},
		IssuedAt:  jwt5.NewNumericDate(now),
		NotBefore: jwt5.NewNumericDate(now),
		ExpiresAt: jwt5.NewNumericDate(exp),
	}
	t := jwt5.NewWithClaims(jwt5.SigningMethodHS256, &c)
	s, err := t.SignedString(i.secret)
	return s, exp, err
}

// Verify parses and validates the token. Returns claims on success.
func (i *Issuer) Verify(token string) (*Claims, error) {
	c := &Claims{}
	tok, err := jwt5.ParseWithClaims(token, c, func(t *jwt5.Token) (any, error) {
		if t.Method.Alg() != jwt5.SigningMethodHS256.Alg() {
			return nil, errors.New("bad alg")
		}
		return i.secret, nil
	}, jwt5.WithIssuer(i.issuer), jwt5.WithAudience(i.audience), jwt5.WithLeeway(30*time.Second))
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return c, nil
}

// ctxKey is unexported to prevent collisions.
type ctxKey int

const claimsKey ctxKey = 1

// FromContext returns claims set by Middleware.
func FromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

// Middleware verifies Bearer tokens and stashes Claims in the request context.
// Skips paths in skip (eg /v1/health, /v1/auth/login, /metrics).
func (i *Issuer) Middleware(skip ...string) func(http.Handler) http.Handler {
	skipSet := map[string]struct{}{}
	for _, s := range skip {
		skipSet[s] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skipSet[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				http.Error(w, `{"code":"unauthorized","message":"missing bearer token"}`, http.StatusUnauthorized)
				return
			}
			claims, err := i.Verify(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				http.Error(w, `{"code":"unauthorized","message":"invalid token"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
