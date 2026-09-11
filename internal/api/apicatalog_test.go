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
	"github.com/radiergummi/cetacean/internal/cache"
)

// catalogDocument is the wire shape of a linkset, read back rather than
// reusing linkset.Document: that type marshals but does not unmarshal, and a
// test that parsed with the same code that wrote would confirm nothing about
// the bytes on the wire.
type catalogDocument struct {
	Linkset []map[string]json.RawMessage `json:"linkset"`
}

type catalogTarget struct {
	Href  string `json:"href"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// contexts returns each link context keyed by its anchor, with its relations.
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

// targets returns every link target the document names, in a stable order.
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

// fetchCatalog drives the catalog route on the given router and parses the
// response.
func fetchCatalog(t *testing.T, router http.Handler, requestPath string) catalogDocument {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, requestPath, nil)
	req.Host = "cetacean.example.com"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	// RFC 9727 §4.2: "The Publisher MUST publish the API catalog document in
	// the Linkset format application/linkset+json."
	if got := rec.Header().Get("Content-Type"); got != linkset.MediaType {
		t.Errorf("Content-Type = %q, want %q", got, linkset.MediaType)
	}

	var doc catalogDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("parse catalog: %v\nbody: %s", err, rec.Body.String())
	}

	return doc
}

// TestAPICatalogTargetsAnswerAsAdvertised is what makes the catalog
// self-checking: every target it publishes is driven against the same router
// that published it, and must answer with the media type the catalog claimed.
//
// A catalog is a promise that what it lists is there. Nothing else in the tree
// would notice an endpoint being renamed out from under this document, because
// it is the one place stating a path no route table mentions.
//
// The assertion is the media type rather than "not 404", which was the first
// version of this test and detected nothing: the SPA fallback answers every
// unrouted path with 200 and an HTML body, so renaming /api/context.jsonld out
// from under the catalog still passed. A target's own type attribute is the
// claim worth holding it to, and it catches the fallback for free — HTML is
// not what any of these advertise.
func TestAPICatalogTargetsAnswerAsAdvertised(t *testing.T) {
	router := newSeededTestRouter(t)

	doc := fetchCatalog(t, router, apiCatalogPath)

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

			// A media type may carry parameters the catalog does not state.
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

	// Without a floor, a catalog that stopped declaring types would walk clean
	// while asserting nothing.
	if typed < 4 {
		t.Errorf("only %d targets declared a media type; the walk checks little", typed)
	}
}

// TestAPICatalogAnswersItsOwnMediaType drives the request an RFC 9727 client
// makes. Linkset is the only format the catalog may be published in, so that
// is what such a client asks for by name — and until application/linkset+json
// was a media type negotiation recognised, the catalog answered its own
// audience with 406.
func TestAPICatalogAnswersItsOwnMediaType(t *testing.T) {
	router := newSeededTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, apiCatalogPath, nil)
	req.Header.Set("Accept", linkset.MediaType)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d asking for %s, want 200; body: %s",
			rec.Code, linkset.MediaType, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != linkset.MediaType {
		t.Errorf("Content-Type = %q, want %q", got, linkset.MediaType)
	}
}

// TestAPICatalogCarriesItemLinks pins the one relation RFC 9727 requires.
// §3.1: "the 'item' link relation identifies a target resource that represents
// an API that is a member of the API catalog." A catalog whose entries are all
// service-desc and describedby is a sitemap of one API's endpoints, which is
// not what this well-known URI means.
func TestAPICatalogCarriesItemLinks(t *testing.T) {
	router := newSeededTestRouter(t)

	doc := fetchCatalog(t, router, apiCatalogPath)
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
	}
}

// TestAPICatalogOmitsUnmountedAPIs holds the catalog to what the process
// actually serves. MCP is off by default, and its OAuth authorization server
// is wired only when an auth mode other than "none" is configured — so a
// catalog built from a single "is MCP on" flag would advertise a metadata
// document that does not exist in the auth-mode-none deployment.
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

// TestAPICatalogIsAbsoluteUnderABasePath covers the trap that bit the
// frontend's index.html: a document of absolute URIs served under
// CETACEAN_BASE_PATH must carry the prefix, or every URI in it addresses a
// path the deployment does not serve.
func TestAPICatalogIsAbsoluteUnderABasePath(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withBasePath("/cetacean")},
		withCache(cache.New(nil)),
	)

	doc := fetchCatalog(t, router, "/cetacean"+apiCatalogPath)

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

// TestAPICatalogETagIsStable fails if a context's relations are serialized in
// map order. Go randomizes that, so the body — and the ETag over it — would
// differ on every request and no conditional GET would ever match.
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
