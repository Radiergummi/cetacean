package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// RFC 9110 §12.5.1: a type's weight is the one its most specific matching
// range gives it, so a wildcard cannot re-admit a type named at q=0. An alias
// of a type counts only where it is named: the response carries the
// canonical type, which is the one a wildcard is judged against.
func TestTheMostSpecificRangeSetsTheWeight(t *testing.T) {
	for accept, want := range map[string]ContentType{
		"application/json;q=0, */*":                ContentTypeHTML,
		"*/*, application/json;q=0":                ContentTypeHTML,
		"application/*;q=0.1, application/json":    ContentTypeJSON,
		"text/csv;q=0.5, text/html;q=0, */*;q=0.1": ContentTypeCSV,
		"text/*":              ContentTypeHTML,
		"text/event-stream":   ContentTypeSSE,
		"application/ld+json": ContentTypeJSON,
		"application/ld+json;q=0.9, text/html;q=0.5": ContentTypeJSON,
		"application/json;q=0, application/*":        ContentTypeAtom,
		"application/json;q=0, application/ld+json":  ContentTypeJSON,
		"*/*": ContentTypeJSON,
		"text/html;q=0.5, application/json;q=0.5":         ContentTypeHTML,
		"application/json;q=0":                            ContentTypeUnsupported,
		"text/event-stream;q=0.2, application/json;q=0.8": ContentTypeJSON,
	} {
		if got := parseAccept(accept); got != want {
			t.Errorf("Accept: %s negotiated %v, want %v", accept, got, want)
		}
	}
}

func TestAWildcardDoesNotReadmitATypeRefusedByName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/cluster", nil)
	req.Header.Set("Accept", "application/json;q=0, application/ld+json;q=0, */*")

	negotiate(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if acceptable(r, ContentTypeJSON) {
			t.Error("JSON is acceptable although both its types were refused by name")
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
}

// An endpoint answers with the best of what it serves, not only the best of
// everything: CSV preferred on a detail route still leaves JSON acceptable.
func TestADetailRouteServesTheBestOfItsOwnTypes(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	router := newTestRouterWithCache(t, c)

	for accept, want := range map[string]int{
		"text/csv, application/json;q=0.5": http.StatusOK,
		"text/csv":                         http.StatusNotAcceptable,
	} {
		req := httptest.NewRequest(http.MethodGet, "/nodes/n1", nil)
		req.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != want {
			t.Errorf("Accept: %s answered %d, want %d: %s", accept, w.Code, want, w.Body.String())
		}
		if want == http.StatusOK && w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("Accept: %s answered %s, want JSON", accept, w.Header().Get("Content-Type"))
		}
	}
}
