package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"

	"github.com/radiergummi/cetacean/internal/api/sbom"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
)

// licenseTextID is an id GET /-/licenses/texts/{id} actually serves. The ids
// are content hashes of the embedded texts, not SPDX identifiers, and they
// change whenever the SBOM is regenerated — so it is read out of the same
// embedded artifact the handler answers from rather than written down.
var licenseTextID = func() string {
	var doc struct {
		Components []struct {
			TextID string `json:"textId"`
		} `json:"components"`
	}

	if err := json.Unmarshal(sbom.ProjectedJSON(), &doc); err != nil {
		return ""
	}

	for _, component := range doc.Components {
		if component.TextID != "" {
			return component.TextID
		}
	}

	return ""
}()

// TestEveryReadEndpointMatchesSpec walks every GET operation in the OpenAPI
// spec, issues a request with substituted path parameters, and validates the
// response body against the spec's response schema. Operations whose path
// parameters can't be resolved from fixtures are skipped with a log line so
// gaps are visible.
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

	// The SBOM endpoint's canonical URL ends in .json. The negotiate middleware
	// strips that suffix before dispatch, mutating r.URL.Path in place, so the
	// request object seen by FindRoute no longer matches the spec path. End-to-end
	// routing coverage is provided by TestLicensesEndpointsRouteThroughNegotiate.
	if path == "/-/sbom.cdx.json" {
		return true
	}

	return false
}

// pathFixtures maps a spec path prefix to the concrete path resolvePath
// substitutes for it: every resource in specFixtureIDs, plus the three
// parameters that name something other than a cached resource.
var pathFixtures = func() map[string]string {
	fixtures := map[string]string{
		"/api/errors/{code}": "/api/errors/SVC001",

		// Not cache fixtures: a text id the embedded license set carries, and
		// a label name the Prometheus proxy would forward.
		"/-/licenses/texts/{id}": "/-/licenses/texts/" + licenseTextID,
		"/metrics/labels/{name}": "/metrics/labels/job",
	}

	for template, id := range specFixtureIDs {
		fixtures[template] = template[:strings.LastIndex(template, "/")+1] + id
	}

	return fixtures
}()

// resolvePath substitutes path parameters in a spec path template with known
// fixture IDs. Returns (resolved, true) if every {param} was substituted,
// or (template, false) if any remain.
func resolvePath(template string) (string, bool) {
	for prefix, replacement := range pathFixtures {
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

// TestSpecFixtureIDsResolve drives the detail endpoint of every specFixtureIDs
// entry against both fixtures that claim to seed it. Neither walk that reads
// the table would notice an id gone stale: the contract walk logs a non-2xx
// and moves on, and the operations-level probe reads its tier off the gate,
// which answers before the resource is ever looked up. Coverage would drain
// away in silence.
func TestSpecFixtureIDsResolve(t *testing.T) {
	specBytes, _, _ := loadTestSpec(t)

	c := cache.New(nil)
	populateSpecFixtures(c)

	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	defer b.Close()

	fixtures := map[string]http.Handler{
		"populateSpecFixtures": newTestRouter(t, newTestHandlers(t, withCache(c)), b, specBytes),
		"newSeededTestRouter":  newSeededTestRouter(t),
	}

	for name, router := range fixtures {
		for template := range specFixtureIDs {
			// Plugins are answered by a Docker client rather than the cache,
			// so a plugin id proves nothing about either fixture's seeding.
			if strings.HasPrefix(template, "/plugins/") {
				continue
			}

			path, _ := resolvePath(template)

			t.Run(name+" "+path, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				req.Header.Set("Accept", "application/json")

				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					t.Errorf(
						"status=%d, want 200 — the fixture seeds no resource under "+
							"the id specFixtureIDs addresses it by",
						rec.Code,
					)
				}
			})
		}
	}
}

// writeEndpointCheck is one non-GET spec operation exercised by the
// behavioural half of TestEveryWriteEndpointDocumentsPreconditions: the spec
// path template it proves coverage for, and the concrete request that drives
// it against newSeededTestRouter.
type writeEndpointCheck struct {
	method      string
	template    string
	path        string
	body        string
	contentType string
}

