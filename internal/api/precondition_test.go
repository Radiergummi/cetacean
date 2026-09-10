package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

func TestPreconditionOnServiceEnv(t *testing.T) {
	// stubEnvWriteClient lets a PATCH that reaches the handler actually
	// succeed, so "admitted"/"unaffected" subtests can assert the real 200
	// rather than merely "not 412" — a check a handler-side panic would pass
	// just as well.
	stubEnvWriteClient := &mockWriteClient{
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceEnvFn: func(
				_ context.Context,
				id string,
				env map[string]string,
			) (swarm.Service, error) {
				envSlice := make([]string, 0, len(env))
				for k, v := range env {
					envSlice = append(envSlice, k+"="+v)
				}
				return swarm.Service{
					ID:   id,
					Meta: swarm.Meta{Version: swarm.Version{Index: 8}},
					Spec: swarm.ServiceSpec{
						Annotations: swarm.Annotations{Name: "web"},
						TaskTemplate: swarm.TaskSpec{
							ContainerSpec: &swarm.ContainerSpec{Env: envSlice},
						},
					},
				}, nil
			},
		},
	}

	newServer := func(t *testing.T) (http.Handler, string) {
		t.Helper()
		c := cache.New(nil)
		c.SetService(swarm.Service{
			ID:   "svc1",
			Meta: swarm.Meta{Version: swarm.Version{Index: 7}},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: "web"},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Env: []string{"A=1"}},
				},
			},
		})
		router := newTestRouterWithCache(t, c, withWriteClient(stubEnvWriteClient))

		req := httptest.NewRequest("GET", "/services/svc1/env", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want 200", rec.Code)
		}
		return router, rec.Header().Get("ETag")
	}

	patch := func(t *testing.T, router http.Handler, ifMatch string) int {
		t.Helper()
		req := httptest.NewRequest("PATCH", "/services/svc1/env",
			strings.NewReader(`{"B":"2"}`))
		req.Header.Set("Content-Type", "application/merge-patch+json")
		req.Header.Set("Accept", "application/json")
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("matching If-Match is admitted", func(t *testing.T) {
		router, etag := newServer(t)
		if got := patch(t, router, etag); got != http.StatusOK {
			t.Errorf("status = %d, want 200 with a matching If-Match", got)
		}
	})

	t.Run("stale If-Match is refused", func(t *testing.T) {
		router, _ := newServer(t)
		if got := patch(t, router, `"stale"`); got != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want 412", got)
		}
	})

	t.Run("weak validator is refused", func(t *testing.T) {
		router, etag := newServer(t)
		weak := `W/` + etag
		if got := patch(t, router, weak); got != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want 412 — weak validators never match If-Match", got)
		}
	})

	t.Run("absent If-Match changes nothing", func(t *testing.T) {
		router, _ := newServer(t)
		if got := patch(t, router, ""); got != http.StatusOK {
			t.Errorf("status = %d, want 200 with no If-Match header", got)
		}
	})

	t.Run("wildcard on a missing resource is 412 not 404", func(t *testing.T) {
		router := newTestRouterWithCache(t, cache.New(nil))
		req := httptest.NewRequest("PATCH", "/services/gone/env",
			strings.NewReader(`{"B":"2"}`))
		req.Header.Set("Content-Type", "application/merge-patch+json")
		req.Header.Set("If-Match", "*")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusPreconditionFailed {
			t.Errorf("status = %d, want 412 (RFC 9110 §13.2.2)", rec.Code)
		}
	})
}

// preconditionEndpoint is one paired (GET, write) endpoint of the round-trip
// table below.
type preconditionEndpoint struct {
	name        string
	getPath     string
	writeMethod string
	writePath   string
	body        string
	contentType string

	// wantStatus is the status the write answers when its precondition holds.
	// Always a concrete status: "not 412" would also pass on a 500.
	wantStatus int
}

