package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// DBPoolStats is the slice of pgxpool.Stat the pool gauges need. *pgxpool.Stat
// satisfies it, so callers pass `func() metrics.DBPoolStats { return pool.Stat() }`
// — which keeps this package free of a pgx dependency and makes the collector
// unit-testable with a fake.
type DBPoolStats interface {
	AcquiredConns() int32
	IdleConns() int32
	TotalConns() int32
	MaxConns() int32
	AcquireDuration() time.Duration
}

var (
	descPoolAcquired = prometheus.NewDesc("db_pool_acquired_conns",
		"Pool connections currently acquired (in use).", []string{"service"}, nil)
	descPoolIdle = prometheus.NewDesc("db_pool_idle_conns",
		"Idle pool connections.", []string{"service"}, nil)
	descPoolTotal = prometheus.NewDesc("db_pool_total_conns",
		"Total pool connections (acquired + idle + constructing).", []string{"service"}, nil)
	descPoolMax = prometheus.NewDesc("db_pool_max_conns",
		"Maximum pool size.", []string{"service"}, nil)
	descPoolAcquireWait = prometheus.NewDesc("db_pool_acquire_wait_seconds_total",
		"Cumulative time callers spent blocked waiting to acquire a connection.", []string{"service"}, nil)
)

// dbPoolCollector reads the pool's Stat() at scrape time — pool saturation is
// invisible in RED (it surfaces only as latency with no cause), so these are the
// signal that points at the cause. Reading on scrape costs nothing between
// scrapes, unlike a per-request Stat() call on the hot path.
type dbPoolCollector struct {
	service string
	stat    func() DBPoolStats
}

func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descPoolAcquired
	ch <- descPoolIdle
	ch <- descPoolTotal
	ch <- descPoolMax
	ch <- descPoolAcquireWait
}

func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stat()
	ch <- prometheus.MustNewConstMetric(descPoolAcquired, prometheus.GaugeValue, float64(s.AcquiredConns()), c.service)
	ch <- prometheus.MustNewConstMetric(descPoolIdle, prometheus.GaugeValue, float64(s.IdleConns()), c.service)
	ch <- prometheus.MustNewConstMetric(descPoolTotal, prometheus.GaugeValue, float64(s.TotalConns()), c.service)
	ch <- prometheus.MustNewConstMetric(descPoolMax, prometheus.GaugeValue, float64(s.MaxConns()), c.service)
	// AcquireDuration is cumulative, so it is a counter.
	ch <- prometheus.MustNewConstMetric(descPoolAcquireWait, prometheus.CounterValue, s.AcquireDuration().Seconds(), c.service)
}

// RegisterDBPool registers a scrape-time collector exposing the service's pgx
// pool gauges under a `service` label matching the RED convention. Call once per
// service after the pool is created.
func RegisterDBPool(service string, stat func() DBPoolStats) {
	prometheus.MustRegister(&dbPoolCollector{service: service, stat: stat})
}
