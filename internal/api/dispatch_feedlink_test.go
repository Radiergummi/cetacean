package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A feed's links may carry only the query parameters that feed actually
// reads. The links inside a feed body already obey that rule (feedQuery);
// the Link *headers* addFeedLinks emits were still built from the raw query,
// so an arbitrary parameter came back reflected. That is not the BREACH
// shape the body rule closes — a header is not part of the compressed body —
// but one rule stated twice is one rule that will drift, so both sides now
// call feedQuery.

// feedLinkHeaders drives a content-negotiated JSON request through the given
// feeds and returns the Link headers as one string.
func feedLinkHeaders(t testing.TB, target string, feeds feedHandlers) string {
	t.Helper()

	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	handler := contentNegotiated(noop, feeds, noop)

	req := httptest.NewRequest("GET", target, nil)
	req = withContentType(req, ContentTypeJSON)
	rec := httptest.NewRecorder()
	handler(rec, req)

	return strings.Join(rec.Header().Values("Link"), ", ")
}

// bothFeeds is a feedHandlers with both formats wired and no extra query
// parameters declared — the shape every endpoint but /search registers.
func bothFeeds() feedHandlers {
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	return feedHandlers{atom: noop, jsonFeed: noop}
}

// TestFeedLinkHeadersDropUnreadParameters fails while addFeedLinks builds its
// href from r.URL.RawQuery: the unread parameter comes back in both links.
func TestFeedLinkHeadersDropUnreadParameters(t *testing.T) {
	links := feedLinkHeaders(t, "/nodes?sort=name&unread=whatever", bothFeeds())

	if strings.Contains(links, "unread") {
		t.Errorf("Link headers echo a parameter no feed reads: %q", links)
	}
	if strings.Contains(links, "sort=name") {
		t.Errorf("Link headers echo ?sort=, which no feed reads: %q", links)
	}
}

// TestFeedLinkHeadersKeepPaginationParameters fails if the fix over-corrects
// and strips the cursor pair every feed does read.
func TestFeedLinkHeadersKeepPaginationParameters(t *testing.T) {
	links := feedLinkHeaders(t, "/nodes?before=42&limit=10", bothFeeds())

	for _, want := range []string{"before=42", "limit=10"} {
		if !strings.Contains(links, want) {
			t.Errorf("Link headers dropped %q, which every feed reads: %q", want, links)
		}
	}
}

// TestSearchFeedDeclaresItsQueryOnBothSides is the point of the change: ?q=
// is declared twice — on the route, for the alternate Link header, and in
// searchFeedData, for the links inside the feed — and the two must agree.
// It drives the real registered route, so dropping either declaration fails
// here rather than silently advertising an alternate that searches for
// something else.
func TestSearchFeedDeclaresItsQueryOnBothSides(t *testing.T) {
	router := newSeededTestRouter(t)
	const target = "/search?q=app&limit=5&unread=whatever"

	jsonReq := httptest.NewRequest("GET", target, nil)
	jsonReq.Header.Set("Accept", "application/json")
	jsonRec := httptest.NewRecorder()
	router.ServeHTTP(jsonRec, jsonReq)

	header := strings.Join(jsonRec.Header().Values("Link"), ", ")
	if !strings.Contains(header, "q=app") {
		t.Errorf("the route does not declare ?q=, so its alternate Link "+
			"header addresses a different search: %q", header)
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
		t.Error("searchFeedData does not declare ?q=, so the feed's own " +
			"links address a different search")
	}
	if strings.Contains(body, "unread") {
		t.Error("the feed body carries a parameter no feed reads")
	}
}
