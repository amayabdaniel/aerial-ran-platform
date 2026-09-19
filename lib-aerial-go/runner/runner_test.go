package runner

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The body-limit middleware must cap request bodies: a body within the limit is
// read normally, but one over the limit makes the handler's read fail (so it
// cannot force the service to buffer an unbounded payload — a memory DoS).
func TestBodyLimitMiddleware(t *testing.T) {
	const limit = 16
	h := bodyLimitMW(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Under the limit: read succeeds.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small")))
	if rec.Code != http.StatusOK {
		t.Fatalf("under limit want 200 got %d", rec.Code)
	}

	// Over the limit: the wrapped body read errors → 413.
	rec2 := httptest.NewRecorder()
	big := strings.Repeat("A", limit+1024)
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(big)))
	if rec2.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over limit must fail the read (413), got %d — body was not capped", rec2.Code)
	}
}
