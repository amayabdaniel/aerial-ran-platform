package jwt

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newIssuer() *Issuer {
	return New("test-secret-at-least-16-chars", "aerial-test", "aerial-clients", 15*time.Minute)
}

func TestSignAndVerifyRoundTrip(t *testing.T) {
	iss := newIssuer()
	tok, exp, err := iss.Sign(Claims{UserID: "u1", OrgID: "o1", Email: "a@b.c", Role: "user", DeviceID: "d1"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !exp.After(time.Now()) {
		t.Fatalf("expiry %v not in the future", exp)
	}
	claims, err := iss.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.UserID != "u1" || claims.OrgID != "o1" || claims.Email != "a@b.c" || claims.Role != "user" || claims.DeviceID != "d1" {
		t.Fatalf("claims round-trip mismatch: %+v", claims)
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	iss := newIssuer()
	tok, _, _ := iss.Sign(Claims{UserID: "u1"})
	// Flip a character in the payload segment.
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT segments, got %d", len(parts))
	}
	parts[1] = mutate(parts[1])
	if _, err := iss.Verify(strings.Join(parts, ".")); err == nil {
		t.Fatal("expected verify to reject a tampered token")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	a := newIssuer()
	b := New("a-totally-different-secret-16ch", "aerial-test", "aerial-clients", 15*time.Minute)
	tok, _, _ := a.Sign(Claims{UserID: "u1"})
	if _, err := b.Verify(tok); err == nil {
		t.Fatal("expected verify with wrong secret to fail")
	}
}

func TestVerifyRejectsWrongIssuerOrAudience(t *testing.T) {
	signer := newIssuer()
	tok, _, _ := signer.Sign(Claims{UserID: "u1"})

	wrongIss := New("test-secret-at-least-16-chars", "someone-else", "aerial-clients", 15*time.Minute)
	if _, err := wrongIss.Verify(tok); err == nil {
		t.Fatal("expected wrong-issuer verify to fail")
	}
	wrongAud := New("test-secret-at-least-16-chars", "aerial-test", "other-aud", 15*time.Minute)
	if _, err := wrongAud.Verify(tok); err == nil {
		t.Fatal("expected wrong-audience verify to fail")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	// negative TTL → already expired; leeway is 30s so use a large negative.
	iss := New("test-secret-at-least-16-chars", "aerial-test", "aerial-clients", -1*time.Hour)
	tok, _, _ := iss.Sign(Claims{UserID: "u1"})
	if _, err := iss.Verify(tok); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestMiddlewareAllowsSkipPaths(t *testing.T) {
	iss := newIssuer()
	h := iss.Middleware("/v1/health")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("skip path should bypass auth; got %d", rec.Code)
	}
}

func TestMiddlewareRejectsMissingAndBadToken(t *testing.T) {
	iss := newIssuer()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			t.Fatal("claims should be in context for authenticated request")
		}
		w.WriteHeader(http.StatusOK)
	})
	h := iss.Middleware("/v1/health")(next)

	// missing header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token want 401 got %d", rec.Code)
	}

	// garbage token
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token want 401 got %d", rec.Code)
	}
}

func TestMiddlewarePassesValidTokenAndPopulatesContext(t *testing.T) {
	iss := newIssuer()
	tok, _, _ := iss.Sign(Claims{UserID: "u-ctx", OrgID: "o-ctx"})
	var seen *Claims
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := FromContext(r.Context())
		seen = c
		w.WriteHeader(http.StatusOK)
	})
	h := iss.Middleware()(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token want 200 got %d", rec.Code)
	}
	if seen == nil || seen.UserID != "u-ctx" || seen.OrgID != "o-ctx" {
		t.Fatalf("context claims not populated: %+v", seen)
	}
}

// mutate flips the first character of s to a different base64url char.
func mutate(s string) string {
	if len(s) == 0 {
		return "A"
	}
	first := s[0]
	repl := byte('A')
	if first == 'A' {
		repl = 'B'
	}
	return string(repl) + s[1:]
}
