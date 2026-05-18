// Package metrics provides RED-style Prometheus middleware for HTTP servers.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	reqTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "HTTP requests by method, path, status.",
	}, []string{"service", "method", "path", "status"})

	reqDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 14), // 1ms..16s
	}, []string{"service", "method", "path"})

	inFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "http_in_flight_requests",
		Help: "Currently in-flight HTTP requests.",
	}, []string{"service"})
)

func init() {
	prometheus.MustRegister(reqTotal, reqDuration, inFlight)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(c int) { s.status = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// Middleware returns Prometheus instrumentation for the given service name.
func Middleware(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inFlight.WithLabelValues(service).Inc()
			defer inFlight.WithLabelValues(service).Dec()

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			path := r.URL.Path
			reqDuration.WithLabelValues(service, r.Method, path).Observe(time.Since(start).Seconds())
			reqTotal.WithLabelValues(service, r.Method, path, strconv.Itoa(rec.status)).Inc()
		})
	}
}

// Handler exposes /metrics.
func Handler() http.Handler { return promhttp.Handler() }
