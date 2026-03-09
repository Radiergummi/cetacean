package api

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type PrometheusProxy struct {
	baseURL string
	client  *http.Client
}

func NewPrometheusProxy(baseURL string) *PrometheusProxy {
	return &PrometheusProxy{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// maxPrometheusResponseBytes limits proxy response size to 10MB.
const maxPrometheusResponseBytes = 10 << 20

var allowedPrometheusPaths = map[string]bool{
	"/query":       true,
	"/query_range": true,
}

// PrometheusNotConfiguredHandler returns a handler that responds with 503
// when Prometheus is not configured.
func PrometheusNotConfiguredHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusServiceUnavailable, "prometheus not configured")
	})
}

func (p *PrometheusProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Map /api/metrics/query → /api/v1/query
	// Map /api/metrics/query_range → /api/v1/query_range
	path := strings.TrimPrefix(r.URL.Path, "/api/metrics")
	if !allowedPrometheusPaths[path] {
		writeError(w, http.StatusForbidden, "forbidden prometheus endpoint")
		return
	}

	targetURL := p.baseURL + "/api/v1" + path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create prometheus request: %s", err))
		return
	}

	resp, err := p.client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("prometheus unreachable at %s: %s", p.baseURL, err))
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, maxPrometheusResponseBytes)); err != nil {
		slog.Warn("prometheus proxy copy error", "error", err)
	}
}
