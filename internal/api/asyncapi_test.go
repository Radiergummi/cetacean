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
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/radiergummi/cetacean/internal/api/sse"
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

	return loadYAMLDocument(t, asyncAPISpecPath)
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
	openAPI := loadYAMLDocument(t, "../../api/openapi.yaml")
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

// loadYAMLDocument parses any of the two specs into the same JSON shape.
func loadYAMLDocument(t *testing.T, path string) map[string]any {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var parsed any
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("%s is not valid YAML: %v", path, err)
	}

	doc, ok := convertYAMLToJSON(parsed).(map[string]any)
	if !ok {
		t.Fatalf("%s does not parse to an object", path)
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

// TestAsyncAPINamesTheRequestOrigin: AsyncAPI 3.0 requires host on a server
// object, so the served document has to name the deployment. The value comes
// from the validated origin, never from a raw forwarding header.
func TestAsyncAPINamesTheRequestOrigin(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withAsyncAPISpec(t)},
		withCache(cache.New(nil)),
	)

	req := httptest.NewRequest(http.MethodGet, asyncAPIPath, nil)
	req.Host = "cetacean.example.com"
	req.Header.Set("X-Forwarded-Host", "evil.example.com")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	var doc struct {
		Servers map[string]struct {
			Host     string `json:"host"`
			Protocol string `json:"protocol"`
			Pathname string `json:"pathname"`
		} `json:"servers"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	self, ok := doc.Servers["self"]
	if !ok {
		t.Fatal("the served document names no server")
	}

	if self.Host != "cetacean.example.com" {
		t.Errorf("host = %q, want the request's own origin", self.Host)
	}

	if self.Protocol != "http" {
		t.Errorf("protocol = %q, want http for a plaintext request", self.Protocol)
	}
}

// TestAsyncAPICarriesTheBasePath: under CETACEAN_BASE_PATH every address the
// document names is reached through the prefix, so the server object has to
// carry it as a pathname.
func TestAsyncAPICarriesTheBasePath(t *testing.T) {
	router := newTestRouterWithConfig(
		t,
		[]routerOption{withBasePath("/cetacean"), withAsyncAPISpec(t)},
		withCache(cache.New(nil)),
	)

	req := httptest.NewRequest(http.MethodGet, "/cetacean"+asyncAPIPath, nil)
	req.Host = "cetacean.example.com"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var doc struct {
		Servers map[string]struct {
			Pathname string `json:"pathname"`
		} `json:"servers"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	if got := doc.Servers["self"].Pathname; got != "/cetacean" {
		t.Errorf("pathname = %q, want /cetacean", got)
	}
}

// TestAsyncAPIIsDiscoverable pins both places the spec names: a typed
// service-desc Link on every non-meta response, and a matching target in the
// API catalog.
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

// asyncAPIChannels returns the channels block, keyed as the document keys it.
func asyncAPIChannels(t *testing.T) map[string]map[string]any {
	t.Helper()

	raw, ok := loadAsyncAPIDoc(t)["channels"].(map[string]any)
	if !ok {
		t.Fatal("the document declares no channels")
	}

	channels := make(map[string]map[string]any, len(raw))

	for name, value := range raw {
		channel, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("channel %q is not an object", name)
		}

		channels[name] = channel
	}

	return channels
}

// asyncAPIAddresses returns every channel address, keyed by channel name.
func asyncAPIAddresses(t *testing.T) map[string]string {
	t.Helper()

	addresses := make(map[string]string)

	for name, channel := range asyncAPIChannels(t) {
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
	c := cache.New(nil)
	populateSpecFixtures(c)

	broadcaster := sse.NewBroadcaster(0, noopErrorWriter, c.History())
	t.Cleanup(broadcaster.Close)

	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	t.Cleanup(prometheus.Close)

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2026-01-01T00:00:00.000000000Z hello\n"))

	router := newTestRouterWithCache(
		t, c,
		withBroadcaster(broadcaster),
		withDockerClient(&mockLogStreamer{data: frames.Bytes()}),
		withPromClient(promapi.NewClient(prometheus.URL)),
	)

	for name, address := range asyncAPIAddresses(t) {
		t.Run(name, func(t *testing.T) {
			path, ok := resolvePath(address)
			if !ok {
				t.Fatalf(
					"no fixture resolves %q — add one to pathFixtures so the walk "+
						"covers this channel instead of skipping it",
					address,
				)
			}

			query, ok := asyncAPIProbeQuery[name]
			if !ok {
				t.Fatalf(
					"channel %q has no entry in asyncAPIProbeQuery — add one (an "+
						"empty string is fine) so a new channel cannot join the "+
						"document without being probed",
					name,
				)
			}

			req := httptest.NewRequest(http.MethodGet, path+query, nil)
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
