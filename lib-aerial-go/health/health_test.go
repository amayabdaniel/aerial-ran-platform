package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func TestCheckerHealthyWhenProbePasses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Checker{}
	c.Start(ctx, 50*time.Millisecond, func(context.Context) error { return nil })

	waitFor(t, func() bool {
		rec := httptest.NewRecorder()
		c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
		return rec.Code == http.StatusOK
	})
}

func TestCheckerUnhealthyWhenProbeFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Checker{}
	c.Start(ctx, 50*time.Millisecond, func(context.Context) error { return errors.New("down") })

	// Initial state is false and stays false; verify the handler reports 503.
	rec := httptest.NewRecorder()
	c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d", rec.Code)
	}
}

func TestCheckerNilProbeIsHealthy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &Checker{}
	c.Start(ctx, 50*time.Millisecond, nil)
	waitFor(t, func() bool {
		rec := httptest.NewRecorder()
		c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
		return rec.Code == http.StatusOK
	})
}
