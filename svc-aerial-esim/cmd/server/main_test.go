package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/bodylimit"
)

// A request body over the limit must be rejected and one under it accepted.
// This drives the service's real newHandler stack rather than a rebuilt copy,
// so deleting the bodylimit middleware from newHandler makes this fail — the
// wiring-vs-behavior distinction. iam/esim/subscriber hand-roll their chain and
// once shipped with no request-body limit at all; this guards against a repeat.
func TestBodyLimitEnforced(t *testing.T) {
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	passthroughAuth := func(h http.Handler) http.Handler { return h }

	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	stack := newHandler(discard, "", passthroughAuth, mux)

	// Under the limit: accepted.
	underRec := httptest.NewRecorder()
	under := httptest.NewRequest(http.MethodPost, "/echo",
		bytes.NewReader(bytes.Repeat([]byte("a"), 1<<10)))
	stack.ServeHTTP(underRec, under)
	if underRec.Code != http.StatusOK {
		t.Fatalf("under-limit request: want 200, got %d", underRec.Code)
	}

	// Over the limit: rejected. Without the cap io.ReadAll would succeed and
	// this would be 200.
	overRec := httptest.NewRecorder()
	over := httptest.NewRequest(http.MethodPost, "/echo",
		bytes.NewReader(bytes.Repeat([]byte("a"), int(bodylimit.DefaultMaxBytes)+1<<10)))
	stack.ServeHTTP(overRec, over)
	if overRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-limit request: want 413, got %d — body limit not enforced", overRec.Code)
	}
}
