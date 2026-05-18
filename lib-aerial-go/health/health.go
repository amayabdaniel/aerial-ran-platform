// Package health exposes a background-refreshed liveness/readiness probe.
// Avoids hitting Postgres on every healthcheck.
package health

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/amayabdaniel/aerial-ran-platform/lib-aerial-go/respond"
)

// Checker periodically runs Probe and stores the result.
type Checker struct {
	healthy atomic.Bool
}

// Probe is any function returning nil when the dependency is OK.
type Probe func(ctx context.Context) error

// Start launches a goroutine that refreshes health every interval until ctx is canceled.
// Multiple probes can be combined by the caller (all must pass).
func (c *Checker) Start(ctx context.Context, interval time.Duration, probe Probe) {
	c.healthy.Store(false)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		c.check(ctx, probe)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.check(ctx, probe)
			}
		}
	}()
}

func (c *Checker) check(parent context.Context, probe Probe) {
	if probe == nil {
		c.healthy.Store(true)
		return
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	c.healthy.Store(probe(ctx) == nil)
}

// Handler returns 200 if healthy, 503 otherwise.
func (c *Checker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if c.healthy.Load() {
			respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		respond.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
	}
}
