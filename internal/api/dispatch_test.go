package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestContentNegotiatedAtom(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	atomH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("atom"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	})

	handler := contentNegotiated(jsonH, feedHandlers{atom: atomH}, spa)

	t.Run("Atom content type dispatches to atom handler", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeAtom)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Body.String() != "atom" {
			t.Errorf("got %q, want %q", rec.Body.String(), "atom")
		}
	})

	t.Run("nil atom handler returns 406", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{}, spa)
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeAtom)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("got %d, want 406", rec.Code)
		}
	})
}

func TestContentNegotiatedJSONFeed(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	feedH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("jsonfeed"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	})

	handler := contentNegotiated(jsonH, feedHandlers{jsonFeed: feedH}, spa)

	t.Run("JSON Feed content type dispatches to JSON Feed handler", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeJSONFeed)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Body.String() != "jsonfeed" {
			t.Errorf("got %q, want %q", rec.Body.String(), "jsonfeed")
		}
	})

	t.Run("nil JSON Feed handler returns 406", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{}, spa)
		req := httptest.NewRequest("GET", "/test", nil)
		req = withContentType(req, ContentTypeJSONFeed)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("got %d, want 406", rec.Code)
		}
	})
}

func TestFeedLinkHeaders(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	atomH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("atom"))
	})
	feedH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("jsonfeed"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	t.Run("JSON response includes atom Link header when atom handler exists", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{atom: atomH}, spa)
		req := httptest.NewRequest("GET", "/services", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, `type="application/atom+xml"`) {
			t.Errorf("expected atom+xml type in Link header, got %q", links)
		}
		if !strings.Contains(links, "/services.atom") {
			t.Errorf("expected /services.atom href in Link header, got %q", links)
		}
	})

	t.Run("JSON response includes JSON Feed Link header when handler exists", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{jsonFeed: feedH}, spa)
		req := httptest.NewRequest("GET", "/nodes", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, `type="application/feed+json"`) {
			t.Errorf("expected feed+json type in Link header, got %q", links)
		}
		if !strings.Contains(links, "/nodes.feed") {
			t.Errorf("expected /nodes.feed href in Link header, got %q", links)
		}
	})

	t.Run("both feed Links present when both handlers exist", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{atom: atomH, jsonFeed: feedH}, spa)
		req := httptest.NewRequest("GET", "/tasks", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/tasks.atom") {
			t.Errorf("expected /tasks.atom in Link header, got %q", links)
		}
		if !strings.Contains(links, "/tasks.feed") {
			t.Errorf("expected /tasks.feed in Link header, got %q", links)
		}
	})

	t.Run("no feed Links when no handlers", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{}, spa)
		req := httptest.NewRequest("GET", "/cluster", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		for _, link := range rec.Header().Values("Link") {
			if strings.Contains(link, "atom") || strings.Contains(link, "feed+json") {
				t.Errorf("expected no feed Link header, got %q", link)
			}
		}
	})

	// The href carries only what the feed reads, so a parameter no feed
	// declares must not come back — the rule feedQuery states for the links
	// inside a feed body, now applied to the alternate Link headers too.
	t.Run("feed Links drop parameters no feed reads", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{atom: atomH, jsonFeed: feedH}, spa)
		req := httptest.NewRequest("GET", "/nodes?sort=name&unread=whatever", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		for _, unwanted := range []string{"unread", "sort=name"} {
			if strings.Contains(links, unwanted) {
				t.Errorf("Link headers echo %q, which no feed reads: %q", unwanted, links)
			}
		}
	})

	// The inverse: the cursor pair every feed reads must survive, or an
	// alternate link addresses the first page instead of this one.
	t.Run("feed Links keep the pagination parameters", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{atom: atomH, jsonFeed: feedH}, spa)
		req := httptest.NewRequest("GET", "/nodes?before=42&limit=10", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		for _, want := range []string{"before=42", "limit=10"} {
			if !strings.Contains(links, want) {
				t.Errorf("Link headers dropped %q, which every feed reads: %q", want, links)
			}
		}
	})

	t.Run("feed Links preserve the parameters the feed reads", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{
			atom:        atomH,
			jsonFeed:    feedH,
			queryParams: []string{"q"},
		}, spa)
		req := httptest.NewRequest("GET", "/search?q=web&limit=10", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		// Sorted, not in the order they arrived: the href is now built by
		// url.Values.Encode rather than pasted from RawQuery, so it is the
		// canonical form the feed's own self link uses.
		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/search.atom?limit=10&q=web") {
			t.Errorf("expected query params preserved in atom Link, got %q", links)
		}
		if !strings.Contains(links, "/search.feed?limit=10&q=web") {
			t.Errorf("expected query params preserved in feed Link, got %q", links)
		}
	})

	t.Run("contentNegotiatedWithSSE also adds feed Links on JSON", func(t *testing.T) {
		sseH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		handler := contentNegotiatedWithSSE(
			jsonH,
			sseH,
			feedHandlers{atom: atomH, jsonFeed: feedH},
			spa,
		)
		req := httptest.NewRequest("GET", "/nodes", nil)
		req = withContentType(req, ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, "/nodes.atom") {
			t.Errorf("expected /nodes.atom in Link header, got %q", links)
		}
		if !strings.Contains(links, "/nodes.feed") {
			t.Errorf("expected /nodes.feed in Link header, got %q", links)
		}
	})
}