// resourceIDPlaceholder inverts specFixtureIDs: each fixture ID against the
// OpenAPI path parameter it fills. specTemplate uses it to turn a
// pairedEndpoints concrete path back into the spec's template, so the 30
// precondition-carrying rows don't have to be retyped here.
var resourceIDPlaceholder = func() map[string]string {
	placeholders := make(map[string]string, len(specFixtureIDs))

	for template, id := range specFixtureIDs {
		placeholders[id] = template[strings.LastIndex(template, "/")+1:]
	}

	return placeholders
}()

// specTemplate turns a concrete fixture path (as used in pairedEndpoints)
// back into the OpenAPI path template it was resolved from.
func specTemplate(concretePath string) string {
	segments := strings.Split(concretePath, "/")
	for i, segment := range segments {
		if placeholder, ok := resourceIDPlaceholder[segment]; ok {
			segments[i] = placeholder
		}
	}
	return strings.Join(segments, "/")
}

// preconditionedWriteEndpointChecks adapts pairedEndpoints (Task 7's table of
// every route that got an If-Match wrapper) into writeEndpointChecks, so this
// test's behavioural pass proves presence of a precondition from the very
// same rows that prove it round-trips, rather than a second hand-kept list.
func preconditionedWriteEndpointChecks() []writeEndpointCheck {
	checks := make([]writeEndpointCheck, 0, len(pairedEndpoints))
	for _, e := range pairedEndpoints {
		checks = append(checks, writeEndpointCheck{
			method:      e.writeMethod,
			template:    specTemplate(e.writePath),
			path:        e.writePath,
			body:        e.body,
			contentType: e.contentType,
		})
	}
	return checks
}

// unpreconditionedWriteEndpointChecks drives every non-GET spec operation that
// carries no If-Match precondition. Several reach a nil or under-stubbed
// backend and error; only h.precond ever answers 412, so any other status
// proves the point.
var unpreconditionedWriteEndpointChecks = []writeEndpointCheck{
	{"POST", "/configs", "/configs",
		`{"name":"cfg2","data":"aGVsbG8="}`, "application/json"},
	{"POST", "/secrets", "/secrets",
		`{"name":"sec2","data":"aHVudGVyMg=="}`, "application/json"},
	{"PUT", "/nodes/{id}/availability", "/nodes/node1/availability",
		`{"availability":"drain"}`, "application/json"},
	{"PUT", "/services/{id}/scale", "/services/svc1/scale",
		`{"replicas":3}`, "application/json"},
	{"PUT", "/services/{id}/image", "/services/svc1/image",
		`{"image":"nginx:1.28"}`, "application/json"},
	{"POST", "/services/{id}/rollback", "/services/svc1/rollback", "", ""},
	{"POST", "/services/{id}/restart", "/services/svc1/restart", "", ""},
	{"POST", "/-/resync", "/-/resync", "", ""},
	{"POST", "/plugins", "/plugins",
		`{"remote":"registry.example.com/plugin:latest"}`, "application/json"},
	{"POST", "/plugins/privileges", "/plugins/privileges",
		`{"remote":"registry.example.com/plugin:latest"}`, "application/json"},
	{"POST", "/plugins/{name}/enable", "/plugins/plug1/enable", "", ""},
	{"POST", "/plugins/{name}/disable", "/plugins/plug1/disable", "", ""},
	{"POST", "/plugins/{name}/upgrade", "/plugins/plug1/upgrade",
		`{"remote":"registry.example.com/plugin:v2"}`, "application/json"},
	{"PATCH", "/plugins/{name}/settings", "/plugins/plug1/settings",
		`{"args":["FOO=bar"]}`, "application/json"},
	{"PATCH", "/swarm/orchestration", "/swarm/orchestration",
		`{}`, "application/merge-patch+json"},
	{"PATCH", "/swarm/raft", "/swarm/raft",
		`{}`, "application/merge-patch+json"},
	{"PATCH", "/swarm/dispatcher", "/swarm/dispatcher",
		`{}`, "application/merge-patch+json"},
	{"PATCH", "/swarm/ca", "/swarm/ca",
		`{}`, "application/merge-patch+json"},
	{"PATCH", "/swarm/encryption", "/swarm/encryption",
		`{}`, "application/merge-patch+json"},
	{"POST", "/swarm/rotate-token", "/swarm/rotate-token",
		`{"target":"worker"}`, "application/json"},
	{"POST", "/swarm/rotate-unlock-key", "/swarm/rotate-unlock-key", "", ""},
	{"POST", "/swarm/force-rotate-ca", "/swarm/force-rotate-ca", "", ""},
	{"POST", "/swarm/unlock", "/swarm/unlock",
		`{"unlockKey":"SWMKEY-x"}`, "application/json"},
}