// pairedEndpoints is every path in the router carrying both a GET and a write
// method, and so every path that declares an If-Match precondition. The three
// collection creates (POST /configs, /secrets, /plugins) are deliberately
// absent: their paired GET is a collection whose ETag turns over on any
// member change, which makes "create only if the collection is unchanged" a
// precondition nobody can satisfy.
//
// /services/{id}/healthcheck appears twice — PUT replaces and PATCH merges,
// both against the one representation.
var pairedEndpoints = []preconditionEndpoint{
	{
		"service env", "/services/svc1/env", "PATCH", "/services/svc1/env",
		`{"B":"2"}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service labels", "/services/svc1/labels", "PATCH", "/services/svc1/labels",
		`{"x":"y"}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service resources", "/services/svc1/resources", "PATCH", "/services/svc1/resources",
		`{"Limits":{"MemoryBytes":1024}}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service healthcheck (PUT)", "/services/svc1/healthcheck",
		"PUT", "/services/svc1/healthcheck",
		`{"Test":["CMD","true"]}`, "application/json", http.StatusOK,
	},
	{
		"service healthcheck (PATCH)", "/services/svc1/healthcheck",
		"PATCH", "/services/svc1/healthcheck",
		`{"Retries":5}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service placement", "/services/svc1/placement", "PUT", "/services/svc1/placement",
		`{"Constraints":["node.role==worker"]}`, "application/json", http.StatusOK,
	},
	{
		"service ports", "/services/svc1/ports", "PATCH", "/services/svc1/ports",
		`{"ports":[{"Protocol":"tcp","TargetPort":80,"PublishedPort":8080}]}`,
		"application/merge-patch+json", http.StatusOK,
	},
	{
		"service update policy", "/services/svc1/update-policy",
		"PATCH", "/services/svc1/update-policy",
		`{"Parallelism":2}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service rollback policy", "/services/svc1/rollback-policy",
		"PATCH", "/services/svc1/rollback-policy",
		`{"Parallelism":3}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service log driver", "/services/svc1/log-driver", "PATCH", "/services/svc1/log-driver",
		`{"Name":"json-file"}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service configs", "/services/svc1/configs", "PATCH", "/services/svc1/configs",
		`{"configs":[{"configID":"cfg1","configName":"app-config","fileName":""}]}`,
		"application/merge-patch+json", http.StatusOK,
	},
	{
		"service secrets", "/services/svc1/secrets", "PATCH", "/services/svc1/secrets",
		`{"secrets":[{"secretID":"sec1","secretName":"app-secret","fileName":""}]}`,
		"application/merge-patch+json", http.StatusOK,
	},
	{
		"service networks", "/services/svc1/networks", "PATCH", "/services/svc1/networks",
		`{"networks":[{"target":"net1"}]}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service mounts", "/services/svc1/mounts", "PATCH", "/services/svc1/mounts",
		`{"mounts":[{"Type":"volume","Source":"vol1","Target":"/data"}]}`,
		"application/merge-patch+json", http.StatusOK,
	},
	{
		"service container config", "/services/svc1/container-config",
		"PATCH", "/services/svc1/container-config",
		`{"user":"nobody"}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"service mode", "/services/svc1/mode", "PUT", "/services/svc1/mode",
		`{"mode":"replicated","replicas":3}`, "application/json", http.StatusOK,
	},
	{
		"service endpoint mode", "/services/svc1/endpoint-mode",
		"PUT", "/services/svc1/endpoint-mode",
		`{"mode":"dnsrr"}`, "application/json", http.StatusOK,
	},
	{
		"service", "/services/svc1", "DELETE", "/services/svc1",
		"", "", http.StatusNoContent,
	},
	{
		"node labels", "/nodes/node1/labels", "PATCH", "/nodes/node1/labels",
		`{"zone":"eu"}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"node role", "/nodes/node1/role", "PUT", "/nodes/node1/role",
		`{"role":"worker"}`, "application/json", http.StatusOK,
	},
	{
		"node", "/nodes/node1", "DELETE", "/nodes/node1",
		"", "", http.StatusNoContent,
	},
	{
		"config labels", "/configs/cfg1/labels", "PATCH", "/configs/cfg1/labels",
		`{"tier":"web"}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"config", "/configs/cfg1", "DELETE", "/configs/cfg1",
		"", "", http.StatusNoContent,
	},
	{
		"secret labels", "/secrets/sec1/labels", "PATCH", "/secrets/sec1/labels",
		`{"tier":"web"}`, "application/merge-patch+json", http.StatusOK,
	},
	{
		"secret", "/secrets/sec1", "DELETE", "/secrets/sec1",
		"", "", http.StatusNoContent,
	},
	{
		"network", "/networks/net1", "DELETE", "/networks/net1",
		"", "", http.StatusNoContent,
	},
	{
		"volume", "/volumes/vol1", "DELETE", "/volumes/vol1",
		"", "", http.StatusNoContent,
	},
	{
		"task", "/tasks/task1", "DELETE", "/tasks/task1",
		"", "", http.StatusNoContent,
	},
	{
		"stack", "/stacks/demo", "DELETE", "/stacks/demo",
		"", "", http.StatusOK,
	},
	{
		"plugin", "/plugins/plug1", "DELETE", "/plugins/plug1",
		"", "", http.StatusNoContent,
	},
}

// TestPreconditionRoundTripsForEveryPairedEndpoint reads each endpoint that
// declares a precondition, then feeds that exact ETag back as If-Match on its
// write. A representation builder that does not reproduce the GET's bytes
// fails here rather than in production.
//
// The second row per endpoint sends a syntactically valid strong tag that
// cannot match and requires 412. Without it a route that simply never got its
// precond wrapper would pass the first row: an unconditioned write admits
// every If-Match, including the right one.
func TestPreconditionRoundTripsForEveryPairedEndpoint(t *testing.T) {
	write := func(t *testing.T, router http.Handler, tc preconditionEndpoint, ifMatch string) int {
		t.Helper()

		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}

		req := httptest.NewRequest(tc.writeMethod, tc.writePath, body)
		req.Header.Set("If-Match", ifMatch)
		req.Header.Set("Accept", "application/json")
		if tc.contentType != "" {
			req.Header.Set("Content-Type", tc.contentType)
		}

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		return rec.Code
	}

	readETag := func(t *testing.T, router http.Handler, path string) string {
		t.Helper()

		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200; body: %s", path, rec.Code, rec.Body.String())
		}

		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("GET %s returned no ETag", path)
		}

		return etag
	}

	for _, tc := range pairedEndpoints {
		t.Run(tc.name+"/matching If-Match is admitted", func(t *testing.T) {
			router := newSeededTestRouter(t)
			etag := readETag(t, router, tc.getPath)

			if got := write(t, router, tc, etag); got != tc.wantStatus {
				t.Errorf(
					"%s %s = %d, want %d with the ETag its own GET just returned",
					tc.writeMethod, tc.writePath, got, tc.wantStatus,
				)
			}
		})

		t.Run(tc.name+"/mismatched If-Match is refused", func(t *testing.T) {
			router := newSeededTestRouter(t)

			if got := write(t, router, tc, `"bogus-etag"`); got != http.StatusPreconditionFailed {
				t.Errorf(
					"%s %s = %d, want 412 — the route declares no precondition",
					tc.writeMethod, tc.writePath, got,
				)
			}
		})
	}
}

// seededStack is the stack namespace every seeded resource is labelled with,
// which is what makes GET /stacks/demo resolve — stacks are derived from
// labels rather than stored.
const seededStack = "demo"

// seededService is the one service in the fixture, carrying every spec
// section a paired sub-resource reads so no representation renders from a
// nil pointer and passes for the wrong reason.
func seededService() swarm.Service {
	replicas := uint64(2)
	stopGrace := 10 * time.Second

	return swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 7}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: "web",
				Labels: map[string]string{
					"com.docker.stack.namespace": seededStack,
					"tier":                       "frontend",
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
			UpdateConfig:   &swarm.UpdateConfig{Parallelism: 1},
			RollbackConfig: &swarm.UpdateConfig{Parallelism: 1},
			EndpointSpec: &swarm.EndpointSpec{
				Mode: swarm.ResolutionModeVIP,
				Ports: []swarm.PortConfig{
					{Protocol: swarm.PortConfigProtocolTCP, TargetPort: 80, PublishedPort: 8080},
				},
			},
			TaskTemplate: swarm.TaskSpec{
				Networks:  []swarm.NetworkAttachmentConfig{{Target: "net1"}},
				Resources: &swarm.ResourceRequirements{},
				Placement: &swarm.Placement{Constraints: []string{"node.role==worker"}},
				LogDriver: &swarm.Driver{Name: "json-file"},
				ContainerSpec: &swarm.ContainerSpec{
					Image:           "nginx:1.27",
					Env:             []string{"A=1"},
					User:            "root",
					StopGracePeriod: &stopGrace,
					Healthcheck:     &container.HealthConfig{Retries: 3},
					Mounts: []mount.Mount{
						{Type: mount.TypeVolume, Source: "vol1", Target: "/data"},
					},
					Configs: []*swarm.ConfigReference{{
						ConfigID:   "cfg1",
						ConfigName: "app-config",
						File:       &swarm.ConfigReferenceFileTarget{Name: "/app-config"},
					}},
					Secrets: []*swarm.SecretReference{{
						SecretID:   "sec1",
						SecretName: "app-secret",
						File: &swarm.SecretReferenceFileTarget{
							Name: "/run/secrets/app-secret",
						},
					}},
					DNSConfig: &swarm.DNSConfig{Nameservers: []string{"1.1.1.1"}},
				},
			},
		},
	}
}

// seededWriteClient accepts every write in pairedEndpoints. The returned
// resources are minimal on purpose — the round trip asserts the status the
// write reaches, not what it echoes back.
func seededWriteClient() *mockWriteClient {
	updated := func(id string) (swarm.Service, error) {
		return swarm.Service{
			ID:   id,
			Meta: swarm.Meta{Version: swarm.Version{Index: 8}},
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
		}, nil
	}
	node := func(id string) (swarm.Node, error) {
		return swarm.Node{ID: id, Meta: swarm.Meta{Version: swarm.Version{Index: 8}}}, nil
	}

	return &mockWriteClient{
		mockServiceLifecycleWriter: mockServiceLifecycleWriter{
			removeServiceFn: func(context.Context, string) error { return nil },
			updateServiceModeFn: func(_ context.Context, id string, _ swarm.ServiceMode) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceEndpointModeFn: func(_ context.Context, id string, _ swarm.ResolutionMode) (swarm.Service, error) {
				return updated(id)
			},
		},
		mockServiceSpecWriter: mockServiceSpecWriter{
			updateServiceEnvFn: func(_ context.Context, id string, _ map[string]string) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceLabelsFn: func(_ context.Context, id string, _ map[string]string) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceResourcesFn: func(_ context.Context, id string, _ *swarm.ResourceRequirements) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceHealthcheckFn: func(_ context.Context, id string, _ *container.HealthConfig) (swarm.Service, error) {
				return updated(id)
			},
			updateServicePlacementFn: func(_ context.Context, id string, _ *swarm.Placement) (swarm.Service, error) {
				return updated(id)
			},
			updateServicePortsFn: func(_ context.Context, id string, _ []swarm.PortConfig) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceUpdatePolicyFn: func(_ context.Context, id string, _ *swarm.UpdateConfig) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceRollbackPolicyFn: func(_ context.Context, id string, _ *swarm.UpdateConfig) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceLogDriverFn: func(_ context.Context, id string, _ *swarm.Driver) (swarm.Service, error) {
				return updated(id)
			},
		},
		mockServiceAttachmentWriter: mockServiceAttachmentWriter{
			updateServiceConfigsFn: func(_ context.Context, id string, _ []*swarm.ConfigReference) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceSecretsFn: func(_ context.Context, id string, _ []*swarm.SecretReference) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceNetworksFn: func(_ context.Context, id string, _ []swarm.NetworkAttachmentConfig) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceMountsFn: func(_ context.Context, id string, _ []mount.Mount) (swarm.Service, error) {
				return updated(id)
			},
			updateServiceContainerConfigFn: func(_ context.Context, id string, _ func(*swarm.ContainerSpec)) (swarm.Service, error) {
				return updated(id)
			},
		},
		mockNodeWriter: mockNodeWriter{
			updateNodeLabelsFn: func(_ context.Context, id string, _ map[string]string) (swarm.Node, error) {
				return node(id)
			},
			updateNodeRoleFn: func(_ context.Context, id string, _ swarm.NodeRole) (swarm.Node, error) {
				return node(id)
			},
			removeNodeFn: func(context.Context, string, bool) error { return nil },
		},
		mockConfigWriter: mockConfigWriter{
			removeConfigFn: func(context.Context, string) error { return nil },
			updateConfigLabelsFn: func(_ context.Context, id string, _ map[string]string) (swarm.Config, error) {
				return swarm.Config{ID: id}, nil
			},
		},
		mockSecretWriter: mockSecretWriter{
			removeSecretFn: func(context.Context, string) error { return nil },
			updateSecretLabelsFn: func(_ context.Context, id string, _ map[string]string) (swarm.Secret, error) {
				return swarm.Secret{ID: id}, nil
			},
		},
		mockResourceRemover: mockResourceRemover{
			removeTaskFn:    func(context.Context, string) error { return nil },
			removeNetworkFn: func(context.Context, string) error { return nil },
			removeVolumeFn:  func(context.Context, string, bool) error { return nil },
		},
	}
}

// newSeededTestRouter builds a router over a cache holding one of every
// resource type the paired endpoints address, plus write and plugin clients
// that accept every write in pairedEndpoints. Each subtest gets its own, so a
// row that removes a resource cannot affect the next.
func newSeededTestRouter(t testing.TB) http.Handler {
	t.Helper()

	stackLabels := map[string]string{"com.docker.stack.namespace": seededStack}

	c := cache.New(nil)
	c.SetService(seededService())
	c.SetNode(swarm.Node{
		ID:   "node1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 3}},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
			Annotations:  swarm.Annotations{Labels: map[string]string{"zone": "eu-west"}},
		},
		Description:   swarm.NodeDescription{Hostname: "worker-1"},
		ManagerStatus: &swarm.ManagerStatus{Leader: true},
	})
	c.SetTask(swarm.Task{
		ID:        "task1",
		ServiceID: "svc1",
		NodeID:    "node1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})
	c.SetConfig(swarm.Config{
		ID: "cfg1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "app-config", Labels: stackLabels},
		},
	})
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "app-secret", Labels: stackLabels},
			Data:        []byte("hunter2"),
		},
	})
	c.SetNetwork(network.Summary{
		ID:     "net1",
		Name:   "app-net",
		Driver: "overlay",
		Labels: stackLabels,
	})
	c.SetVolume(volume.Volume{
		Name:      "vol1",
		Driver:    "local",
		CreatedAt: "2026-01-01T00:00:00Z",
		Labels:    stackLabels,
	})

	plugins := &mockPluginClient{
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			return &types.Plugin{ID: "plugid", Name: name, Enabled: true}, nil
		},
		pluginRemoveFn: func(context.Context, string, bool) error { return nil },
	}

	return newTestRouterWithCache(
		t,
		c,
		withWriteClient(seededWriteClient()),
		withPluginClient(plugins),
	)
}
