package prometheus

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// ErrorWriter is a callback for writing HTTP error responses.
// This decouples the Prometheus package from the API error registry.
type ErrorWriter func(w http.ResponseWriter, r *http.Request, code, detail string)

// nilProxyErrorWriter is used by nil-receiver handlers to produce structured
// error responses matching the rest of the API. Must be set during
// initialization (before serving requests) via SetNilProxyErrorWriter.
var nilProxyErrorWriter atomic.Pointer[ErrorWriter]

// SetNilProxyErrorWriter registers the error writer used when a nil *Proxy
// receives a request (i.e., Prometheus is not configured). Must be called
// during initialization, before any requests are served.
func SetNilProxyErrorWriter(w ErrorWriter) {
	nilProxyErrorWriter.Store(&w)
}

type Proxy struct {
	baseURL    string
	client     *http.Client
	writeError ErrorWriter
}

func NewProxy(baseURL string, writeError ErrorWriter) *Proxy {
	SetNilProxyErrorWriter(writeError)
	return &Proxy{
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     &http.Client{Timeout: 30 * time.Second},
		writeError: writeError,
	}
}

// maxResponseBytes limits proxy response size to 10MB.
const maxResponseBytes = 10 << 20

// proxyTo sends a proxied request to the given Prometheus API path with the given params.
// Does not read r.URL.Path — the caller determines the target path.
func (p *Proxy) proxyTo(
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
		p.writeError(w, r, "MTR007", "failed to create prometheus request")
		return
	}

	resp, err := p.client.Do(outReq)
	if err != nil {
		slog.Error("prometheus unreachable", "url", p.baseURL, "error", err)
		p.writeError(w, r, "MTR002", "prometheus unreachable")
		return
	}
	defer resp.Body.Close()

	// Forward successful responses directly.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, io.LimitReader(resp.Body, maxResponseBytes)); err != nil {
			slog.Warn("prometheus proxy copy error", "error", err)
		}
		return
	}

	// Non-2xx: return a structured error instead of forwarding the raw
	// Prometheus response (which may be HTML or an opaque error page).
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	slog.Error("prometheus returned error",
		"url", targetURL,
		"status", resp.StatusCode,
		"body", string(preview),
	)
	p.writeError(w, r, "MTR002", fmt.Sprintf(
		"prometheus returned HTTP %d for %s",
		resp.StatusCode, targetURL,
	))
}

// HandleMetricsLabels proxies to /api/v1/labels with optional match[] param.
func (p *Proxy) HandleMetricsLabels(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeNilProxyError(w, r)
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
func (p *Proxy) HandleMetricsLabelValues(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeNilProxyError(w, r)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		p.writeError(w, r, "MTR006", "missing label name")
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
func (p *Proxy) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if p == nil {
		writeNilProxyError(w, r)
		return
	}

	q := r.URL.Query()
	query := q.Get("query")
	if query == "" {
		p.writeError(w, r, "MTR003", "missing required parameter: query")
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

func writeNilProxyError(w http.ResponseWriter, r *http.Request) {
	if ew := nilProxyErrorWriter.Load(); ew != nil {
		(*ew)(w, r, "MTR001", "prometheus not configured")
		return
	}

	http.Error(w, "prometheus not configured", http.StatusServiceUnavailable)
}
