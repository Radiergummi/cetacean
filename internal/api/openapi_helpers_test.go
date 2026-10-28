package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/errdefs"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/radiergummi/cetacean/internal/api/linkset"
	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	promapi "github.com/radiergummi/cetacean/internal/prometheus"
)

// Cached spec artifacts — parsing and validation cost dozens of ms so we only
// pay it once per `go test` process. The returned values are read-only after
// construction (tests call Paths.Map() and specRouter.FindRoute, both
// concurrency-safe).
var (
	specOnce    sync.Once
	specBytes   []byte
	specDoc     *openapi3.T
	specRouter  routers.Router
	specLoadErr error
)

// loadTestSpec returns the OpenAPI spec bytes, parsed document, and request
// router. The spec is loaded, validated, and routed once per test binary.
func loadTestSpec(t *testing.T) ([]byte, *openapi3.T, routers.Router) {
	t.Helper()

	specOnce.Do(func() {
		// openapi3filter decodes by exact media type, so these two would be
		// "unsupported content type" and have to be skipped like /topology,
		// leaving their schemas unchecked.
		openapi3filter.RegisterBodyDecoder(
			linkset.MediaType,
			openapi3filter.RegisteredBodyDecoder("application/json"),
		)

		openapi3filter.RegisterBodyDecoder(
			"application/feed+json",
			openapi3filter.RegisteredBodyDecoder("application/json"),
		)

		openapi3filter.RegisterBodyDecoder(
			openSearchMediaType,
			openapi3filter.RegisteredBodyDecoder("text/plain"),
		)

		const specPath = "../../api/openapi.yaml"

		bytes, err := os.ReadFile(specPath)
		if err != nil {
			specLoadErr = err
			return
		}

		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromData(bytes)
		if err != nil {
			specLoadErr = err
			return
		}

		if err := doc.Validate(loader.Context); err != nil {
			specLoadErr = err
			return
		}

		r, err := gorillamux.NewRouter(doc)
		if err != nil {
			specLoadErr = err
			return
		}

		specBytes, specDoc, specRouter = bytes, doc, r
	})

	if specLoadErr != nil {
		t.Fatalf("load spec: %v", specLoadErr)
	}

	return specBytes, specDoc, specRouter
}

// newTestRouter wires up an HTTP router backed by the given handlers and
// broadcaster with noop auth and a stub SPA. Shared by spec-conformance
// tests that need to exercise the full middleware chain.
func newTestRouter(
	t *testing.T,
	handlers *Handlers,
	broadcaster *sse.Broadcaster,
	specBytes []byte,
) http.Handler {
	t.Helper()

	noopSPA := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html></html>"))
	})

	return NewRouter(RouterConfig{
		Hosts:              auth.NewHostSet("", "example.com", "cetacean.example.com"),
		Handlers:           handlers,
		Broadcaster:        broadcaster,
		SPA:                noopSPA,
		OpenAPISpec:        specBytes,
		AsyncAPISpec:       []byte("asyncapi: '3.0.0'"),
		EnableSelfMetrics:  true,
		EnableVersionCheck: true,
		AuthProvider:       &auth.NoneProvider{},
		TokenRevoker:       stubRevoker{},
	})
}

// specFixtureIDs pairs each parameterised resource path in the spec with the
// identifier that addresses it. resolvePath substitutes from it and specTemplate
// inverts it, and both fixtures seed these ids — one vocabulary, so a walk
// cannot spend its probes on resources no fixture holds.
var specFixtureIDs = map[string]string{
	"/nodes/{id}":     "node1",
	"/services/{id}":  "svc1",
	"/tasks/{id}":     "task1",
	"/stacks/{name}":  seededStack,
	"/configs/{id}":   "cfg1",
	"/secrets/{id}":   "sec1",
	"/networks/{id}":  "net1",
	"/volumes/{name}": "vol1",
	"/plugins/{name}": "plug1",
}

