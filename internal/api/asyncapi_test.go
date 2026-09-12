package api

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/radiergummi/cetacean/internal/cache"
	promapi "github.com/radiergummi/cetacean/internal/prometheus"
)

// asyncAPISchema is the official AsyncAPI 3.0.0 meta-schema. It is embedded
// from a _test.go file so it is never linked into the binary.
//
//go:embed testdata/asyncapi-3.0.0.json
var asyncAPISchema []byte

const asyncAPISpecPath = "../../api/asyncapi.yaml"

// loadAsyncAPIDoc parses api/asyncapi.yaml into the JSON-shaped value every
// test here reads. The served document is the same value, so a test that
// passes against the file passes against the response.
func loadAsyncAPIDoc(t *testing.T) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(asyncAPISpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", asyncAPISpecPath, err)
	}

	return loadYAMLDocument(t, asyncAPISpecPath, raw)
}

// TestAsyncAPIDocumentIsValid validates api/asyncapi.yaml against the official
// AsyncAPI 3.0.0 meta-schema.
//
// The meta-schema's $refs are absolute http://asyncapi.com/ URLs, but every
// one of them resolves inside the file: each entry under definitions carries
// the matching $id. No loader is installed, so a ref that did not resolve
// locally would fail the compile rather than reach the network.
func TestAsyncAPIDocumentIsValid(t *testing.T) {
	const metaURL = "http://asyncapi.com/definitions/3.0.0/asyncapi.json"

	meta, err := jsonschema.UnmarshalJSON(bytes.NewReader(asyncAPISchema))
	if err != nil {
		t.Fatalf("the vendored meta-schema is not JSON: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(metaURL, meta); err != nil {
		t.Fatalf("add meta-schema: %v", err)
	}

	schema, err := compiler.Compile(metaURL)
	if err != nil {
		t.Fatalf("compile meta-schema: %v", err)
	}

	encoded, err := json.Marshal(loadAsyncAPIDoc(t))
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}

	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("re-parse document: %v", err)
	}

	if err := schema.Validate(instance); err != nil {
		t.Fatalf("api/asyncapi.yaml is not valid AsyncAPI 3.0.0:\n%v", err)
	}
}

// TestAsyncAPISchemasMatchOpenAPI pins the two schemas asyncapi.yaml copies
// from openapi.yaml. They are copied rather than $ref'd because the correct
// reference differs between the file in the repository and the document as
// served, and one reference with two correct spellings is the trap
// /.well-known/jwks.json fell into from the other direction.
func TestAsyncAPISchemasMatchOpenAPI(t *testing.T) {
	specYAML, _, _ := loadTestSpec(t)
	openAPI := loadYAMLDocument(t, "openapi.yaml", specYAML)
	asyncAPI := loadAsyncAPIDoc(t)

	for _, name := range []string{"SSEEvent", "LogLine"} {
		t.Run(name, func(t *testing.T) {
			want := schemaNamed(t, openAPI, name)
			got := schemaNamed(t, asyncAPI, name)

			if !reflect.DeepEqual(got, want) {
				t.Errorf(
					"asyncapi.yaml's %s has drifted from openapi.yaml's\n got: %#v\nwant: %#v",
					name, got, want,
				)
			}
		})
	}
}

// loadYAMLDocument parses either spec into the same JSON shape. name only
// names the source in a failure.
func loadYAMLDocument(t *testing.T, name string, raw []byte) map[string]any {
	t.Helper()

	doc, err := yamlDocument(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	return doc
}

// schemaNamed digs components.schemas.<name> out of either document.
func schemaNamed(t *testing.T, doc map[string]any, name string) any {
	t.Helper()

	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatal("document has no components")
	}

	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("document has no components.schemas")
	}

	schema, ok := schemas[name]
	if !ok {
		t.Fatalf("document has no components.schemas.%s", name)
	}

	return schema
}

