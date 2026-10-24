package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// RFC 9110 §12.5.1: a type's weight is the one its most specific matching
// range gives it, so a wildcard cannot re-admit a type named at q=0. An alias
// of a type counts only where it is named and the type sent is not: the
// response carries the type sent, which is the one a wildcard is judged against.
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
		"application/json;q=0, application/ld+json":  ContentTypeUnsupported,
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

// The type an endpoint chose among its own is the one its handler renders: a
// handler that re-read the best of every endpoint's would render a type the
// client ranked below the one it was dispatched for.
func TestTheArmRendersTheTypeItWasChosenFor(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID:   "s1",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
		},
	)
	router := newTestRouterWithCache(t, c)

	for _, tc := range []struct{ path, accept, want string }{
		{"/services", "application/vnd.jgf+json, text/csv;q=0.5", "text/csv"},
		{"/tasks", "application/vnd.jgf+json, text/csv;q=0.5", "text/csv"},
		{"/nodes", "application/vnd.jgf+json, text/csv;q=0.5", "text/csv"},
		{"/api/asyncapi", "text/html, application/yaml;q=0.5", asyncAPIYAMLMediaType},
		{"/api", "text/csv, application/yaml;q=0.5", openAPIYAMLMediaType},
		{"/events", "application/atom+xml;q=0.5, application/json", "application/atom+xml"},
		{"/topology", "text/csv, application/vnd.jgf+json;q=0.5", "application/vnd.jgf+json"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Accept", tc.accept)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if got := w.Header().Get("Content-Type"); w.Code != http.StatusOK ||
			!strings.HasPrefix(got, tc.want) {
			t.Errorf("GET %s, Accept: %s answered %d %s, want 200 %s",
				tc.path, tc.accept, w.Code, got, tc.want)
		}
	}
}

// RFC 9110 §12.5.1: a range with parameters is more specific than one
// without, and matches only a representation carrying those parameters, so
// refusing a JSON profile this API never sends does not refuse its JSON.
func TestARangeWithParametersGovernsOnlyWhatCarriesThem(t *testing.T) {
	for accept, want := range map[string]ContentType{
		`application/json;profile="x";q=0, application/json`: ContentTypeJSON,
		`application/json, application/json;profile="x";q=0`: ContentTypeJSON,
		`application/json;profile="x"`:                       ContentTypeUnsupported,
		`application/json;charset=UTF-8`:                     ContentTypeJSON,
		`text/csv;header=present, application/json;q=0.5`:    ContentTypeCSV,
		`text/csv;header=absent, application/json;q=0.5`:     ContentTypeJSON,
		`text/csv;header=present;q=0, text/csv`:              ContentTypeUnsupported,
		`text/html;level="a,b";q=0, text/html`:               ContentTypeHTML,
	} {
		if got := parseAccept(accept); got != want {
			t.Errorf("Accept: %s negotiated %v, want %v", accept, got, want)
		}
	}
}

// A type is weighed by the media type the endpoint sends for it: refusing that
// one by name refuses the representation, whatever a wildcard or another
// spelling admits.
func TestATypeIsWeighedByWhatTheEndpointSends(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "s1", Spec: swarm.ServiceSpec{
		Annotations:  swarm.Annotations{Name: "web"},
		TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: "nginx"}},
	}})
	router := newTestRouterWithCache(t, c)

	for _, tc := range []struct{ path, accept, want string }{
		{"/services/s1", "application/yaml;q=0, application/*;q=0.9, application/json;q=0.5", "application/atom+xml"},
		{"/services/s1", "application/yaml;q=0, application/vnd.oai.openapi", ""},
		{"/services/s1", "text/yaml, application/json;q=0.5", "application/yaml"},
		{"/api", "application/vnd.oai.openapi;q=0, application/*", "application/json"},
		{"/api", "application/yaml", openAPIYAMLMediaType},
		{"/api/asyncapi", "application/vnd.aai.asyncapi+yaml;version=2.6.0, application/json;q=0.1", asyncAPIMediaType},
		{"/api/context.jsonld", "application/ld+json;q=0, */*", ""},
		{"/topology", "application/vnd.jgf+json;q=0, application/json", ""},
		{"/cluster", "application/json;q=0, application/ld+json", ""},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Accept", tc.accept)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		got := w.Header().Get("Content-Type")
		switch {
		case tc.want == "" && w.Code != http.StatusNotAcceptable:
			t.Errorf("GET %s, Accept: %s answered %d %s, want 406",
				tc.path, tc.accept, w.Code, got)
		case tc.want != "" && (w.Code != http.StatusOK || !strings.HasPrefix(got, tc.want)):
			t.Errorf("GET %s, Accept: %s answered %d %s, want 200 %s",
				tc.path, tc.accept, w.Code, got, tc.want)
		}
	}
}
