package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/bodylimit"
	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/runner"
)

// A request body over the limit must be rejected and one under it accepted.
// This drives the real assembled stack via runner.Handler, so dropping the
// bodylimit middleware from that chain makes this fail — the wiring-vs-behavior
// distinction. iam/esim/subscriber once hand-rolled their own chains and shipped
// with no request-body limit at all; they now share runner.Handler, and this
// per-service test is the regression net that the shared limit stays wired.
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
	stack := runner.Handler(discard, "svc-aerial-esim", "", passthroughAuth, mux)

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
