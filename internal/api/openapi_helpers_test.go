package api

import (
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
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
		Handlers:          handlers,
		Broadcaster:       broadcaster,
		SPA:               noopSPA,
		OpenAPISpec:       specBytes,
		EnableSelfMetrics: true,
		AuthProvider:      &auth.NoneProvider{},
	})
}

// populateSpecFixtures seeds the cache with resource IDs that the exhaustive
// contract test uses when resolving path templates. The fixture IDs
// (node-1, svc-1, task-1, cfg-1, sec-1, net-1, vol-1, stack "myapp") match
// resolvePath(). The service carries a stack-namespace label so the "myapp"
// stack is derivable from cache state.
func populateSpecFixtures(c *cache.Cache) {
	c.SetNode(swarm.Node{
		ID: "node-1",
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
		ID: "svc-1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "web",
				Labels: map[string]string{"com.docker.stack.namespace": "myapp"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.27"},
			},
		},
	})

	c.SetTask(swarm.Task{
		ID:           "task-1",
		ServiceID:    "svc-1",
		NodeID:       "node-1",
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
		DesiredState: swarm.TaskStateRunning,
	})

	c.SetConfig(swarm.Config{
		ID:   "cfg-1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}},
	})

	c.SetSecret(swarm.Secret{
		ID:   "sec-1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}},
	})

	c.SetNetwork(network.Summary{ID: "net-1", Name: "net"})

	c.SetVolume(volume.Volume{Name: "vol-1", Driver: "local"})
}
