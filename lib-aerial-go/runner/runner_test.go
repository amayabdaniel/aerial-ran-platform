package runner

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/bodylimit"
	jwtlib "github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
)

// signedTTL issues a token through the given issuer and returns the lifetime
// baked into the token itself (exp − iat), read back after a real verify rather
// than trusting the issuer's return value.
func signedTTL(t *testing.T, iss *jwtlib.Issuer) time.Duration {
	t.Helper()
	tok, _, err := iss.Sign(jwtlib.Claims{UserID: "u"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	c, err := iss.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.ExpiresAt == nil || c.IssuedAt == nil {
		t.Fatalf("token missing exp/iat claims")
	}
	return c.ExpiresAt.Sub(c.IssuedAt.Time)
}

// TestAccessTokenTTLEquivalence is the gate on this consolidation. Before the
// refactor, iam built its issuer directly from cfg.AccessTokenTTL; after it,
// runner owns issuer construction via buildIssuer(Opts). This proves a token
// issued through the consolidated path carries the SAME lifetime as the
// pre-consolidation path did for the same config — the actual exp−iat claim,
// not merely that a token exists. The TTL is deliberately a non-default value:
// at 15m a hardcoded-15m regression would be invisible.
func TestAccessTokenTTLEquivalence(t *testing.T) {
	const cfgTTL = 37 * time.Minute
	const secret, iss, aud = "0123456789abcdef", "aerial-ran-platform", "aerial-clients"

	// Pre-consolidation path: iam's own jwtlib.New(secret, iss, aud, cfg.AccessTokenTTL).
	before := jwtlib.New(secret, iss, aud, cfgTTL)
	// Post-consolidation path: runner's buildIssuer from Opts.
	after := buildIssuer(Opts{JWTSecret: secret, JWTIssuer: iss, JWTAudience: aud, AccessTokenTTL: cfgTTL})

	beforeTTL := signedTTL(t, before)
	afterTTL := signedTTL(t, after)

	if afterTTL != cfgTTL {
		t.Fatalf("consolidated token TTL = %v, want configured %v — TTL not threaded through Opts", afterTTL, cfgTTL)
	}
	if afterTTL != beforeTTL {
		t.Fatalf("consolidated TTL %v != pre-consolidation TTL %v for identical config", afterTTL, beforeTTL)
	}
}

// TestAccessTTLDefaultsTo15m pins that a service which does not set
// AccessTokenTTL (billing, provision, ran-control, esim, subscriber) keeps the
// historical 15m, so the consolidation does not change their behavior.
func TestAccessTTLDefaultsTo15m(t *testing.T) {
	if got := accessTTL(Opts{}); got != 15*time.Minute {
		t.Fatalf("default access TTL = %v, want 15m", got)
	}
	if got := accessTTL(Opts{AccessTokenTTL: 20 * time.Minute}); got != 20*time.Minute {
		t.Fatalf("explicit access TTL = %v, want 20m", got)
	}
}

// TestAuthSkipPaths diffs the effective auth-skip set rather than eyeballing the
// literals. A verify-only service exposes exactly the three always-public
// endpoints; iam's set is those three plus its four public /v1/auth/* endpoints,
// seven in total and in order — the exact set iam hand-rolled before.
func TestAuthSkipPaths(t *testing.T) {
	base := authSkipPaths(Opts{})
	wantBase := []string{"/metrics", "/v1/health", "/v1/ready"}
	if !slices.Equal(base, wantBase) {
		t.Fatalf("base skip set = %v, want %v", base, wantBase)
	}

	iamExtra := []string{"/v1/auth/signup", "/v1/auth/login", "/v1/auth/refresh", "/v1/auth/logout"}
	iam := authSkipPaths(Opts{AuthSkipPaths: iamExtra})
	wantIAM := []string{
		"/metrics", "/v1/health", "/v1/ready",
		"/v1/auth/signup", "/v1/auth/login", "/v1/auth/refresh", "/v1/auth/logout",
	}
	if !slices.Equal(iam, wantIAM) {
		t.Fatalf("iam skip set = %v, want %v", iam, wantIAM)
	}
	if len(iam) != 7 {
		t.Fatalf("iam skip set has %d paths, want exactly 7", len(iam))
	}
}

// TestHandlerEnforcesBodyLimit drives the real assembled chain and proves the
// 4 MiB request-body cap is in it. This is the runner-level guard for the body
// limit; the three cmd/server tests are the same assertion per service. Dropping
// bodylimit.Middleware from Handler makes this fail.
func TestHandlerEnforcesBodyLimit(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	passthroughAuth := func(h http.Handler) http.Handler { return h }

	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	stack := Handler(discard, "svc-test", "", passthroughAuth, mux)

	underRec := httptest.NewRecorder()
	stack.ServeHTTP(underRec, httptest.NewRequest(http.MethodPost, "/echo",
		bytes.NewReader(bytes.Repeat([]byte("a"), 1<<10))))
	if underRec.Code != http.StatusOK {
		t.Fatalf("under-limit request: want 200, got %d", underRec.Code)
	}

	overRec := httptest.NewRecorder()
	stack.ServeHTTP(overRec, httptest.NewRequest(http.MethodPost, "/echo",
		bytes.NewReader(bytes.Repeat([]byte("a"), int(bodylimit.DefaultMaxBytes)+1<<10))))
	if overRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit request: want 413, got %d — body limit not enforced", overRec.Code)
	}
}
