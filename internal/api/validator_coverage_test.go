package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
)

// Compression and conditional caching are a property of which write helper a
// handler calls, not of the response — so a handler that writes its body with
// a bare w.Write is silently uncompressed and unvalidatable, and nothing
// notices. Three handlers had done exactly that (`/api`, `/api/scalar.js` and
// `/api/context.jsonld`), each found by reading rather than by failing.
//
// This is the guard that finds the fourth. Vary: Accept-Encoding is written
// in exactly two non-test places — negotiateCoding, which every write helper
// reaches, and spa.go — so its presence on a 2xx GET is a precise witness for
// "this handler went through the helpers", not a proxy for it.
//
// It walks the OpenAPI document rather than a hand-kept list, so a new
// endpoint is covered by existing it in the spec.
func TestEveryReadEndpointCarriesAValidator(t *testing.T) {
	specBytes, doc, _ := loadTestSpec(t)

	c := cache.New(nil)
	populateSpecFixtures(c)

	h := newTestHandlers(t, withCache(c))
	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	defer b.Close()

	router := newTestRouter(t, h, b, specBytes)

	// Endpoints that bypass the helpers today and are not fixed here. They
	// are listed rather than skipped so the debt is visible and cannot grow
	// quietly: an entry that starts passing fails as stale, and an endpoint
	// not listed fails outright.
	knownBypasses := map[string]string{
		"/metrics/status": "writes with plain writeJSON; one of 12 such call " +
			"sites across 7 files, so fixing it belongs with that sweep",
	}
	seenBypass := map[string]bool{}

	var checked int

	for pathTemplate, pathItem := range doc.Paths.Map() {
		if pathItem.Get == nil || skipValidatorCoverage(pathTemplate) {
			continue
		}

		requestPath, ok := resolvePath(pathTemplate)
		if !ok {
			t.Logf("skipping %s: no fixture for path parameters", pathTemplate)
			continue
		}

		t.Run(pathTemplate, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, requestPath, nil)
			req.Header.Set("Accept", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			// A non-2xx says the fixture could not reach the handler, not
			// that the handler is wrong. TestEveryReadEndpointMatchesSpec
			// reports those; here they are simply not evidence.
			if rec.Code < 200 || rec.Code >= 300 {
				t.Skipf("status=%d, handler not reached", rec.Code)
			}

			checked++

			validated := rec.Header().Get("ETag") != "" ||
				rec.Header().Get("Last-Modified") != ""
			negotiated := strings.Contains(
				strings.Join(rec.Header().Values("Vary"), ", "),
				"Accept-Encoding",
			)

			if reason, known := knownBypasses[pathTemplate]; known {
				seenBypass[pathTemplate] = true

				// Either half is enough to make the entry stale. A bypass is
				// the absence of both, because the two arrive together: the
				// write helpers are the only thing that sets a validator here,
				// and negotiateCoding is one of only two places that write
				// Vary: Accept-Encoding — spa.go is the other, and no listed
				// bypass is the SPA. Requiring both, as this did, let a
				// half-finished fix sit here indefinitely: an endpoint that
				// gained an ETag but no coding stayed listed as bypassing the
				// helpers it had already partly started using, which is the
				// one state the entry cannot honestly describe.
				if validated || negotiated {
					t.Errorf("listed as a known bypass (%s) but answered with "+
						"validator=%t and Vary: Accept-Encoding=%t — a bypass is "+
						"neither. Finish the fix and remove the entry, or record "+
						"here what the endpoint actually does",
						reason, validated, negotiated)
				}

				return
			}

			if !validated {
				t.Errorf(
					"200 with no ETag and no Last-Modified — the handler wrote "+
						"its body without a write helper, so it cannot be "+
						"revalidated; path=%s", requestPath,
				)
			}

			if !negotiated {
				t.Errorf(
					"200 without Vary: Accept-Encoding — the handler never "+
						"negotiated a content coding, so its body is served "+
						"uncompressed however large it is; path=%s", requestPath,
				)
			}
		})
	}

	if checked == 0 {
		t.Fatal("no endpoint was checked — has the spec walk broken?")
	}

	for path := range knownBypasses {
		if !seenBypass[path] {
			t.Errorf("%s is listed as a known bypass but the walk never "+
				"reached it — remove the entry or fix the fixture", path)
		}
	}
}

// skipValidatorCoverage names the GET endpoints that deliberately answer
// without going through a write helper. It is separate from skipEndpoint,
// whose exclusions are about schema validation.
//
// Note the walk only reaches endpoints the OpenAPI document declares, so it
// finds the next bypass only for those. /api/scalar.js is not among them — the
// spec has no path for it — and is covered instead by TestAPIDocsAreCompressed
// and TestAPIDocsRevalidate, which drive the route directly.
func skipValidatorCoverage(path string) bool {
	switch {
	// Streams: the body is open-ended, so there is nothing to hash and
	// nothing to compress without defeating the stream.
	case strings.HasSuffix(path, "/logs"), path == "/events":
		return true

	// Proxied straight from Prometheus, whose own headers pass through.
	case path == "/metrics", strings.HasPrefix(path, "/metrics/labels"):
		return true

	// Opt-in debug surface served by net/http/pprof, not by this package.
	case strings.HasPrefix(path, "/debug/pprof"):
		return true

	// Redirects and liveness probes: no representation to validate.
	case strings.HasPrefix(path, "/auth/login"),
		strings.HasPrefix(path, "/auth/callback"),
		strings.HasPrefix(path, "/-/health"),
		strings.HasPrefix(path, "/-/ready"):
		return true

	// Per-identity and explicitly Cache-Control: no-store. A validator on a
	// response that must never be cached invites exactly the sharing it is
	// marked against.
	case path == "/auth/whoami":
		return true

	// Prometheus exposition, written by promhttp rather than by this package.
	case path == "/-/metrics":
		return true

	default:
		return false
	}
}
