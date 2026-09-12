package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/linkset"
)

// catalogDocument is the wire shape, declared separately from linkset.Document
// so parsing does not go through the code that wrote it.
type catalogDocument struct {
	Linkset []map[string]json.RawMessage `json:"linkset"`
}

type catalogTarget struct {
	Href  string `json:"href"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// contexts keys each link context by its anchor.
func (d catalogDocument) contexts(t *testing.T) map[string]map[string][]catalogTarget {
	t.Helper()

	out := make(map[string]map[string][]catalogTarget, len(d.Linkset))

	for _, raw := range d.Linkset {
		anchorRaw, ok := raw["anchor"]
		if !ok {
			t.Fatalf("link context carries no anchor: %v", raw)
		}

		var anchor string
		if err := json.Unmarshal(anchorRaw, &anchor); err != nil {
			t.Fatalf("anchor is not a string: %v", err)
		}

		relations := make(map[string][]catalogTarget)

		for name, value := range raw {
			if name == "anchor" {
				continue
			}

			var targets []catalogTarget
			if err := json.Unmarshal(value, &targets); err != nil {
				t.Fatalf("relation %q is not a list of targets: %v", name, err)
			}

			relations[name] = targets
		}

		out[anchor] = relations
	}

	return out
}

// targets returns every link target, in a stable order.
func (d catalogDocument) targets(t *testing.T) []catalogTarget {
	t.Helper()

	var found []catalogTarget

	for _, relations := range d.contexts(t) {
		for _, targets := range relations {
			found = append(found, targets...)
		}
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].Href != found[j].Href {
			return found[i].Href < found[j].Href
		}

		return found[i].Type < found[j].Type
	})

	return found
}

// fetchCatalog drives the catalog route and parses the response. An empty
// accept sends no Accept header.
func fetchCatalog(
	t *testing.T,
	router http.Handler,
	requestPath, accept string,
) catalogDocument {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, requestPath, nil)
	req.Host = "cetacean.example.com"

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	// RFC 9727 §4.2 permits no other format.
	if got := rec.Header().Get("Content-Type"); got != linkset.MediaType {
		t.Errorf("Content-Type = %q, want %q", got, linkset.MediaType)
	}

	var doc catalogDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("parse catalog: %v\nbody: %s", err, rec.Body.String())
	}

	return doc
}

// TestAPICatalogTargetsAnswerAsAdvertised keeps the catalog honest: every
// target is driven against the router that published it and must answer with
// the media type the catalog claimed.
//
// The assertion is the media type, not "not 404" — the SPA fallback answers
// every unrouted path with 200 and HTML, so a 404 check passes even after a
// route is renamed out from under the catalog.
func TestAPICatalogTargetsAnswerAsAdvertised(t *testing.T) {
	router := newSeededTestRouter(t)

	doc := fetchCatalog(t, router, apiCatalogPath, "")

	targets := doc.targets(t)
	if len(targets) == 0 {
		t.Fatal("the catalog names no targets at all")
	}

	var typed int

	for _, target := range targets {
		if target.Type != "" {
			typed++
		}

		t.Run(target.Href+" "+target.Type, func(t *testing.T) {
			parsed, err := url.Parse(target.Href)
			if err != nil {
				t.Fatalf("not a URL: %v", err)
			}

			if !parsed.IsAbs() {
				t.Fatal("href is relative; a linkset target must be a URI")
			}

			accept := target.Type
			if accept == "" {
				accept = "*/*"
			}

			req := httptest.NewRequest(http.MethodGet, parsed.Path, nil)
			req.Header.Set("Accept", accept)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code == http.StatusNotFound {
				t.Fatalf(
					"the catalog publishes this URI but the router answers 404: %s",
					strings.TrimSpace(rec.Body.String()),
				)
			}

			if target.Type == "" {
				return
			}

			// The response may carry parameters the catalog does not state.
			got, _, _ := strings.Cut(rec.Header().Get("Content-Type"), ";")
			if strings.TrimSpace(got) != target.Type {
				t.Errorf(
					"the catalog advertises %q but the endpoint answered %q (status %d) "+
						"— either the catalog is stale, or the route moved and the SPA "+
						"fallback is answering in its place",
					target.Type, got, rec.Code,
				)
			}
		})
	}

	// Without a floor, a catalog that stopped declaring types walks clean
	// while asserting nothing.
	if typed < 4 {
		t.Errorf("only %d targets declared a media type; the walk checks little", typed)
	}
}

// TestAPICatalogCarriesItemLinks pins RFC 9727 §3.1's only MUST: item names a
// member API. A catalog of service-desc and describedby links alone is a
// sitemap of one API, which is not what this URI means.
func TestAPICatalogCarriesItemLinks(t *testing.T) {
	router := newSeededTestRouter(t)

	doc := fetchCatalog(t, router, apiCatalogPath, "")
	contexts := doc.contexts(t)

	catalogAnchor := "http://cetacean.example.com" + apiCatalogPath

	relations, ok := contexts[catalogAnchor]
	if !ok {
		t.Fatalf("no context anchored at the catalog itself (%s); have %v",
			catalogAnchor, contexts)
	}

	items := relations["item"]
	if len(items) == 0 {
		t.Fatal("the catalog carries no item links — RFC 9727 §3.1's only MUST")
	}

	for _, item := range items {
		if item.Href == "" {
			t.Error("an item link carries no href")
		}

		if item.Title == "" {
			t.Errorf("the item %q carries no title", item.Href)
		}
	}
}

// TestAPICatalogOmitsUnmountedAPIs holds the catalog to what the process
// serves. A catalog keyed on "is MCP on" alone would advertise a metadata
// document that does not exist when auth.mode is "none".
func TestAPICatalogOmitsUnmountedAPIs(t *testing.T) {
	tests := []struct {
		name        string
		mounts      catalogMounts
		wantMCPItem bool
		wantMetaDoc bool
	}{
		{
			name:   "neither mounted",
			mounts: catalogMounts{},
		},
		{
			name:        "MCP without an authorization server",
			mounts:      catalogMounts{mcp: true},
			wantMCPItem: true,
		},
		{
			name:        "MCP with an authorization server",
			mounts:      catalogMounts{mcp: true, oauthMetadata: true},
			wantMCPItem: true,
			wantMetaDoc: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, apiCatalogPath, nil)
			req.Host = "cetacean.example.com"
			rec := httptest.NewRecorder()

			HandleAPICatalog(tt.mounts)(rec, req)

			body := rec.Body.String()

			names := strings.Contains(body, `"http://cetacean.example.com/mcp"`)
			if names != tt.wantMCPItem {
				t.Errorf("names /mcp = %v, want %v; body: %s", names, tt.wantMCPItem, body)
			}

			meta := strings.Contains(body, "oauth-protected-resource")
			if meta != tt.wantMetaDoc {
				t.Errorf(
					"names the protected resource metadata = %v, want %v; body: %s",
					meta, tt.wantMetaDoc, body,
				)
			}
		})
	}
}

// TestAPICatalogIsAbsoluteUnderABasePath: under CETACEAN_BASE_PATH every URI
// must carry the prefix, or it addresses a path the deployment does not
// serve.
func TestAPICatalogIsAbsoluteUnderABasePath(t *testing.T) {
	router := newBasePathTestRouter(t, "/cetacean")

	doc := fetchCatalog(t, router, "/cetacean"+apiCatalogPath, "")

	targets := doc.targets(t)
	if len(targets) == 0 {
		t.Fatal("the catalog names no targets at all")
	}

	for _, target := range targets {
		if !strings.HasPrefix(target.Href, "http://cetacean.example.com/cetacean") {
			t.Errorf("href %q does not carry the base path", target.Href)
		}
	}
}

// TestAPICatalogETagIsStable fails if relations serialize in map order: Go
// randomizes it, so no conditional GET would ever match.
func TestAPICatalogETagIsStable(t *testing.T) {
	router := newSeededTestRouter(t)

	var first string

	for i := range 8 {
		req := httptest.NewRequest(http.MethodGet, apiCatalogPath, nil)
		req.Host = "cetacean.example.com"
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Fatal("no ETag on the catalog")
		}

		if i == 0 {
			first = etag

			continue
		}

		if etag != first {
			t.Fatalf("ETag changed between identical requests: %q then %q", first, etag)
		}
	}
}