// writeEndpointChecksExcluded lists spec operations the behavioural pass cannot
// drive, with the reason. An entry may only cover an endpoint with no
// precondition; the coverage assertion below fails outright if one of these
// grows a documented If-Match.
var writeEndpointChecksExcluded = map[string]string{
	"POST /auth/logout": "session endpoint, not a cluster resource with " +
		"a representation for If-Match to compare against. The NoneProvider " +
		"newSeededTestRouter uses doesn't register it (only OIDCProvider " +
		"does), so no router path reaches the real handler to exercise here " +
		"— it falls through to the SPA handler instead.",
}

// TestEveryWriteEndpointDocumentsPreconditions holds the spec and the router
// together in both directions: the spec-internal pass checks that every
// non-GET operation documents a 412 iff it documents an If-Match parameter,
// and the behavioural pass drives each against the router with a bogus
// If-Match, requiring 412 exactly when the spec documents the precondition.
// The 412 body shape is not validated.
func TestEveryWriteEndpointDocumentsPreconditions(t *testing.T) {
	_, doc, _ := loadTestSpec(t)

	documented := map[string]bool{}

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if method == http.MethodGet || method == http.MethodHead {
				continue
			}

			_, has412 := op.Responses.Map()["412"]
			hasIfMatch := false
			for _, p := range op.Parameters {
				if p.Value != nil && p.Value.Name == "If-Match" {
					hasIfMatch = true
					break
				}
			}

			if has412 != hasIfMatch {
				t.Errorf("%s %s: documents 412=%v but If-Match=%v — both or neither",
					method, path, has412, hasIfMatch)
			}

			documented[method+" "+path] = has412
		}
	}

	router := newSeededTestRouter(t)

	checks := append(preconditionedWriteEndpointChecks(), unpreconditionedWriteEndpointChecks...)
	seen := map[string]bool{}

	for _, c := range checks {
		key := c.method + " " + c.template
		seen[key] = true

		wantDoc, ok := documented[key]
		if !ok {
			t.Errorf(
				"%s: not present in the OpenAPI spec — remove this row or fix the template",
				key,
			)
			continue
		}

		t.Run(key, func(t *testing.T) {
			var body io.Reader
			if c.body != "" {
				body = strings.NewReader(c.body)
			}

			req := httptest.NewRequest(c.method, c.path, body)
			req.Header.Set("If-Match", `"bogus-etag"`)
			req.Header.Set("Accept", "application/json")
			if c.contentType != "" {
				req.Header.Set("Content-Type", c.contentType)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			got412 := rec.Code == http.StatusPreconditionFailed
			if got412 != wantDoc {
				t.Errorf(
					"%s %s = %d (412=%v), want documented=%v; body: %s",
					c.method, c.path, rec.Code, got412, wantDoc, rec.Body.String(),
				)
			}
		})
	}

	for key, hasPrecond := range documented {
		if seen[key] {
			continue
		}
		if reason, ok := writeEndpointChecksExcluded[key]; ok {
			// An exclusion may only cover an endpoint with no precondition to
			// prove. One that grows a documented If-Match needs a driver, not
			// a skip.
			if hasPrecond {
				t.Errorf(
					"%s: excluded from the behavioural pass but documents an If-Match "+
						"precondition — add a driver row instead of excluding it",
					key,
				)

				continue
			}

			t.Logf("skipping %s: %s", key, reason)

			continue
		}
		t.Errorf(
			"%s: no driver in this test's check tables — add one to "+
				"preconditionedWriteEndpointChecks/unpreconditionedWriteEndpointChecks "+
				"(or writeEndpointChecksExcluded with a reason) so the behavioural pass "+
				"can prove its precondition state (documented=%v)",
			key, hasPrecond,
		)
	}
}