// populateSpecFixtures seeds the cache with the specFixtureIDs resources, for
// the contract walks that want a lean fixture rather than newSeededTestRouter's
// fully populated one. The service carries a stack-namespace label so the
// stack is derivable from cache state.
func populateSpecFixtures(c *cache.Cache) {
	c.SetNode(swarm.Node{
		ID: "node1",
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
		},
		Description: swarm.NodeDescription{Hostname: "manager-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		ManagerStatus: &swarm.ManagerStatus{
			Leader:       true,
			Reachability: swarm.ReachabilityReachable,
		},
	})

	replicas := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "web",
				Labels: map[string]string{"com.docker.stack.namespace": seededStack},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.27"},
				// Set, so the sub-resource bodies carry every documented field.
				Resources: &swarm.ResourceRequirements{
					Limits:       &swarm.Limit{NanoCPUs: 2e9, MemoryBytes: 512 << 20, Pids: 100},
					Reservations: &swarm.Resources{NanoCPUs: 5e8, MemoryBytes: 256 << 20},
				},
				Placement: &swarm.Placement{
					Constraints: []string{"node.role == worker"},
					MaxReplicas: 2,
				},
			},
			UpdateConfig: &swarm.UpdateConfig{
				Parallelism:     1,
				Delay:           10 * time.Second,
				FailureAction:   swarm.UpdateFailureActionRollback,
				Monitor:         5 * time.Second,
				MaxFailureRatio: 0.1,
				Order:           swarm.UpdateOrderStartFirst,
			},
			RollbackConfig: &swarm.UpdateConfig{
				Parallelism:   1,
				FailureAction: swarm.UpdateFailureActionPause,
				Order:         swarm.UpdateOrderStopFirst,
			},
		},
	})

	c.SetTask(swarm.Task{
		ID:           "task1",
		ServiceID:    "svc1",
		NodeID:       "node1",
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
		DesiredState: swarm.TaskStateRunning,
	})

	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}},
	})

	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}},
	})

	c.SetNetwork(network.Summary{ID: "net1", Name: "net"})

	c.SetVolume(volume.Volume{Name: "vol1", Driver: "local"})
}

// newSpecWalkRouter is the router both GET walks drive: the specFixtureIDs cache
// plus stub Docker, Prometheus and secret-entry backends, so every endpoint
// answering from one of those reaches its handler. The map gives the concrete
// request for templates resolvePath cannot write down.
func newSpecWalkRouter(t *testing.T, specBytes []byte) (http.Handler, map[string]string) {
	t.Helper()

	c := cache.New(nil)
	populateSpecFixtures(c)

	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(r.URL.Path, "/query_range") {
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"matrix",`+
				`"result":[{"metric":{},"values":[[1,"1"],[2,"1"]]}]}}`)
			return
		}

		_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"vector",`+
			`"result":[{"metric":{},"value":[1,"1"]}]}}`)
	}))
	t.Cleanup(prom.Close)

	plugin := types.Plugin{
		ID:      "plugin-id",
		Name:    specFixtureIDs["/plugins/{name}"],
		Enabled: true,
		Config:  types.PluginConfig{Interface: types.PluginConfigInterface{Socket: "p.sock"}},
	}
	plugins := &mockPluginClient{
		pluginListFn: func(context.Context) (types.PluginsListResponse, error) {
			return types.PluginsListResponse{&plugin}, nil
		},
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			if name != plugin.Name {
				return nil, errdefs.NotFound(fmt.Errorf("plugin %s not found", name))
			}
			return &plugin, nil
		},
	}

	system := &mockSystemClient{
		swarmInspectFn: func(context.Context) (swarm.Swarm, error) { return validSwarm(), nil },
		diskUsageFn: func(context.Context) (types.DiskUsage, error) {
			return types.DiskUsage{LayersSize: 1 << 20}, nil
		},
		getUnlockKeyFn: func(context.Context) (string, error) { return "SWMKEY-1-test", nil },
	}

	h := newTestHandlers(t, withCache(c), withPromClient(promapi.NewClient(prom.URL)),
		withPluginClient(plugins), withSystemClient(system))

	// Seeded, so /-/docker-latest-version answers without calling GitHub.
	h.dockerVersionCache.version = &DockerLatestVersion{
		Version: "28.0.0",
		URL:     "https://github.com/moby/moby/releases/tag/v28.0.0",
	}
	h.dockerVersionCache.fetchedAt = time.Now()

	entries := cluster.NewSecretEntries()
	h.SetSecretEntries(entries)

	entry, err := entries.Open("anonymous", "db_password", nil, "utf8")
	if err != nil {
		t.Fatal(err)
	}

	b := sse.NewBroadcaster(0, nil)
	t.Cleanup(b.Close)

	return newTestRouter(t, h, b, specBytes), map[string]string{
		"/secret-entries/{id}": "/secret-entries/" + entry.ID,
		"/search":              "/search?q=web",
	}
}
