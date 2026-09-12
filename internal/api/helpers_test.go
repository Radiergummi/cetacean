package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/klauspost/compress/zstd"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/prometheus"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

type testHandlersConfig struct {
	cache           *cache.Cache
	broadcaster     *sse.Broadcaster
	dockerClient    DockerLogStreamer
	systemClient    DockerSystemClient
	writeClient     DockerWriteClient
	pluginClient    DockerPluginClient
	ready           <-chan struct{}
	promClient      *prometheus.Client
	operationsLevel config.OperationsLevel
	aclEval         *acl.Evaluator
	recEngine       *recommendations.Engine
}

type testHandlersOption func(*testHandlersConfig)

func withCache(c *cache.Cache) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.cache = c }
}

func withWriteClient(wc DockerWriteClient) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.writeClient = wc }
}

func withOpsLevel(level config.OperationsLevel) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.operationsLevel = level }
}

func withPromClient(pc *prometheus.Client) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.promClient = pc }
}

func withReady(ch <-chan struct{}) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.ready = ch }
}

func withPluginClient(pc DockerPluginClient) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.pluginClient = pc }
}

func withSystemClient(sc DockerSystemClient) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.systemClient = sc }
}

func withDockerClient(dc DockerLogStreamer) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.dockerClient = dc }
}

func withACL(e *acl.Evaluator) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.aclEval = e }
}

func withBroadcaster(b *sse.Broadcaster) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.broadcaster = b }
}

func withRecEngine(e *recommendations.Engine) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.recEngine = e }
}

// newTestHandlers creates a Handlers instance with sensible test defaults.
// All dependencies default to nil except cache (empty), ready (closed), and
// operationsLevel (OpsImpactful). Use option functions to override.
func newTestHandlers(t testing.TB, opts ...testHandlersOption) *Handlers {
	t.Helper()

	cfg := testHandlersConfig{
		cache:           cache.New(nil),
		ready:           closedReady(),
		operationsLevel: config.OpsImpactful,
	}

	for _, opt := range opts {
		opt(&cfg)
	}

	return NewHandlers(
		cfg.cache,
		cfg.broadcaster,
		cfg.dockerClient,
		cfg.systemClient,
		cfg.writeClient,
		cfg.pluginClient,
		cfg.ready,
		cfg.promClient,
		cfg.operationsLevel,
		cfg.recEngine,
		cfg.aclEval,
	)
}

// decodeZstd decompresses a zstd response body, failing the test if it is
// not a valid frame. Three tests decode a compressed response by hand;
// this is that block.
func decodeZstd(t testing.TB, body []byte) []byte {
	t.Helper()

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer decoder.Close()

	plain, err := decoder.DecodeAll(body, nil)
	if err != nil {
		t.Fatalf("response body is not a valid zstd frame: %v", err)
	}

	return plain
}

// decodeGzip decompresses a gzip response body, failing the test if it is not
// a valid stream.
func decodeGzip(t testing.TB, body []byte) []byte {
	t.Helper()

	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("response body is not a valid gzip stream: %v", err)
	}
	defer reader.Close()

	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("gzip stream did not decode: %v", err)
	}

	return plain
}

// decodeCoding decompresses a body under the coding its Content-Encoding named,
// so a test can check the bytes against the label rather than trusting it. An
// unrecognised token fails rather than passing the body through: treating it as
// identity is exactly how a mislabelled body would look correct.
func decodeCoding(t testing.TB, encoding string, body []byte) []byte {
	t.Helper()

	switch encoding {
	case "gzip":
		return decodeGzip(t, body)
	case "zstd":
		return decodeZstd(t, body)
	case "", "identity":
		return body
	default:
		t.Fatalf("unexpected Content-Encoding %q", encoding)

		return nil
	}
}

// routerOption adjusts the RouterConfig newTestRouterWithCache assembles, for
// the router-level settings no testHandlersOption can reach.
type routerOption func(*RouterConfig)

// withCORS configures the router's CORS allowlist, which also decides which
// origins cross-origin protection trusts.
func withCORS(origins ...string) routerOption {
	return func(cfg *RouterConfig) {
		cfg.CORS = &CORSConfig{AllowedOrigins: origins}
	}
}

// withBasePath serves the router under a path prefix, as CETACEAN_BASE_PATH does.
func withBasePath(basePath string) routerOption {
	return func(cfg *RouterConfig) {
		cfg.BasePath = basePath
	}
}

// newBasePathTestRouter serves a router under a path prefix.
func newBasePathTestRouter(t *testing.T, basePath string) http.Handler {
	t.Helper()

	return newTestRouterWithConfig(
		t,
		[]routerOption{withBasePath(basePath)},
		withCache(cache.New(nil)),
	)
}

// withSPAFiles serves the frontend off fsys, for the tests that care what the
// embedded filesystem holds beside index.html.
func withSPAFiles(fsys fs.FS) routerOption {
	return func(cfg *RouterConfig) {
		cfg.SPA = NewSPAHandler(fsys, "")
	}
}

// withAPIDocs configures the two documents the /api endpoints serve, which
// default to a stub spec and no bundle.
func withAPIDocs(spec, scalarJS []byte) routerOption {
	return func(cfg *RouterConfig) {
		cfg.OpenAPISpec = spec
		cfg.ScalarJS = scalarJS
	}
}

// newTestRouterWithCache builds a fully wired router around a caller-seeded
// cache, for tests that exercise real routes rather than call a handler
// directly. Further testHandlersOption values are applied on top of the cache.
func newTestRouterWithCache(
	t testing.TB,
	c *cache.Cache,
	opts ...testHandlersOption,
) http.Handler {
	t.Helper()

	return newTestRouterWithConfig(t, nil, append([]testHandlersOption{withCache(c)}, opts...)...)
}

// newTestRouterWithConfig is newTestRouterWithCache plus the router-level
// settings, so the RouterConfig every assembled-router test drives is written
// once.
func newTestRouterWithConfig(
	t testing.TB,
	routerOpts []routerOption,
	opts ...testHandlersOption,
) http.Handler {
	t.Helper()

	return NewRouter(testRouterConfig(t, routerOpts, opts...))
}

// testRouterConfig is the RouterConfig the test routers are built from, so a
// test that enumerates the routes enumerates the ones it drives.
func testRouterConfig(
	t testing.TB,
	routerOpts []routerOption,
	opts ...testHandlersOption,
) RouterConfig {
	t.Helper()

	h := newTestHandlers(t, opts...)
	b := sse.NewBroadcaster(0, noopErrorWriter, nil)
	t.Cleanup(b.Close)
	fsys := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	spa := NewSPAHandler(fs.FS(fsys), "")

	cfg := RouterConfig{
		Handlers:          h,
		Broadcaster:       b,
		SPA:               spa,
		OpenAPISpec:       []byte("openapi: '3.1.0'"),
		EnableSelfMetrics: true,
		AuthProvider:      &auth.NoneProvider{},
		Resyncer:          stubResyncer{},
	}

	for _, opt := range routerOpts {
		opt(&cfg)
	}

	return cfg
}

// routerPatterns returns every pattern NewRouter registers. Route
// registration does not vary with the operations level or the cache, so the
// list holds for any of the routers a test builds.
func routerPatterns(t testing.TB) []string {
	t.Helper()

	_, patterns := newRouter(testRouterConfig(t, nil, withCache(cache.New(nil))))

	return patterns
}

// stubResyncer stands in for the watcher, so POST /-/resync is registered and
// the tests that sweep the spec's operations can reach it.
type stubResyncer struct{}

func (stubResyncer) Resync(context.Context) error { return nil }
