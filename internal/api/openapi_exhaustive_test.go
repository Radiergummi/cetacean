package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
)

// TestEveryReadEndpointMatchesSpec walks every GET operation in the OpenAPI
// spec, issues a request with substituted path parameters, and validates the
// response body against the spec's response schema. New endpoints in the spec
// are picked up automatically. Operations whose path parameters can't be
// resolved from fixtures are skipped with a log line so gaps are visible.
//
// This complements TestResponsesMatchOpenAPISpec (which asserts specific
// expected statuses on a hand-picked list) by detecting schema drift on every
// read endpoint without requiring manual test registration.
func TestEveryReadEndpointMatchesSpec(t *testing.T) {
	specBytes, doc, specRouter := loadTestSpec(t)

	c := cache.New(nil)
	populateSpecFixtures(c)

	h := newTestHandlers(t, withCache(c))
	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	defer b.Close()

	router := newTestRouter(t, h, b, specBytes)

	var (
		validated  int
		unresolved int
		nonSuccess int
	)

	for pathTemplate, pathItem := range doc.Paths.Map() {
		if pathItem.Get == nil {
			continue
		}

		if skipEndpoint(pathTemplate) {
			continue
		}

		requestPath, ok := resolvePath(pathTemplate)
		if !ok {
			t.Logf("skipping %s: no fixture for path parameters", pathTemplate)
			unresolved++
			continue
		}

		t.Run(pathTemplate, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, requestPath, nil)
			req.Header.Set("Accept", "application/json")

			ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
			defer cancel()
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()
			defer resp.Body.Close()

			// Read the body into memory so we can show it in error logs even
			// after the validator consumes its reader.
			bodyBytes, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				t.Fatalf("read response body: %v", readErr)
			}

			// Only validate 2xx responses. 4xx/5xx are acceptable (endpoint
			// might require prerequisites we can't easily set up), and the
			// spec's error schemas are already covered by specific tests.
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				t.Logf(
					"status=%d (accepted, spec not validated): %s",
					resp.StatusCode,
					strings.TrimSpace(string(bodyBytes)),
				)
				nonSuccess++
				return
			}

			route, pathParams, err := specRouter.FindRoute(req)
			if err != nil {
				t.Fatalf("route %s not found in spec: %v", requestPath, err)
			}

			if err := openapi3filter.ValidateResponse(
				req.Context(),
				&openapi3filter.ResponseValidationInput{
					RequestValidationInput: &openapi3filter.RequestValidationInput{
						Request:    req,
						PathParams: pathParams,
						Route:      route,
						Options:    &openapi3filter.Options{SkipSettingDefaults: true},
					},
					Status:  resp.StatusCode,
					Header:  resp.Header,
					Body:    io.NopCloser(bytes.NewReader(bodyBytes)),
					Options: &openapi3filter.Options{SkipSettingDefaults: true},
				},
			); err != nil {
				t.Errorf(
					"response validation failed: %v\nresponse body: %s",
					err,
					string(bodyBytes),
				)
				return
			}

			validated++
		})
	}

	t.Logf(
		"validated=%d non-2xx=%d unresolved-params=%d",
		validated, nonSuccess, unresolved,
	)
}

// skipEndpoint returns true for paths that can't be exercised by a generic
// contract test: streaming endpoints, SPA fallbacks, binary bundles, and
// proxies whose schemas are intentionally opaque.
func skipEndpoint(path string) bool {
	// Streaming endpoints need real event sources.
	if strings.HasSuffix(path, "/logs") || path == "/events" {
		return true
	}

	// Prometheus proxy passes through responses without conforming to an
	// internal schema; covered by dedicated tests.
	if path == "/metrics" || strings.HasPrefix(path, "/metrics/labels") {
		return true
	}

	// Topology returns custom content types (application/vnd.jgf+json,
	// application/graphml+xml, text/vnd.graphviz) that openapi3filter's
	// default body decoders don't understand. Covered by dedicated tests.
	if path == "/topology" {
		return true
	}

	// Scalar bundle is a binary asset, not JSON.
	if path == "/api/scalar.js" {
		return true
	}

	// pprof endpoints are opt-in and not content-negotiated.
	if strings.HasPrefix(path, "/debug/pprof") {
		return true
	}

	// Auth flow endpoints return redirects.
	if strings.HasPrefix(path, "/auth/login") || strings.HasPrefix(path, "/auth/callback") {
		return true
	}

	// Plugin endpoints hit a Docker client that isn't stubbed here.
	if strings.HasPrefix(path, "/plugins") {
		return true
	}

	return false
}

// resolvePath substitutes path parameters in a spec path template with known
// fixture IDs. Returns (resolved, true) if every {param} was substituted,
// or (template, false) if any remain.
func resolvePath(template string) (string, bool) {
	replacements := map[string]string{
		"/nodes/{id}":        "/nodes/node-1",
		"/services/{id}":     "/services/svc-1",
		"/tasks/{id}":        "/tasks/task-1",
		"/stacks/{name}":     "/stacks/myapp",
		"/configs/{id}":      "/configs/cfg-1",
		"/secrets/{id}":      "/secrets/sec-1",
		"/networks/{id}":     "/networks/net-1",
		"/volumes/{name}":    "/volumes/vol-1",
		"/api/errors/{code}": "/api/errors/SVC001",
	}

	for prefix, replacement := range replacements {
		if template == prefix {
			return replacement, true
		}

		// Sub-paths like /nodes/{id}/tasks, /services/{id}/env, etc.
		if strings.HasPrefix(template, prefix+"/") {
			return strings.Replace(template, prefix, replacement, 1), true
		}
	}

	// No path parameters at all — use template as-is.
	if !strings.Contains(template, "{") {
		return template, true
	}

	return template, false
}