// TestAsyncAPIIsServed drives the route the discovery links point at.
func TestAsyncAPIIsServed(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withAsyncAPISpec(t)},
		withCache(cache.New(nil)),
	)

	for _, path := range []string{asyncAPIPath, asyncAPIPath + ".json"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "cetacean.example.com"
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}

			if got := rec.Header().Get("Content-Type"); got != asyncAPIMediaType {
				t.Errorf("Content-Type = %q, want %q", got, asyncAPIMediaType)
			}

			if got := rec.Header().Get("Cache-Control"); got != "public, max-age=3600" {
				t.Errorf("Cache-Control = %q", got)
			}

			if rec.Header().Get("ETag") == "" {
				t.Error("no ETag")
			}

			var doc map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}

			if doc["asyncapi"] != "3.0.0" {
				t.Errorf("asyncapi = %v, want 3.0.0", doc["asyncapi"])
			}
		})
	}
}

// asyncAPIServer is the server block of the served document.
type asyncAPIServer struct {
	Host     string `json:"host"`
	Protocol string `json:"protocol"`
	Pathname string `json:"pathname"`
}

// asyncAPIServerOf fetches the document and returns the server it names.
func asyncAPIServerOf(t *testing.T, router http.Handler, path, host string) asyncAPIServer {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	req.Header.Set("X-Forwarded-Host", "evil.example.com")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var doc struct {
		Servers map[string]asyncAPIServer `json:"servers"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	self, ok := doc.Servers["self"]
	if !ok {
		t.Fatal("the served document names no server")
	}

	return self
}

// TestAsyncAPINamesTheDeployment: AsyncAPI 3.0 requires host on a server
// object, so the served document has to name the deployment — from the
// validated origin, never from a raw forwarding header — and to carry the
// base path every address is reached through.
func TestAsyncAPINamesTheDeployment(t *testing.T) {
	t.Run("the request's own origin, not a forwarding header", func(t *testing.T) {
		router := newTestRouterWithConfig(
			t,
			[]routerOption{withAsyncAPISpec(t)},
			withCache(cache.New(nil)),
		)

		self := asyncAPIServerOf(t, router, asyncAPIPath, "cetacean.example.com")

		if self.Host != "cetacean.example.com" {
			t.Errorf("host = %q, want the request's own origin", self.Host)
		}

		if self.Protocol != "http" {
			t.Errorf("protocol = %q, want http for a plaintext request", self.Protocol)
		}

		if self.Pathname != "" {
			t.Errorf("pathname = %q, want none without a base path", self.Pathname)
		}
	})

	t.Run("the base path as a pathname", func(t *testing.T) {
		router := newTestRouterWithConfig(
			t,
			[]routerOption{withBasePath("/cetacean"), withAsyncAPISpec(t)},
			withCache(cache.New(nil)),
		)

		self := asyncAPIServerOf(
			t, router, "/cetacean"+asyncAPIPath, "cetacean.example.com",
		)

		if self.Pathname != "/cetacean" {
			t.Errorf("pathname = %q, want /cetacean", self.Pathname)
		}
	})

	// One rendering is retained per origin, so a second origin has to displace
	// the first rather than be served it.
	t.Run("a second origin is not served the first one's document", func(t *testing.T) {
		router := newTestRouterWithConfig(
			t,
			[]routerOption{withAsyncAPISpec(t)},
			withCache(cache.New(nil)),
		)

		for _, host := range []string{"one.example.com", "two.example.com", "one.example.com"} {
			if got := asyncAPIServerOf(t, router, asyncAPIPath, host).Host; got != host {
				t.Errorf("host = %q, want %q — a retained rendering outlived its origin", got, host)
			}
		}
	})
}

// TestAsyncAPIIsDiscoverable pins the typed service-desc Link every non-meta
// response carries. The catalog's target is walked by
// TestAPICatalogTargetsAnswerAsAdvertised.
func TestAsyncAPIIsDiscoverable(t *testing.T) {
	// The stub document testRouterConfig defaults to is enough here: the Link
	// header says nothing about the document's content.
	router := newTestRouterWithCache(t, cache.New(nil))

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	want := `<` + asyncAPIPath + `>; rel="service-desc"; type="` + asyncAPIMediaType + `"`

	if !slices.Contains(rec.Header().Values("Link"), want) {
		t.Errorf("no Link header %q; got %q", want, rec.Header().Values("Link"))
	}
}

// asyncAPIAddresses returns every channel address, keyed by channel name.
func asyncAPIAddresses(t *testing.T) map[string]string {
	t.Helper()

	raw, ok := loadAsyncAPIDoc(t)["channels"].(map[string]any)
	if !ok {
		t.Fatal("the document declares no channels")
	}

	addresses := make(map[string]string, len(raw))

	for name, value := range raw {
		channel, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("channel %q is not an object", name)
		}

		address, ok := channel["address"].(string)
		if !ok {
			t.Fatalf("channel %q has no address", name)
		}

		addresses[name] = address
	}

	return addresses
}

// TestAsyncAPIChannelsAnswerAsStreams drives every channel address the
// document declares and requires it to open a stream.
//
// The assertion is the content type, not the status: the SPA fallback answers
// 200 for any unrouted path, so a bogus address would pass a status check. It
// answers text/html, which this fails on.
func TestAsyncAPIChannelsAnswerAsStreams(t *testing.T) {
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	t.Cleanup(prometheus.Close)

	router, _, _ := streamTestRouter(t, 0, withPromClient(promapi.NewClient(prometheus.URL)))

	for name, address := range asyncAPIAddresses(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, ok := resolvePath(address)
			if !ok {
				t.Fatalf(
					"no fixture resolves %q — add one to pathFixtures so the walk "+
						"covers this channel instead of skipping it",
					address,
				)
			}

			req := httptest.NewRequest(http.MethodGet, path+asyncAPIProbeQuery[name], nil)
			req.Header.Set("Accept", "text/event-stream")

			ctx, cancel := context.WithTimeout(req.Context(), 200*time.Millisecond)
			defer cancel()

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req.WithContext(ctx))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}

			if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
				t.Fatalf(
					"Content-Type = %q, want text/event-stream — this address does "+
						"not open a stream, so the document is describing one that "+
						"does not exist",
					got,
				)
			}
		})
	}
}

// asyncAPIProbeQuery is the query string each channel needs to open. Only
// /metrics needs one; every other channel streams on its address alone. The
// walk above fails on a channel missing from this map rather than skipping
// it, so this cannot fall behind the document.
var asyncAPIProbeQuery = map[string]string{
	"nodes": "", "nodeDetail": "",
	"services": "", "serviceDetail": "",
	"tasks": "", "taskDetail": "",
	"stacks": "", "stackDetail": "",
	"configs": "", "configDetail": "",
	"secrets": "", "secretDetail": "",
	"networks": "", "networkDetail": "",
	"volumes": "", "volumeDetail": "",
	"events":      "",
	"serviceLogs": "",
	"taskLogs":    "",
	"metrics":     "?query=up&step=15",
}

// withAsyncAPISpec wires the real document into the test router, so the tests
// drive the document that ships rather than a stub.
func withAsyncAPISpec(t *testing.T) routerOption {
	t.Helper()

	raw, err := os.ReadFile(asyncAPISpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", asyncAPISpecPath, err)
	}

	return func(cfg *RouterConfig) { cfg.AsyncAPISpec = raw }
}

// asyncAPIRequest fetches the document under one Accept header.
func asyncAPIRequest(
	t *testing.T,
	router http.Handler,
	path, accept string,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "cetacean.example.com"

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("%s (Accept: %q) = %d; body: %s", path, accept, rec.Code, rec.Body.String())
	}

	return rec
}

// TestAsyncAPIYAMLIsTheSameDocument holds the two representations together.
// The server block is spliced into a node tree for YAML and into a map for
// JSON — two injection sites that would otherwise drift silently.
func TestAsyncAPIYAMLIsTheSameDocument(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withAsyncAPISpec(t)},
		withCache(cache.New(nil)),
	)

	asJSON := asyncAPIRequest(t, router, asyncAPIPath, "application/json")
	asYAML := asyncAPIRequest(t, router, asyncAPIYAMLPath, "")

	// Both sides start from the same file, so normalising the YAML through the
	// same conversion the JSON path uses has to land on identical bytes.
	normalized, err := json.Marshal(loadYAMLDocument(t, "the served YAML", asYAML.Body.Bytes()))
	if err != nil {
		t.Fatalf("marshal the served YAML: %v", err)
	}

	if !bytes.Equal(normalized, asJSON.Body.Bytes()) {
		t.Errorf(
			"the YAML and JSON representations are different documents\n yaml: %s\n json: %s",
			truncate(normalized), truncate(asJSON.Body.Bytes()),
		)
	}
}

// TestAsyncAPIYAMLKeepsTheAuthoredDocument: the point of serving YAML is that
// a person reads it, so the comments and the key order in api/asyncapi.yaml
// have to survive. Re-encoding the map the JSON is built from loses both.
func TestAsyncAPIYAMLKeepsTheAuthoredDocument(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withAsyncAPISpec(t)},
		withCache(cache.New(nil)),
	)

	body := asyncAPIRequest(t, router, asyncAPIYAMLPath, "").Body.String()

	if !strings.HasPrefix(body, "asyncapi:") {
		t.Errorf(
			"the document does not open on asyncapi:, so the key order was rebuilt:\n%.120s",
			body,
		)
	}

	// A comment from the source file. A bare "#" would pass on any $ref, so
	// this names one the file actually carries.
	const comment = "# The /metrics stream forwards Prometheus' own response bodies unchanged."

	if !strings.Contains(body, comment) {
		t.Error("the served YAML dropped the source file's comments")
	}

	// The authored file indents by two; yaml.Marshal's default is four.
	if !strings.Contains(body, "\ninfo:\n  title:") {
		t.Error("the served YAML is not indented like the file it comes from")
	}

	// The placeholder in the file must not survive: the served document names
	// the origin the request arrived on.
	if strings.Contains(body, "localhost:9000") {
		t.Error("the served YAML still names the file's placeholder host")
	}

	if !strings.Contains(body, "cetacean.example.com") {
		t.Error("the served YAML does not name the request's origin")
	}
}

// TestSpecDocumentsNegotiateYAML drives every spelling a client might send.
func TestSpecDocumentsNegotiateYAML(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withAsyncAPISpec(t), withAPIDocs([]byte("openapi: '3.1.0'\n"), nil)},
		withCache(cache.New(nil)),
	)

	yamlTypes := []string{
		"application/yaml",
		"text/yaml",
		"application/x-yaml",
		"text/x-yaml",
	}

	t.Run("openapi", func(t *testing.T) {
		for _, accept := range append(yamlTypes,
			"application/vnd.oai.openapi", "application/openapi+yaml",
		) {
			t.Run(accept, func(t *testing.T) {
				rec := asyncAPIRequest(t, router, "/api", accept)

				if got := rec.Header().Get("Content-Type"); got != openAPIYAMLMediaType {
					t.Errorf("Content-Type = %q, want %q", got, openAPIYAMLMediaType)
				}

				if !strings.HasPrefix(rec.Body.String(), "openapi:") {
					t.Errorf("body is not the YAML source: %.60s", rec.Body.String())
				}
			})
		}
	})

	t.Run("asyncapi", func(t *testing.T) {
		for _, accept := range append(yamlTypes,
			"application/vnd.aai.asyncapi+yaml", "application/asyncapi+yaml",
		) {
			t.Run(accept, func(t *testing.T) {
				rec := asyncAPIRequest(t, router, asyncAPIPath, accept)

				if got := rec.Header().Get("Content-Type"); got != asyncAPIYAMLMediaType {
					t.Errorf("Content-Type = %q, want %q", got, asyncAPIYAMLMediaType)
				}

				if !strings.HasPrefix(rec.Body.String(), "asyncapi:") {
					t.Errorf("body is not YAML: %.60s", rec.Body.String())
				}
			})
		}
	})

	// The generic spellings ask for a format, not a document, so JSON must
	// still be what an ordinary client gets.
	t.Run("json is still the default", func(t *testing.T) {
		for _, accept := range []string{"", "*/*", "application/json", "application/*"} {
			rec := asyncAPIRequest(t, router, asyncAPIPath, accept)

			if got := rec.Header().Get("Content-Type"); got != asyncAPIMediaType {
				t.Errorf("Accept %q gave Content-Type %q, want %q", accept, got, asyncAPIMediaType)
			}
		}
	})
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}

	return string(b)
}
