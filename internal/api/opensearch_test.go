package api

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// parsedOpenSearch is the document read back off the wire, declared separately
// from the type that wrote it.
type parsedOpenSearch struct {
	XMLName       xml.Name `xml:"OpenSearchDescription"`
	ShortName     string   `xml:"ShortName"`
	Description   string   `xml:"Description"`
	InputEncoding string   `xml:"InputEncoding"`
	Image         struct {
		Width  int    `xml:"width,attr"`
		Height int    `xml:"height,attr"`
		Type   string `xml:"type,attr"`
		URL    string `xml:",chardata"`
	} `xml:"Image"`
	URLs []struct {
		Type     string `xml:"type,attr"`
		Template string `xml:"template,attr"`
	} `xml:"Url"`
}

func fetchOpenSearch(t *testing.T, router http.Handler, requestPath string) parsedOpenSearch {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, requestPath, nil)
	req.Host = "cetacean.example.com"
	req.Header.Set("Accept", openSearchMediaType)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Type"); got != openSearchMediaType {
		t.Errorf("Content-Type = %q, want %q", got, openSearchMediaType)
	}

	var doc parsedOpenSearch
	if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("document is not well-formed XML: %v\nbody: %s", err, rec.Body.String())
	}

	if doc.XMLName.Space != openSearchNamespace {
		t.Errorf("namespace = %q, want %q", doc.XMLName.Space, openSearchNamespace)
	}

	return doc
}

// TestOpenSearchDescriptionIsValid holds the document to the OpenSearch 1.1
// constraints. A browser that rejects one says nothing about why.
func TestOpenSearchDescriptionIsValid(t *testing.T) {
	router := newSeededTestRouter(t)

	doc := fetchOpenSearch(t, router, openSearchPath)

	// §4.2: ShortName "must contain 16 or fewer characters of plain text".
	if doc.ShortName == "" {
		t.Error("ShortName is required")
	}

	if len(doc.ShortName) > 16 {
		t.Errorf("ShortName is %d characters, the specification allows 16", len(doc.ShortName))
	}

	// §4.2: Description "must contain 1024 or fewer characters of plain text".
	if doc.Description == "" {
		t.Error("Description is required")
	}

	if len(doc.Description) > 1024 {
		t.Errorf(
			"Description is %d characters, the specification allows 1024",
			len(doc.Description),
		)
	}

	// §4.2: the encoding a client should use for {searchTerms}.
	if doc.InputEncoding != "UTF-8" {
		t.Errorf("InputEncoding = %q, want UTF-8", doc.InputEncoding)
	}

	// A browser renders Image at its declared size.
	if doc.Image.Width != 32 || doc.Image.Height != 32 {
		t.Errorf("Image is %dx%d, want the 32x32 favicon it points at",
			doc.Image.Width, doc.Image.Height)
	}

	if doc.Image.Type != "image/png" {
		t.Errorf("Image type = %q, want image/png", doc.Image.Type)
	}

	if len(doc.URLs) == 0 {
		t.Fatal("a description document with no Url element describes no search")
	}

	var html bool

	for _, u := range doc.URLs {
		if !strings.Contains(u.Template, "{searchTerms}") {
			t.Errorf("the %s template carries no {searchTerms}: %q", u.Type, u.Template)
		}

		if !strings.HasPrefix(u.Template, "http://") && !strings.HasPrefix(u.Template, "https://") {
			t.Errorf(
				"the %s template is not absolute (%q) — a template is used with no "+
					"document to resolve against",
				u.Type, u.Template,
			)
		}

		if u.Type == "text/html" {
			html = true
		}
	}

	if !html {
		t.Error("no text/html Url; a browser has nothing to open")
	}
}

// TestOpenSearchTemplatesResolve drives each template against the router that
// published it. A document whose templates 404 is worse than none: the browser
// offers the search and it fails.
func TestOpenSearchTemplatesResolve(t *testing.T) {
	router := newSeededTestRouter(t)

	doc := fetchOpenSearch(t, router, openSearchPath)

	for _, u := range doc.URLs {
		t.Run(u.Type, func(t *testing.T) {
			target := strings.Replace(u.Template, "{searchTerms}", "web", 1)

			path := strings.TrimPrefix(target, "http://cetacean.example.com")
			if path == target {
				t.Fatalf("template does not address this host: %q", target)
			}

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", u.Type)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body: %s",
					path, rec.Code, strings.TrimSpace(rec.Body.String()))
			}

			got, _, _ := strings.Cut(rec.Header().Get("Content-Type"), ";")
			if strings.TrimSpace(got) != u.Type {
				t.Errorf("the template declares %q but the endpoint answered %q", u.Type, got)
			}
		})
	}
}

// TestOpenSearchIsAbsoluteUnderABasePath: the templates are built by the
// server, so they must carry the base path themselves.
func TestOpenSearchIsAbsoluteUnderABasePath(t *testing.T) {
	router := newBasePathTestRouter(t, "/cetacean")

	doc := fetchOpenSearch(t, router, "/cetacean"+openSearchPath)

	const want = "http://cetacean.example.com/cetacean/"

	if !strings.HasPrefix(doc.Image.URL, want) {
		t.Errorf("Image %q does not carry the base path", doc.Image.URL)
	}

	for _, u := range doc.URLs {
		if !strings.HasPrefix(u.Template, want) {
			t.Errorf("the %s template %q does not carry the base path", u.Type, u.Template)
		}
	}
}
