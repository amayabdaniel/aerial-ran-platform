package bodylimit

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The single shared implementation: a read past max fails, a read under it
// succeeds. Services rely on this, so it is pinned here as well as in each
// service's own stack test.
func TestMiddlewareRejectsOversizedBody(t *testing.T) {
	const max = 1 << 10 // 1 KiB, small for a fast test
	h := Middleware(max)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	underRec := httptest.NewRecorder()
	h.ServeHTTP(underRec, httptest.NewRequest(http.MethodPost, "/",
		bytes.NewReader(bytes.Repeat([]byte("a"), max/2))))
	if underRec.Code != http.StatusOK {
		t.Fatalf("under-limit body: want 200, got %d", underRec.Code)
	}

	overRec := httptest.NewRecorder()
	h.ServeHTTP(overRec, httptest.NewRequest(http.MethodPost, "/",
		bytes.NewReader(bytes.Repeat([]byte("a"), max*2))))
	if overRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit body: want 413, got %d", overRec.Code)
	}
}

// A nil body must not panic (GET requests, etc.).
func TestMiddlewareNilBodyPasses(t *testing.T) {
	h := Middleware(DefaultMaxBytes)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("nil body: want 204, got %d", rec.Code)
	}
}

func TestDefaultMaxBytesIs4MiB(t *testing.T) {
	if DefaultMaxBytes != 4*1024*1024 {
		t.Fatalf("DefaultMaxBytes = %d, want 4 MiB (%d)", DefaultMaxBytes, 4*1024*1024)
	}
}
