package api

import (
	"io"
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

func (p *PrometheusProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Map /api/metrics/query → /api/v1/query
	// Map /api/metrics/query_range → /api/v1/query_range
	path := strings.TrimPrefix(r.URL.Path, "/api/metrics")
	if !allowedPrometheusPaths[path] {
		http.Error(w, "forbidden prometheus endpoint", http.StatusForbidden)
		return
	}

	targetURL := p.baseURL + "/api/v1" + path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "prometheus request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, io.LimitReader(resp.Body, maxPrometheusResponseBytes))
}
