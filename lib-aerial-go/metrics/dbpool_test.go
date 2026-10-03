package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

type fakeStats struct {
	acquired, idle, total, max int32
	wait                       time.Duration
}

func (f fakeStats) AcquiredConns() int32           { return f.acquired }
func (f fakeStats) IdleConns() int32               { return f.idle }
func (f fakeStats) TotalConns() int32              { return f.total }
func (f fakeStats) MaxConns() int32                { return f.max }
func (f fakeStats) AcquireDuration() time.Duration { return f.wait }

// The collector must emit the pool's actual Stat values under the service label —
// gauges for the live counts, a counter for cumulative acquire-wait. Fabricating
// a *pgxpool.Stat is impossible (unexported fields), which is exactly why the
// collector reads the DBPoolStats interface instead.
func TestDBPoolCollector(t *testing.T) {
	c := &dbPoolCollector{service: "svc-x", stat: func() DBPoolStats {
		return fakeStats{acquired: 7, idle: 3, total: 10, max: 20, wait: 2500 * time.Millisecond}
	}}

	expected := `
# HELP db_pool_acquire_wait_seconds_total Cumulative time callers spent blocked waiting to acquire a connection.
# TYPE db_pool_acquire_wait_seconds_total counter
db_pool_acquire_wait_seconds_total{service="svc-x"} 2.5
# HELP db_pool_acquired_conns Pool connections currently acquired (in use).
# TYPE db_pool_acquired_conns gauge
db_pool_acquired_conns{service="svc-x"} 7
# HELP db_pool_idle_conns Idle pool connections.
# TYPE db_pool_idle_conns gauge
db_pool_idle_conns{service="svc-x"} 3
# HELP db_pool_max_conns Maximum pool size.
# TYPE db_pool_max_conns gauge
db_pool_max_conns{service="svc-x"} 20
# HELP db_pool_total_conns Total pool connections (acquired + idle + constructing).
# TYPE db_pool_total_conns gauge
db_pool_total_conns{service="svc-x"} 10
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected)); err != nil {
		t.Fatalf("db pool metrics mismatch: %v", err)
	}
}
