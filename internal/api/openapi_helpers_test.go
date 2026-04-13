package api

import (
	"net/http"
	"os"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// loadTestSpec reads the OpenAPI spec from disk, validates it, and returns
// the raw bytes, parsed document, and request router. Shared by all
// spec-conformance tests.
func loadTestSpec(t *testing.T) ([]byte, *openapi3.T, routers.Router) {
	t.Helper()

	specPath := "../../api/openapi.yaml"
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(specBytes)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}

	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("spec validation: %v", err)
	}

	specRouter, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build spec router: %v", err)
	}

	return specBytes, doc, specRouter
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
// (node-1, svc-1, task-1, cfg-1, sec-1, net-1) match resolvePath().
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
			Annotations: swarm.Annotations{Name: "web"},
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
}
