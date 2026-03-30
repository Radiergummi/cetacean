package metrics

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry is a custom Prometheus registry for Cetacean's own metrics.
var Registry = prometheus.NewRegistry()

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cetacean_http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "handler", "status"},
	)

	httpRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cetacean_http_request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "handler"},
	)

	httpRequestSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cetacean_http_request_size_bytes",
			Help:    "Size of HTTP requests in bytes.",
			Buckets: prometheus.ExponentialBuckets(64, 4, 8),
		},
		[]string{"method", "handler"},
	)

	httpResponseSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cetacean_http_response_size_bytes",
			Help:    "Size of HTTP responses in bytes.",
			Buckets: prometheus.ExponentialBuckets(64, 4, 8),
		},
		[]string{"method", "handler"},
	)
)

func init() {
	Registry.MustRegister(httpRequestsTotal)
	Registry.MustRegister(httpRequestDurationSeconds)
	Registry.MustRegister(httpRequestSizeBytes)
	Registry.MustRegister(httpResponseSizeBytes)
}

// RecordHTTPRequest records metrics for a completed HTTP request.
func RecordHTTPRequest(handler, method string, status int, durationSeconds float64, requestBytes, responseBytes int64) {
	httpRequestsTotal.WithLabelValues(method, handler, strconv.Itoa(status)).Inc()
	httpRequestDurationSeconds.WithLabelValues(method, handler).Observe(durationSeconds)
	httpRequestSizeBytes.WithLabelValues(method, handler).Observe(float64(requestBytes))
	httpResponseSizeBytes.WithLabelValues(method, handler).Observe(float64(responseBytes))
}

// Handler returns an http.Handler that serves metrics from the custom registry.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}
