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
