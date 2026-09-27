package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
	// Probe starts passing, then fails. Asserting 503 alone would be satisfied by
	// the initial false state (Start stores false before the first probe runs), so
	// it wouldn't catch a check() that marked healthy on a FAILING probe. Reaching
	// 200 first proves a passing probe set healthy; the flip to 503 then proves the
	// checker reacts to the probe FAILING, which is the behaviour this names.
	var fail atomic.Bool
	c.Start(ctx, 20*time.Millisecond, func(context.Context) error {
		if fail.Load() {
			return errors.New("down")
		}
		return nil
	})

	waitFor(t, func() bool {
		rec := httptest.NewRecorder()
		c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
		return rec.Code == http.StatusOK
	})

	fail.Store(true)
	waitFor(t, func() bool {
		rec := httptest.NewRecorder()
		c.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
		return rec.Code == http.StatusServiceUnavailable
	})
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
