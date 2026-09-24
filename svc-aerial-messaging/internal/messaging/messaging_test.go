package messaging

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/jwt"
)

// A malformed message request (missing to_user_id/body) is the CALLER's fault
// and must return 400, not 500: reporting a client error as a server error tells
// an operator to investigate a fault that does not exist and pages someone
// eventually. Send's validation returns before touching the DB/JetStream, so a
// nil-backed Service is safe here — the request never reaches those.
func TestSend_MissingFields_Returns400(t *testing.T) {
	iss := jwt.New("test-secret-at-least-32-bytes-long!!", "aerial-test", "aerial-clients", time.Hour)
	tok, _, err := iss.Sign(jwt.Claims{
		UserID: "11111111-1111-1111-1111-111111111111",
		OrgID:  "22222222-2222-2222-2222-222222222222",
	})
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	h := NewHandler(&Service{}) // nil pool/js: validation short-circuits before use
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := iss.Middleware()(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed message body must return 400 (client error), got %d — a 500 blames the server for the caller's mistake", rec.Code)
	}
}

func TestOriginHostsFromCSV(t *testing.T) {
	got := OriginHostsFromCSV("http://localhost:3000, https://app.example.com ,plainhost:8080,")
	want := []string{"localhost:3000", "app.example.com", "plainhost:8080"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

// CSWSH: a cross-site Origin must be rejected on the WebSocket upgrade handshake
// with 403, independent of the JWT. Otherwise a page on another origin could open
// a victim's authenticated message stream in their browser. The request carries a
// valid token and a well-formed WS handshake so Accept reaches the Origin check;
// only the Origin is hostile.
func TestStreamRejectsCrossSiteOrigin(t *testing.T) {
	iss := jwt.New("test-secret-at-least-32-bytes-long!!", "aerial-test", "aerial-clients", time.Hour)
	tok, _, err := iss.Sign(jwt.Claims{
		UserID: "11111111-1111-1111-1111-111111111111",
		OrgID:  "22222222-2222-2222-2222-222222222222",
	})
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	// Service is nil-backed: origin rejection happens before any svc use.
	h := NewHandler(&Service{}, "good.example") // allow-list excludes the attacker
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := iss.Middleware()(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/messages/stream", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	// Make it a well-formed WS handshake so Accept reaches the Origin check.
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Origin", "http://evil.example") // cross-site, not in the allow-list
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site Origin must be rejected with 403 on the WS upgrade, got %d — CSWSH not prevented", rec.Code)
	}
}
