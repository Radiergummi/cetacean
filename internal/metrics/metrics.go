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

	// SSE metrics.
	sseConnectionsActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cetacean_sse_connections_active",
			Help: "Number of active SSE connections.",
		},
	)

	sseEventsBroadcastTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cetacean_sse_events_broadcast_total",
			Help: "Total number of SSE events broadcast.",
		},
	)

	sseEventsDroppedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cetacean_sse_events_dropped_total",
			Help: "Total number of SSE events dropped.",
		},
	)

	// Cache metrics.
	cacheResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cetacean_cache_resources",
			Help: "Number of cached resources by type.",
		},
		[]string{"type"},
	)

	cacheSyncDurationSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "cetacean_cache_sync_duration_seconds",
			Help:    "Duration of cache sync operations in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)

	cacheMutationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cetacean_cache_mutations_total",
			Help: "Total number of cache mutations.",
		},
		[]string{"type", "action"},
	)

	// Prometheus proxy metrics.
	prometheusRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cetacean_prometheus_requests_total",
			Help: "Total number of Prometheus proxy requests.",
		},
		[]string{"status"},
	)

	prometheusRequestDurationSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "cetacean_prometheus_request_duration_seconds",
			Help:    "Duration of Prometheus proxy requests in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)

	// Recommendation metrics.
	recommendationsCheckDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cetacean_recommendations_check_duration_seconds",
			Help:    "Duration of recommendation checker runs in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"checker"},
	)

	recommendationsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cetacean_recommendations_total",
			Help: "Number of active recommendations by severity.",
		},
		[]string{"severity"},
	)
)

func init() {
	Registry.MustRegister(httpRequestsTotal)
	Registry.MustRegister(httpRequestDurationSeconds)
	Registry.MustRegister(httpRequestSizeBytes)
	Registry.MustRegister(httpResponseSizeBytes)
	Registry.MustRegister(sseConnectionsActive)
	Registry.MustRegister(sseEventsBroadcastTotal)
	Registry.MustRegister(sseEventsDroppedTotal)
	Registry.MustRegister(cacheResources)
	Registry.MustRegister(cacheSyncDurationSeconds)
	Registry.MustRegister(cacheMutationsTotal)
	Registry.MustRegister(prometheusRequestsTotal)
	Registry.MustRegister(prometheusRequestDurationSeconds)
	Registry.MustRegister(recommendationsCheckDurationSeconds)
	Registry.MustRegister(recommendationsTotal)
}

// RecordHTTPRequest records metrics for a completed HTTP request.
func RecordHTTPRequest(
	handler, method string,
	status int,
	durationSeconds float64,
	requestBytes, responseBytes int64,
) {
	httpRequestsTotal.WithLabelValues(method, handler, strconv.Itoa(status)).Inc()
	httpRequestDurationSeconds.WithLabelValues(method, handler).Observe(durationSeconds)
	httpRequestSizeBytes.WithLabelValues(method, handler).Observe(float64(requestBytes))
	httpResponseSizeBytes.WithLabelValues(method, handler).Observe(float64(responseBytes))
}

// RecordSSEConnect increments the active SSE connection gauge.
func RecordSSEConnect() {
	sseConnectionsActive.Inc()
}

// RecordSSEDisconnect decrements the active SSE connection gauge.
func RecordSSEDisconnect() {
	sseConnectionsActive.Dec()
}

// RecordSSEBroadcast increments the SSE events broadcast counter.
func RecordSSEBroadcast() {
	sseEventsBroadcastTotal.Inc()
}

// RecordSSEDrop increments the SSE events dropped counter.
func RecordSSEDrop() {
	sseEventsDroppedTotal.Inc()
}

// SetCacheResources sets the number of cached resources for a given type.
func SetCacheResources(resourceType string, count int) {
	cacheResources.WithLabelValues(resourceType).Set(float64(count))
}

// ObserveSyncDuration records the duration of a cache sync operation.
func ObserveSyncDuration(seconds float64) {
	cacheSyncDurationSeconds.Observe(seconds)
}

// RecordCacheMutation increments the cache mutation counter for a given type and action.
func RecordCacheMutation(resourceType, action string) {
	cacheMutationsTotal.WithLabelValues(resourceType, action).Inc()
}

// RecordPrometheusRequest records metrics for a Prometheus proxy request.
func RecordPrometheusRequest(status int, durationSeconds float64) {
	prometheusRequestsTotal.WithLabelValues(strconv.Itoa(status)).Inc()
	prometheusRequestDurationSeconds.Observe(durationSeconds)
}

// ObserveRecommendationCheck records the duration of a recommendation checker run.
func ObserveRecommendationCheck(checker string, durationSeconds float64) {
	recommendationsCheckDurationSeconds.WithLabelValues(checker).Observe(durationSeconds)
}

// SetRecommendationCounts sets the number of active recommendations by severity.
func SetRecommendationCounts(critical, warning, info int) {
	recommendationsTotal.WithLabelValues("critical").Set(float64(critical))
	recommendationsTotal.WithLabelValues("warning").Set(float64(warning))
	recommendationsTotal.WithLabelValues("info").Set(float64(info))
}

// Handler returns an http.Handler that serves metrics from the custom registry.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}