// TestSearchFeedReachesFeedQueryOnBothPaths drives the real registered route
// and checks that ?q= survives, and an unread parameter does not, on both
// paths that build a feed link: the alternate Link header, built at
// registration, and the links inside the feed itself, built at render.
//
// Both read searchFeedParams, so they cannot disagree about the value — this
// is a wiring check, not a drift guard. It fails if /search stops using
// searchFeeds(), or if either path stops going through feedQuery.
func TestSearchFeedReachesFeedQueryOnBothPaths(t *testing.T) {
	router := newSeededTestRouter(t)
	const target = "/search?q=app&limit=5&unread=whatever"

	jsonReq := httptest.NewRequest("GET", target, nil)
	jsonReq.Header.Set("Accept", "application/json")
	jsonRec := httptest.NewRecorder()
	router.ServeHTTP(jsonRec, jsonReq)

	header := strings.Join(jsonRec.Header().Values("Link"), ", ")
	if !strings.Contains(header, "q=app") {
		t.Errorf("the alternate Link header addresses a different search: %q", header)
	}
	if strings.Contains(header, "unread") {
		t.Errorf("Link header carries a parameter no feed reads: %q", header)
	}

	atomReq := httptest.NewRequest("GET", target, nil)
	atomReq.Header.Set("Accept", "application/atom+xml")
	atomRec := httptest.NewRecorder()
	router.ServeHTTP(atomRec, atomReq)

	body := atomRec.Body.String()
	if atomRec.Code != http.StatusOK {
		t.Fatalf("atom status = %d, want 200; body: %s", atomRec.Code, body)
	}
	if !strings.Contains(body, "q=app") {
		t.Error("the feed's own links address a different search")
	}
	if strings.Contains(body, "unread") {
		t.Error("the feed body carries a parameter no feed reads")
	}
}

func TestContentNegotiatedCSV(t *testing.T) {
	jsonH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("json"))
	})
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("spa"))
	})

	// CSV is rendered by the handler that renders the JSON, off the list it
	// already prepared, rather than by a second handler beside it.
	t.Run("CSV dispatches to the list handler", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{csv: true}, spa)
		req := withContentType(httptest.NewRequest("GET", "/services", nil), ContentTypeCSV)
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Body.String() != "json" {
			t.Errorf("got %q, want the list handler's answer", rec.Body.String())
		}
	})

	t.Run("an endpoint that renders no CSV refuses it", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{}, spa)
		req := withContentType(httptest.NewRequest("GET", "/swarm", nil), ContentTypeCSV)
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusNotAcceptable {
			t.Errorf("got %d, want 406", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "text/csv") {
			t.Errorf("the refusal names text/csv as served: %s", rec.Body.String())
		}
	})

	t.Run("a CSV endpoint names text/csv when it refuses something else", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{csv: true}, spa)
		req := withContentType(httptest.NewRequest("GET", "/services", nil), ContentTypeJGF)
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusNotAcceptable {
			t.Fatalf("got %d, want 406", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "text/csv") {
			t.Errorf("the refusal does not name text/csv: %s", rec.Body.String())
		}
	})

	t.Run("the JSON response advertises the CSV alternate", func(t *testing.T) {
		handler := contentNegotiated(jsonH, feedHandlers{csv: true}, spa)
		req := withContentType(httptest.NewRequest("GET", "/services", nil), ContentTypeJSON)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")
		if !strings.Contains(links, `type="text/csv"`) ||
			!strings.Contains(links, "/services.csv") {
			t.Errorf("expected a text/csv alternate in Link, got %q", links)
		}
	})

	// The CSV comes off the list the JSON handler prepared, so an alternate
	// that dropped the query would hand back every row instead of the ones
	// the caller is looking at.
	t.Run("the CSV alternate carries the parameters the CSV reads", func(t *testing.T) {
		handler := contentNegotiated(
			jsonH,
			feedHandlers{csv: true, csvParams: listCSVParams},
			spa,
		)
		req := withContentType(
			httptest.NewRequest("GET", "/services?search=web&sort=name&nonsense=x", nil),
			ContentTypeJSON,
		)
		rec := httptest.NewRecorder()
		handler(rec, req)

		links := strings.Join(rec.Header().Values("Link"), ", ")

		for _, want := range []string{"search=web", "sort=name"} {
			if !strings.Contains(links, want) {
				t.Errorf("the CSV alternate drops %q: %q", want, links)
			}
		}

		// Only the declared parameters: the rest of the raw query is not
		// reflected back into a compressed response.
		if strings.Contains(links, "nonsense") {
			t.Errorf("the CSV alternate reflects an undeclared parameter: %q", links)
		}
	})
}

// TestAssembledRouterCarriesCSVParams drives the real router, because the test
// above builds feedHandlers itself and so cannot see a route that forgot to
// declare csvParams — which is exactly what a rebase dropped once.
func TestAssembledRouterCarriesCSVParams(t *testing.T) {
	router := newTestRouterWithCache(t, cache.New(nil))

	for path, want := range map[string]string{
		"/services?search=web&sort=name": "search=web",
		"/history?type=service":          "type=service",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Accept", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			links := strings.Join(rec.Header().Values("Link"), ", ")
			if !strings.Contains(links, want) {
				t.Errorf("the CSV alternate drops %q: %q", want, links)
			}
		})
	}
}
