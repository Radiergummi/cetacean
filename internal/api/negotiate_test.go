package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/linkset"
	"github.com/radiergummi/cetacean/internal/cache"
)

// parseAccept is the whole header-to-type path in one call. negotiate drives
// the two halves separately, since it needs the ranges for rangesAcceptHTML.
func parseAccept(accept string) ContentType {
	return bestMatch(parseAcceptRanges(accept))
}

func TestNegotiate(t *testing.T) {
	// Helper: runs a request through the negotiate middleware and returns
	// the resolved ContentType and the path seen by the inner handler.
	run := func(path string, accept string) (ContentType, string) {
		var ct ContentType
		var innerPath string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ct = ContentTypeFromContext(r.Context())
			innerPath = r.URL.Path
		})

		req := httptest.NewRequest("GET", path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()
		negotiate(inner).ServeHTTP(rec, req)
		return ct, innerPath
	}

	t.Run("extension .json strips suffix and returns JSON", func(t *testing.T) {
		ct, path := run("/services.json", "")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
		if path != "/services" {
			t.Errorf("got path %q, want /services", path)
		}
	})

	t.Run("extension .html strips suffix and returns HTML", func(t *testing.T) {
		ct, path := run("/services.html", "")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
		if path != "/services" {
			t.Errorf("got path %q, want /services", path)
		}
	})

	t.Run("Accept application/json", func(t *testing.T) {
		ct, _ := run("/services", "application/json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("Accept application/vnd.cetacean.v1+json", func(t *testing.T) {
		ct, _ := run("/services", "application/vnd.cetacean.v1+json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("Accept text/html browser default", func(t *testing.T) {
		ct, _ := run("/services", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("Accept text/event-stream", func(t *testing.T) {
		ct, _ := run("/events", "text/event-stream")
		if ct != ContentTypeSSE {
			t.Errorf("got %v, want SSE", ct)
		}
	})

	t.Run("no Accept header defaults to JSON", func(t *testing.T) {
		ct, _ := run("/services", "")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("Accept */* defaults to JSON", func(t *testing.T) {
		ct, _ := run("/services", "*/*")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("extension overrides Accept header", func(t *testing.T) {
		ct, path := run("/services.html", "application/json")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
		if path != "/services" {
			t.Errorf("got path %q, want /services", path)
		}
	})

	t.Run("quality value parsing prefers higher q", func(t *testing.T) {
		ct, _ := run("/services", "text/html;q=0.9, application/json;q=1.0")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("quality value parsing html wins", func(t *testing.T) {
		ct, _ := run("/services", "application/json;q=0.5, text/html;q=0.9")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("Vary header is set", func(t *testing.T) {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		req := httptest.NewRequest("GET", "/services", nil)
		rec := httptest.NewRecorder()
		negotiate(inner).ServeHTTP(rec, req)
		if v := rec.Header().Get("Vary"); v != "Accept" {
			t.Errorf("Vary header = %q, want Accept", v)
		}
	})

	t.Run("application/xhtml+xml returns HTML", func(t *testing.T) {
		ct, _ := run("/services", "application/xhtml+xml")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("path with dot but not known extension is unchanged", func(t *testing.T) {
		ct, path := run("/services/my.service", "application/json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
		if path != "/services/my.service" {
			t.Errorf("got path %q, want /services/my.service", path)
		}
	})

	t.Run("ContentTypeFromContext default is JSON", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		ct := ContentTypeFromContext(req.Context())
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	// --- Partial wildcard support ---

	t.Run("partial wildcard text/* matches HTML", func(t *testing.T) {
		ct, _ := run("/services", "text/*")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	t.Run("partial wildcard application/* matches JSON", func(t *testing.T) {
		ct, _ := run("/services", "application/*")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	// --- Specificity ---

	t.Run("specificity: exact match beats partial wildcard at same q", func(t *testing.T) {
		// text/* and application/json both q=1.0, but application/json is exact (specificity 3)
		// while text/* is partial (specificity 2).
		ct, _ := run("/services", "text/*, application/json")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("specificity: higher q wins over higher specificity", func(t *testing.T) {
		// text/html is exact (specificity 3, q=0.9); text/* is partial (specificity 2, q=1.0).
		// Higher q wins, so text/* matches. text/* matches text/html first in our preference order.
		ct, _ := run("/services", "text/html;q=0.9, text/*;q=1.0")
		if ct != ContentTypeHTML {
			t.Errorf("got %v, want HTML", ct)
		}
	})

	// --- Unsupported types are recorded, not refused ---
	// TestUnservedTypeIsRefusedByTheEndpoint drives the other half.

	t.Run("application/xml alone resolves to Unsupported", func(t *testing.T) {
		ct, _ := run("/services", "application/xml")
		if ct != ContentTypeUnsupported {
			t.Errorf("got %v, want Unsupported", ct)
		}
	})

	t.Run("text/plain alone resolves to Unsupported", func(t *testing.T) {
		ct, _ := run("/services", "text/plain")
		if ct != ContentTypeUnsupported {
			t.Errorf("got %v, want Unsupported", ct)
		}
	})

	t.Run("application/xml with */* fallback returns JSON", func(t *testing.T) {
		ct, _ := run("/services", "application/xml, */*;q=0.1")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("unsupported type with higher q still picks only match", func(t *testing.T) {
		// application/xml;q=1.0 is unsupported; application/json;q=0.5 is our only match.
		ct, _ := run("/services", "application/xml;q=1.0, application/json;q=0.5")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	// --- Malformed headers ---

	t.Run("malformed: empty commas", func(t *testing.T) {
		ct, _ := run("/services", ",,")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for all-empty ranges)", ct)
		}
	})

	t.Run("malformed: broken q value", func(t *testing.T) {
		// ";q=broken" has no valid media type before the semicolon — it's just "".
		ct, _ := run("/services", ";q=broken")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	t.Run("malformed: bare slash", func(t *testing.T) {
		ct, _ := run("/services", "/")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	t.Run("malformed: no subtype", func(t *testing.T) {
		ct, _ := run("/services", "text")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	t.Run("malformed: empty subtype after slash", func(t *testing.T) {
		ct, _ := run("/services", "text/")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON (default for malformed)", ct)
		}
	})

	// --- ContentType String ---

	t.Run("ContentTypeUnsupported String", func(t *testing.T) {
		if s := ContentTypeUnsupported.String(); s != "Unsupported" {
			t.Errorf("got %q, want Unsupported", s)
		}
	})

	t.Run("extension .atom strips suffix and returns Atom", func(t *testing.T) {
		ct, path := run("/services.atom", "")
		if ct != ContentTypeAtom {
			t.Errorf("got %v, want Atom", ct)
		}
		if path != "/services" {
			t.Errorf("got path %q, want /services", path)
		}
	})

	t.Run("Accept application/atom+xml", func(t *testing.T) {
		ct, _ := run("/services", "application/atom+xml")
		if ct != ContentTypeAtom {
			t.Errorf("got %v, want Atom", ct)
		}
	})

	t.Run("Atom lower quality than JSON prefers JSON", func(t *testing.T) {
		ct, _ := run("/services", "application/atom+xml;q=0.5, application/json;q=1.0")
		if ct != ContentTypeJSON {
			t.Errorf("got %v, want JSON", ct)
		}
	})

	t.Run("Atom higher quality than JSON prefers Atom", func(t *testing.T) {
		ct, _ := run("/services", "application/atom+xml;q=1.0, application/json;q=0.5")
		if ct != ContentTypeAtom {
			t.Errorf("got %v, want Atom", ct)
		}
	})
}

func TestParseAccept_JGF(t *testing.T) {
	ct := parseAccept("application/vnd.jgf+json")
	if ct != ContentTypeJGF {
		t.Errorf("got %v, want ContentTypeJGF", ct)
	}
}

// TestParseAccept_JSONLD: application/ld+json is an alias for the JSON branch,
// not a second representation.
func TestParseAccept_JSONLD(t *testing.T) {
	if ct := parseAccept("application/ld+json"); ct != ContentTypeJSON {
		t.Errorf("got %v, want ContentTypeJSON", ct)
	}
}

func TestParseAccept_GraphML(t *testing.T) {
	ct := parseAccept("application/graphml+xml")
	if ct != ContentTypeGraphML {
		t.Errorf("got %v, want ContentTypeGraphML", ct)
	}
}

func TestParseAccept_DOT(t *testing.T) {
	ct := parseAccept("text/vnd.graphviz")
	if ct != ContentTypeDOT {
		t.Errorf("got %v, want ContentTypeDOT", ct)
	}
}

func TestNegotiate_GraphMLSuffix(t *testing.T) {
	var captured ContentType
	var capturedPath string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = ContentTypeFromContext(r.Context())
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	handler := negotiate(inner)
	req := httptest.NewRequest("GET", "/topology.graphml", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if captured != ContentTypeGraphML {
		t.Errorf("expected ContentTypeGraphML, got %v", captured)
	}
	if capturedPath != "/topology" {
		t.Errorf("expected path /topology, got %s", capturedPath)
	}
}

func TestNegotiate_DOTSuffix(t *testing.T) {
	var captured ContentType
	var capturedPath string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = ContentTypeFromContext(r.Context())
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	handler := negotiate(inner)
	req := httptest.NewRequest("GET", "/topology.dot", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if captured != ContentTypeDOT {
		t.Errorf("expected ContentTypeDOT, got %v", captured)
	}
	if capturedPath != "/topology" {
		t.Errorf("expected path /topology, got %s", capturedPath)
	}
}

func TestNegotiate_JGFSuffix(t *testing.T) {
	var captured ContentType
	var capturedPath string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = ContentTypeFromContext(r.Context())
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	handler := negotiate(inner)
	req := httptest.NewRequest("GET", "/topology.jgf", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if captured != ContentTypeJGF {
		t.Errorf("expected ContentTypeJGF, got %v", captured)
	}
	if capturedPath != "/topology" {
		t.Errorf("expected path /topology, got %s", capturedPath)
	}
}

// refuse drives one request and asserts it was refused as unacceptable,
// returning the problem body so a caller can read what the endpoint offered
// instead. The code matters as much as the status: API001 is also a 406, and
// asserting the status alone would let the SSE refusal stand in for this one.
func refuse(t *testing.T, router http.Handler, path, accept string) ProblemDetail {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", accept)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotAcceptable {
		t.Fatalf("status=%d, want 406; body=%s", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type=%q, want application/problem+json", ct)
	}

	var problem ProblemDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("parse problem body: %v", err)
	}

	if !strings.Contains(problem.Type, "API003") {
		t.Errorf("problem type=%q, want it to name API003", problem.Type)
	}

	return problem
}

// An endpoint refuses every type it does not serve, including one another
// endpoint does: a graph format resolves here and is no more servable for it.
// The rows differ in how they dispatch — /services and /cluster in whether
// they carry a stream, and /api, /events and /topology without the helpers.
func TestUnservedTypeIsRefusedByTheEndpoint(t *testing.T) {
	router := newTestRouterWithCache(t, cache.New(nil))

	for _, probe := range []struct{ path, accept string }{
		{"/services", openSearchMediaType},
		{"/services", "application/vnd.jgf+json"},
		{"/services", "application/graphml+xml"},
		{"/services", "text/vnd.graphviz"},
		{"/cluster", linkset.MediaType},
		{"/cluster", "application/vnd.jgf+json"},
		{"/api", "application/graphml+xml"},
		{"/events", "application/json"},
		{"/topology", "application/atom+xml"},
	} {
		t.Run(probe.path+" "+probe.accept, func(t *testing.T) {
			refuse(t, router, probe.path, probe.accept)
		})
	}
}

// TestRefusalNamesOnlyWhatTheEndpointServes: the 406 says what to ask for
// instead, so an endpoint registered without feeds must not name Atom — it
// refuses Atom one branch above, and one endpoint cannot answer two
// contradicting 406s.
func TestRefusalNamesOnlyWhatTheEndpointServes(t *testing.T) {
	router := newTestRouterWithCache(t, cache.New(nil))

	for _, accept := range []string{"application/xml", "application/atom+xml"} {
		t.Run(accept, func(t *testing.T) {
			problem := refuse(t, router, "/cluster", accept)

			if strings.Contains(problem.Detail, "atom") {
				t.Errorf("/cluster has no feed, but its 406 offers one: %q", problem.Detail)
			}
		})
	}
}

// TestSingleRepresentationDocumentsNeedNoTableRow holds both halves of the
// claim together: neither media type resolves against supportedTypes, and each
// document still answers a client asking for it. Asserting only the second
// half is satisfied by putting the row back, which is the thing being removed.
//
// The fetch helpers assert the status and the content type.
func TestSingleRepresentationDocumentsNeedNoTableRow(t *testing.T) {
	router := newTestRouterWithCache(t, cache.New(nil))

	for _, mediaType := range []string{openSearchMediaType, linkset.MediaType} {
		if ct := parseAccept(mediaType); ct != ContentTypeUnsupported {
			t.Errorf(
				"parseAccept(%q) = %v; the document is reachable without this row",
				mediaType, ct,
			)
		}
	}

	fetchOpenSearch(t, router, openSearchPath)
	fetchCatalog(t, router, apiCatalogPath, linkset.MediaType)
}
