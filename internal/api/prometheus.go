package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
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

// proxyTo sends a proxied request to the given Prometheus API path with the given params.
// Does not read r.URL.Path — the caller determines the target path.
func (p *PrometheusProxy) proxyTo(
	w http.ResponseWriter,
	r *http.Request,
	promPath string,
	params url.Values,
) {
	targetURL := p.baseURL + promPath
	if encoded := params.Encode(); encoded != "" {
		targetURL += "?" + encoded
	}

	outReq, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		slog.Error("failed to create prometheus request", "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to create prometheus request")
		return
	}

	resp, err := p.client.Do(outReq)
	if err != nil {
		slog.Error("prometheus unreachable", "url", p.baseURL, "error", err)
		writeProblem(w, r, http.StatusBadGateway, "prometheus unreachable")
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, io.LimitReader(resp.Body, maxPrometheusResponseBytes)); err != nil {
		slog.Warn("prometheus proxy copy error", "error", err)
	}
}

// HandleMetricsLabels proxies to /api/v1/labels with optional match[] param.
func (p *PrometheusProxy) HandleMetricsLabels(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}
	allowed := url.Values{}
	for _, m := range r.URL.Query()["match[]"] {
		allowed.Add("match[]", m)
	}
	for _, key := range []string{"start", "end"} {
		if v := r.URL.Query().Get(key); v != "" {
			allowed.Set(key, v)
		}
	}
	p.proxyTo(w, r, "/api/v1/labels", allowed)
}

// HandleMetricsLabelValues proxies to /api/v1/label/{name}/values.
func (p *PrometheusProxy) HandleMetricsLabelValues(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}
	name := r.PathValue("name")
	if name == "" {
		writeProblem(w, r, http.StatusBadRequest, "missing label name")
		return
	}
	allowed := url.Values{}
	for _, m := range r.URL.Query()["match[]"] {
		allowed.Add("match[]", m)
	}
	p.proxyTo(w, r, "/api/v1/label/"+url.PathEscape(name)+"/values", allowed)
}

// HandleMetrics is a content-negotiated handler that proxies Prometheus queries.
// It routes instant vs range queries by the presence of start+end params.
func (p *PrometheusProxy) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "prometheus not configured")
		return
	}

	q := r.URL.Query()
	query := q.Get("query")
	if query == "" {
		writeProblem(w, r, http.StatusBadRequest, "missing required parameter: query")
		return
	}

	promPath := "/api/v1/query"
	if q.Get("start") != "" && q.Get("end") != "" {
		promPath = "/api/v1/query_range"
	}

	allowed := url.Values{}
	for _, key := range []string{"query", "time", "timeout", "start", "end", "step"} {
		if v := q.Get(key); v != "" {
			allowed.Set(key, v)
		}
	}

	p.proxyTo(w, r, promPath, allowed)
}
