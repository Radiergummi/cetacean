package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	playgroundBaseHref = regexp.MustCompile(`<base href="([^"]*)"`)
	playgroundScript   = regexp.MustCompile(`<script src="([^"]*)"`)
	playgroundSpecURL  = regexp.MustCompile(`data-url="([^"]*)"`)
)

// The playground's references and the document's servers must resolve under
// the base path, or the explorer loads nothing and a client calls the root.
func TestAPIDocsFollowTheBasePath(t *testing.T) {
	for _, base := range []string{"", "/cetacean"} {
		t.Run("base="+base, func(t *testing.T) {
			router := newTestRouterWithConfig(t, []routerOption{
				withBasePath(base),
				withAPIDocs(
					[]byte("openapi: '3.1.0'\n# kept\nservers:\n  - url: /\npaths: {}\n"),
					[]byte("/* scalar */"),
				),
			})

			get := func(path, accept string) *httptest.ResponseRecorder {
				req := httptest.NewRequest("GET", path, nil)
				req.Header.Set("Accept", accept)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s: status = %d, want 200", path, rec.Code)
				}
				return rec
			}

			page := base + "/api"
			body := get(page, "text/html").Body.String()

			ref, _ := url.Parse(page)
			if m := playgroundBaseHref.FindStringSubmatch(body); m != nil {
				ref = ref.ResolveReference(mustParseURL(t, m[1]))
			}

			for _, pattern := range []*regexp.Regexp{playgroundScript, playgroundSpecURL} {
				m := pattern.FindStringSubmatch(body)
				if m == nil {
					t.Fatalf("playground has no match for %s:\n%s", pattern, body)
				}
				resolved := ref.ResolveReference(mustParseURL(t, m[1])).Path
				get(resolved, "*/*")
			}
			if got := ref.ResolveReference(mustParseURL(t, "api")).Path; got != page {
				t.Errorf("spec URL resolves to %q, want %q", got, page)
			}

			want := base + "/"

			var doc struct {
				Servers []struct {
					URL string `json:"url"`
				} `json:"servers"`
			}
			if err := json.Unmarshal(get(page, "application/json").Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Servers) != 1 || doc.Servers[0].URL != want {
				t.Errorf("JSON servers = %+v, want one at %q", doc.Servers, want)
			}

			source := get(base+openAPIYAMLPath, "*/*").Body.Bytes()
			if err := yaml.Unmarshal(source, &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Servers) != 1 || doc.Servers[0].URL != want {
				t.Errorf("YAML servers = %+v, want one at %q", doc.Servers, want)
			}
			if !strings.Contains(string(source), "# kept") {
				t.Errorf("YAML lost the source's comments:\n%s", source)
			}
		})
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	return u
}
