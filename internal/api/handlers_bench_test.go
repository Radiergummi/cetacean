package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func populateCache(c *cache.Cache, n int) {
	for i := range n {
		stack := fmt.Sprintf("stack-%d", i%5)
		id := fmt.Sprintf("id-%d", i)

		c.SetNode(swarm.Node{
			ID:     id,
			Status: swarm.NodeStatus{State: swarm.NodeStateReady},
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
				Annotations:  swarm.Annotations{Labels: map[string]string{"env": "prod"}},
			},
			Description: swarm.NodeDescription{
				Hostname: fmt.Sprintf("node-%d.example.com", i),
				Resources: swarm.Resources{
					NanoCPUs:    4_000_000_000,
					MemoryBytes: 8 * 1024 * 1024 * 1024,
				},
			},
		})

		replicas := uint64(3)
		c.SetService(swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("svc-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{
						Image: fmt.Sprintf("registry.example.com/app-%d:latest@sha256:abc123", i),
						Configs: []*swarm.ConfigReference{
							{ConfigID: id, ConfigName: fmt.Sprintf("cfg-%d", i)},
						},
						Secrets: []*swarm.SecretReference{
							{SecretID: id, SecretName: fmt.Sprintf("sec-%d", i)},
						},
						Mounts: []mount.Mount{
							{Type: mount.TypeVolume, Source: fmt.Sprintf("vol-%d", i)},
						},
					},
					Networks: []swarm.NetworkAttachmentConfig{
						{Target: id},
					},
				},
			},
			Endpoint: swarm.Endpoint{
				VirtualIPs: []swarm.EndpointVirtualIP{
					{NetworkID: id},
				},
			},
		})

		c.SetTask(swarm.Task{
			ID:        id,
			ServiceID: fmt.Sprintf("id-%d", i%10),
			NodeID:    fmt.Sprintf("id-%d", i%10),
			Slot:      i%3 + 1,
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
			Spec: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: fmt.Sprintf("registry.example.com/app-%d:latest", i%10),
				},
			},
		})

		c.SetConfig(swarm.Config{
			ID: id,
			Spec: swarm.ConfigSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("cfg-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		})

		c.SetSecret(swarm.Secret{
			ID: id,
			Spec: swarm.SecretSpec{
				Annotations: swarm.Annotations{
					Name:   fmt.Sprintf("sec-%d", i),
					Labels: map[string]string{"com.docker.stack.namespace": stack},
				},
			},
		})

		c.SetNetwork(network.Summary{
			ID:     id,
			Name:   fmt.Sprintf("net-%d", i),
			Driver: "overlay",
			Scope:  "swarm",
			Labels: map[string]string{"com.docker.stack.namespace": stack},
		})

		c.SetVolume(volume.Volume{
			Name:   fmt.Sprintf("vol-%d", i),
			Driver: "local",
			Scope:  "local",
			Labels: map[string]string{"com.docker.stack.namespace": stack},
		})
	}
}

var handlerSizes = []int{10, 100, 1000}

// benchHandler runs a handler benchmark across all sizes.
func benchHandler(b *testing.B, name string, fn func(b *testing.B, h *Handlers)) {
	b.Helper()
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			fn(b, h)
		})
	}
}

// =============================================================================
// List endpoints
// =============================================================================

func BenchmarkHandleListNodes(b *testing.B) {
	benchHandler(b, "ListNodes", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes", nil)
			h.HandleListNodes(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListServices(b *testing.B) {
	benchHandler(b, "ListServices", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/services", nil)
			h.HandleListServices(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListTasks(b *testing.B) {
	benchHandler(b, "ListTasks", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/tasks", nil)
			h.HandleListTasks(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListStacks(b *testing.B) {
	benchHandler(b, "ListStacks", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/stacks", nil)
			h.HandleListStacks(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListConfigs(b *testing.B) {
	benchHandler(b, "ListConfigs", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/configs", nil)
			h.HandleListConfigs(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListSecrets(b *testing.B) {
	benchHandler(b, "ListSecrets", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/secrets", nil)
			h.HandleListSecrets(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListNetworks(b *testing.B) {
	benchHandler(b, "ListNetworks", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/networks", nil)
			h.HandleListNetworks(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListVolumes(b *testing.B) {
	benchHandler(b, "ListVolumes", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/volumes", nil)
			h.HandleListVolumes(httptest.NewRecorder(), req)
		}
	})
}

// =============================================================================
// Detail endpoints
// =============================================================================

func BenchmarkHandleGetNode(b *testing.B) {
	benchHandler(b, "GetNode", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes/id-0", nil)
			req.SetPathValue("id", "id-0")
			h.HandleGetNode(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetService(b *testing.B) {
	benchHandler(b, "GetService", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/services/id-0", nil)
			req.SetPathValue("id", "id-0")
			h.HandleGetService(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetTask(b *testing.B) {
	benchHandler(b, "GetTask", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/tasks/id-0", nil)
			req.SetPathValue("id", "id-0")
			h.HandleGetTask(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetStack(b *testing.B) {
	benchHandler(b, "GetStack", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/stacks/stack-0", nil)
			req.SetPathValue("name", "stack-0")
			h.HandleGetStack(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetConfig(b *testing.B) {
	benchHandler(b, "GetConfig", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/configs/id-0", nil)
			req.SetPathValue("id", "id-0")
			h.HandleGetConfig(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetSecret(b *testing.B) {
	benchHandler(b, "GetSecret", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/secrets/id-0", nil)
			req.SetPathValue("id", "id-0")
			h.HandleGetSecret(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetNetwork(b *testing.B) {
	benchHandler(b, "GetNetwork", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/networks/id-0", nil)
			req.SetPathValue("id", "id-0")
			h.HandleGetNetwork(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetVolume(b *testing.B) {
	benchHandler(b, "GetVolume", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/volumes/vol-0", nil)
			req.SetPathValue("name", "vol-0")
			h.HandleGetVolume(httptest.NewRecorder(), req)
		}
	})
}

// =============================================================================
// Sub-resource endpoints
// =============================================================================

func BenchmarkHandleNodeTasks(b *testing.B) {
	benchHandler(b, "NodeTasks", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes/id-0/tasks", nil)
			req.SetPathValue("id", "id-0")
			h.HandleNodeTasks(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleServiceTasks(b *testing.B) {
	benchHandler(b, "ServiceTasks", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/services/id-0/tasks", nil)
			req.SetPathValue("id", "id-0")
			h.HandleServiceTasks(httptest.NewRecorder(), req)
		}
	})
}

// =============================================================================
// Aggregate / meta endpoints
// =============================================================================

func BenchmarkHandleCluster(b *testing.B) {
	benchHandler(b, "Cluster", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/cluster", nil)
			h.HandleCluster(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleStackSummary(b *testing.B) {
	benchHandler(b, "StackSummary", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/stacks/summary", nil)
			h.HandleStackSummary(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleHistory(b *testing.B) {
	benchHandler(b, "History", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/history", nil)
			h.HandleHistory(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleHealth(b *testing.B) {
	h := NewHandlers(cache.New(nil), nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	for b.Loop() {
		req := httptest.NewRequestWithContext(b.Context(), "GET", "/-/health", nil)
		h.HandleHealth(httptest.NewRecorder(), req)
	}
}

func BenchmarkHandleReady(b *testing.B) {
	h := NewHandlers(cache.New(nil), nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	for b.Loop() {
		req := httptest.NewRequestWithContext(b.Context(), "GET", "/-/ready", nil)
		h.HandleReady(httptest.NewRecorder(), req)
	}
}

func BenchmarkHandleMonitoringStatus_NoPrometheus(b *testing.B) {
	h := NewHandlers(cache.New(nil), nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
	for b.Loop() {
		req := httptest.NewRequestWithContext(b.Context(), "GET", "/-/metrics/status", nil)
		h.HandleMonitoringStatus(httptest.NewRecorder(), req)
	}
}

// =============================================================================
// Topology endpoints
// =============================================================================

func BenchmarkHandleNetworkTopology(b *testing.B) {
	benchHandler(b, "NetworkTopology", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/topology/networks", nil)
			h.HandleNetworkTopology(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandlePlacementTopology(b *testing.B) {
	benchHandler(b, "PlacementTopology", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/topology/placement", nil)
			h.HandlePlacementTopology(httptest.NewRecorder(), req)
		}
	})
}

// =============================================================================
// Search
// =============================================================================

func BenchmarkHandleSearch(b *testing.B) {
	benchHandler(b, "Search", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/search?q=svc-5", nil)
			h.HandleSearch(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleSearch_Broad(b *testing.B) {
	benchHandler(b, "Search_Broad", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(
				b.Context(),
				"GET",
				"/search?q=stack&limit=0",
				nil,
			)
			h.HandleSearch(httptest.NewRecorder(), req)
		}
	})
}

// =============================================================================
// Query parameter variations (search, filter, sort, pagination)
// =============================================================================

func BenchmarkHandleListNodes_Search(b *testing.B) {
	benchHandler(b, "ListNodes_Search", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes?search=node-5", nil)
			h.HandleListNodes(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListServices_Search(b *testing.B) {
	benchHandler(b, "ListServices_Search", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/services?search=svc-5", nil)
			h.HandleListServices(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListNodes_Filter(b *testing.B) {
	benchHandler(b, "ListNodes_Filter", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(
				b.Context(),
				"GET",
				`/nodes?filter=Status+%3D%3D+"ready"`,
				nil,
			)
			h.HandleListNodes(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListServices_Filter(b *testing.B) {
	benchHandler(b, "ListServices_Filter", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(
				b.Context(),
				"GET",
				`/services?filter=Mode+%3D%3D+"replicated"`,
				nil,
			)
			h.HandleListServices(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListTasks_Filter(b *testing.B) {
	benchHandler(b, "ListTasks_Filter", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(
				b.Context(),
				"GET",
				`/tasks?filter=State+%3D%3D+"running"`,
				nil,
			)
			h.HandleListTasks(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListNodes_Sort(b *testing.B) {
	benchHandler(b, "ListNodes_Sort", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(
				b.Context(),
				"GET",
				"/nodes?sort=hostname&dir=desc",
				nil,
			)
			h.HandleListNodes(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListServices_Sort(b *testing.B) {
	benchHandler(b, "ListServices_Sort", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(
				b.Context(),
				"GET",
				"/services?sort=name&dir=desc",
				nil,
			)
			h.HandleListServices(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleListNodes_Paginated(b *testing.B) {
	benchHandler(b, "ListNodes_Paginated", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(
				b.Context(),
				"GET",
				"/nodes?limit=10&offset=5",
				nil,
			)
			h.HandleListNodes(httptest.NewRecorder(), req)
		}
	})
}

// =============================================================================
// ETag / conditional responses
// =============================================================================

func BenchmarkHandleListNodes_WithETag(b *testing.B) {
	benchHandler(b, "ListNodes_WithETag", func(b *testing.B, h *Handlers) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes", nil)
			req.Header.Set("If-None-Match", `"some-old-etag"`)
			h.HandleListNodes(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkHandleGetNode_ETagHit(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes/id-0", nil)
			req.SetPathValue("id", "id-0")
			w := httptest.NewRecorder()
			h.HandleGetNode(w, req)
			etag := w.Header().Get("ETag")

			b.ResetTimer()
			for b.Loop() {
				req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes/id-0", nil)
				req.SetPathValue("id", "id-0")
				req.Header.Set("If-None-Match", etag)
				w := httptest.NewRecorder()
				h.HandleGetNode(w, req)
				if w.Code != http.StatusNotModified {
					b.Fatalf("expected 304, got %d", w.Code)
				}
			}
		})
	}
}

// =============================================================================
// Content negotiation
// =============================================================================

func BenchmarkParseAccept(b *testing.B) {
	headers := map[string]string{
		"empty":      "",
		"json":       "application/json",
		"html":       "text/html",
		"sse":        "text/event-stream",
		"browser":    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"multi_json": "application/json, text/html;q=0.9, */*;q=0.8",
	}
	for name, accept := range headers {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				parseAccept(accept)
			}
		})
	}
}

// =============================================================================
// ETag computation and matching
// =============================================================================

func BenchmarkComputeETag(b *testing.B) {
	for _, size := range []int{256, 4096, 65536} {
		body := make([]byte, size)
		rand.Read(body)
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			for b.Loop() {
				computeETag(body)
			}
		})
	}
}

func BenchmarkETagMatch(b *testing.B) {
	etag := `"abcdef1234567890abcdef1234567890"`
	b.Run("miss", func(b *testing.B) {
		header := `"0000000000000000"`
		for b.Loop() {
			etagMatch(header, etag)
		}
	})
	b.Run("hit_single", func(b *testing.B) {
		for b.Loop() {
			etagMatch(etag, etag)
		}
	})
	b.Run("hit_multi", func(b *testing.B) {
		header := `"aaa", "bbb", W/"ccc", ` + etag + `, "ddd"`
		for b.Loop() {
			etagMatch(header, etag)
		}
	})
	b.Run("wildcard", func(b *testing.B) {
		for b.Loop() {
			etagMatch("*", etag)
		}
	})
}

// =============================================================================
// JSON-LD DetailResponse serialization
// =============================================================================

func BenchmarkDetailResponseMarshalJSON(b *testing.B) {
	b.Run("small", func(b *testing.B) {
		dr := NewDetailResponse("/nodes/n1", "Node", map[string]any{
			"node": swarm.Node{ID: "n1"},
		})
		for b.Loop() {
			_, _ = json.Marshal(dr)
		}
	})
	b.Run("with_services", func(b *testing.B) {
		services := make([]any, 5)
		for i := range services {
			replicas := uint64(3)
			services[i] = swarm.Service{
				ID: fmt.Sprintf("svc-%d", i),
				Spec: swarm.ServiceSpec{
					Annotations: swarm.Annotations{Name: fmt.Sprintf("mystack_web-%d", i)},
					Mode: swarm.ServiceMode{
						Replicated: &swarm.ReplicatedService{Replicas: &replicas},
					},
				},
			}
		}
		dr := NewDetailResponse("/configs/cfg1", "Config", map[string]any{
			"config":   swarm.Config{ID: "cfg1"},
			"services": services,
		})
		for b.Loop() {
			_, _ = json.Marshal(dr)
		}
	})
}

// =============================================================================
// Log line parsing
// =============================================================================

func BenchmarkParseLine(b *testing.B) {
	b.Run("with_details", func(b *testing.B) {
		line := "2026-03-12T10:30:45.123456789Z com.docker.swarm.node.id=abc123,com.docker.swarm.service.id=svc456,com.docker.swarm.task.id=task789 INFO: request processed successfully"
		for b.Loop() {
			parseLine(line, "stdout")
		}
	})
	b.Run("plain_message", func(b *testing.B) {
		line := "2026-03-12T10:30:45.123456789Z INFO: request processed successfully"
		for b.Loop() {
			parseLine(line, "stdout")
		}
	})
	b.Run("no_timestamp", func(b *testing.B) {
		line := "some raw log output without timestamp"
		for b.Loop() {
			parseLine(line, "stderr")
		}
	})
}

// =============================================================================
// labelsMatch (search hot path)
// =============================================================================

func BenchmarkLabelsMatch(b *testing.B) {
	labels := map[string]string{
		"com.docker.stack.namespace": "mystack",
		"com.docker.stack.image":     "registry.example.com/web:v2.1.0",
		"app.version":                "2.1.0",
		"maintainer":                 "team-platform",
		"environment":                "production",
	}
	b.Run("hit_value", func(b *testing.B) {
		for b.Loop() {
			labelsMatch(labels, "platform")
		}
	})
	b.Run("miss", func(b *testing.B) {
		for b.Loop() {
			labelsMatch(labels, "nonexistent")
		}
	})
	b.Run("hit_key", func(b *testing.B) {
		for b.Loop() {
			labelsMatch(labels, "namespace")
		}
	})
	b.Run("empty_labels", func(b *testing.B) {
		for b.Loop() {
			labelsMatch(nil, "anything")
		}
	})
}

// =============================================================================
// writeJSONWithETag (full marshal + hash + conditional response)
// =============================================================================

func BenchmarkWriteJSONWithETag(b *testing.B) {
	c := cache.New(nil)
	populateCache(c, 100)
	nodes := c.ListNodes()

	b.Run("miss", func(b *testing.B) {
		for b.Loop() {
			req := httptest.NewRequest("GET", "/nodes", nil)
			w := httptest.NewRecorder()
			writeJSONWithETag(w, req, nodes)
		}
	})
	b.Run("hit_304", func(b *testing.B) {
		// Pre-compute the ETag.
		body, _ := json.Marshal(nodes)
		etag := computeETag(body)
		for b.Loop() {
			req := httptest.NewRequest("GET", "/nodes", nil)
			req.Header.Set("If-None-Match", etag)
			w := httptest.NewRecorder()
			writeJSONWithETag(w, req, nodes)
		}
	})
}

// =============================================================================
// HandleSearch in-depth (labelsMatch is the inner hot path)
// =============================================================================

func BenchmarkHandleSearch_LabelHeavy(b *testing.B) {
	// Populate cache with many labels per resource to stress labelsMatch.
	c := cache.New(nil)
	for i := range 100 {
		id := fmt.Sprintf("id-%d", i)
		labels := map[string]string{
			"com.docker.stack.namespace": fmt.Sprintf("stack-%d", i%5),
		}
		for j := range 10 {
			labels[fmt.Sprintf("label-%d", j)] = fmt.Sprintf("value-%d-%d", i, j)
		}
		c.SetService(swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: fmt.Sprintf("svc-%d", i), Labels: labels},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{Image: fmt.Sprintf("img-%d", i)},
				},
			},
		})
	}
	h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)

	b.Run("label_hit", func(b *testing.B) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/search?q=value-50", nil)
			h.HandleSearch(httptest.NewRecorder(), req)
		}
	})
	b.Run("label_miss", func(b *testing.B) {
		for b.Loop() {
			req := httptest.NewRequestWithContext(b.Context(), "GET", "/search?q=zzzznotfound", nil)
			h.HandleSearch(httptest.NewRecorder(), req)
		}
	})
}

// =============================================================================
// Full list handler pipeline: filter + sort + paginate + ETag
// =============================================================================

func BenchmarkHandleListNodes_FullPipeline(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			for b.Loop() {
				req := httptest.NewRequestWithContext(
					b.Context(),
					"GET",
					"/nodes?filter="+strings.ReplaceAll(
						"role == \"worker\"",
						" ",
						"%20",
					)+"&sort=name&dir=desc&limit=10&offset=5",
					nil,
				)
				h.HandleListNodes(httptest.NewRecorder(), req)
			}
		})
	}
}

// =============================================================================
// SSE event encoding
// =============================================================================

// realisticServiceEvent returns an event with a full swarm.Service payload,
// representative of real-world SSE event sizes.
func realisticServiceEvent() cache.Event {
	replicas := uint64(3)
	return cache.Event{
		Type:   "service",
		Action: "update",
		ID:     "svc-abc123",
		Resource: swarm.Service{
			ID: "svc-abc123",
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name: "mystack_web",
					Labels: map[string]string{
						"com.docker.stack.namespace": "mystack",
						"com.docker.stack.image":     "registry.example.com/web:v2.1.0",
					},
				},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{Replicas: &replicas},
				},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{
						Image: "registry.example.com/web:v2.1.0@sha256:abcdef1234567890",
						Configs: []*swarm.ConfigReference{
							{ConfigID: "cfg-1", ConfigName: "mystack_nginx.conf"},
						},
						Secrets: []*swarm.SecretReference{
							{
								SecretID:   "sec-1",
								SecretName: "mystack_tls_cert",
							}, //nolint:gosec // test data
						},
					},
					Networks: []swarm.NetworkAttachmentConfig{
						{Target: "net-1"},
					},
				},
			},
			Endpoint: swarm.Endpoint{
				VirtualIPs: []swarm.EndpointVirtualIP{
					{NetworkID: "net-1", Addr: "10.0.0.5/24"},
				},
			},
		},
	}
}

func realisticTaskEvent() cache.Event {
	return cache.Event{
		Type:   "task",
		Action: "update",
		ID:     "task-xyz789",
		Resource: swarm.Task{
			ID:        "task-xyz789",
			ServiceID: "svc-abc123",
			NodeID:    "node-1",
			Slot:      1,
			Status: swarm.TaskStatus{
				State:   swarm.TaskStateRunning,
				Message: "started",
			},
			Spec: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "registry.example.com/web:v2.1.0@sha256:abcdef1234567890",
				},
			},
		},
	}
}

func BenchmarkToSSEEvent(b *testing.B) {
	b.Run("empty_resource", func(b *testing.B) {
		e := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
		for b.Loop() {
			toSSEEvent(e)
		}
	})
	b.Run("service_payload", func(b *testing.B) {
		e := realisticServiceEvent()
		for b.Loop() {
			toSSEEvent(e)
		}
	})
}

func BenchmarkWriteBatch(b *testing.B) {
	b.Run("single", func(b *testing.B) {
		events := []cache.Event{realisticServiceEvent()}
		var id uint64
		for b.Loop() {
			id = 0
			writeBatch(io.Discard, discardFlusher{}, events, &id)
		}
	})
	for _, n := range []int{5, 20} {
		b.Run(fmt.Sprintf("batch=%d", n), func(b *testing.B) {
			events := make([]cache.Event, n)
			for i := range n {
				events[i] = cache.Event{
					Type:     "task",
					Action:   "update",
					ID:       fmt.Sprintf("task-%d", i),
					Resource: realisticTaskEvent().Resource,
				}
			}
			var id uint64
			for b.Loop() {
				id = 0
				writeBatch(io.Discard, discardFlusher{}, events, &id)
			}
		})
	}
}

// discardFlusher is an http.Flusher that does nothing (pairs with io.Discard).
type discardFlusher struct{}

func (discardFlusher) Flush() {}

// =============================================================================
// SSE matcher functions
// =============================================================================

func BenchmarkTypeMatcher(b *testing.B) {
	match := typeMatcher("service")
	events := []struct {
		name string
		e    cache.Event
	}{
		{"hit", cache.Event{Type: "service", Action: "update", ID: "s1"}},
		{"miss", cache.Event{Type: "node", Action: "update", ID: "n1"}},
		{"sync", cache.Event{Type: "sync", Action: "sync", ID: ""}},
	}
	for _, tc := range events {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				match(tc.e)
			}
		})
	}
}

func BenchmarkResourceMatcher(b *testing.B) {
	taskForService := cache.Event{
		Type: "task", Action: "update", ID: "t1",
		Resource: swarm.Task{ServiceID: "svc-1", NodeID: "node-2"},
	}
	taskForNode := cache.Event{
		Type: "task", Action: "update", ID: "t1",
		Resource: swarm.Task{ServiceID: "svc-other", NodeID: "node-1"},
	}
	taskMiss := cache.Event{
		Type: "task", Action: "update", ID: "t1",
		Resource: swarm.Task{ServiceID: "svc-other", NodeID: "node-other"},
	}

	b.Run("service/direct_hit", func(b *testing.B) {
		match := resourceMatcher("service", "svc-1")
		e := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
		for b.Loop() {
			match(e)
		}
	})
	b.Run("service/task_hit", func(b *testing.B) {
		match := resourceMatcher("service", "svc-1")
		for b.Loop() {
			match(taskForService)
		}
	})
	b.Run("service/task_miss", func(b *testing.B) {
		match := resourceMatcher("service", "svc-1")
		for b.Loop() {
			match(taskMiss)
		}
	})
	b.Run("node/direct_hit", func(b *testing.B) {
		match := resourceMatcher("node", "node-1")
		e := cache.Event{Type: "node", Action: "update", ID: "node-1"}
		for b.Loop() {
			match(e)
		}
	})
	b.Run("node/task_hit", func(b *testing.B) {
		match := resourceMatcher("node", "node-1")
		for b.Loop() {
			match(taskForNode)
		}
	})
	b.Run("config/hit", func(b *testing.B) {
		match := resourceMatcher("config", "cfg-1")
		e := cache.Event{Type: "config", Action: "update", ID: "cfg-1"}
		for b.Loop() {
			match(e)
		}
	})
	b.Run("config/miss", func(b *testing.B) {
		match := resourceMatcher("config", "cfg-1")
		e := cache.Event{Type: "config", Action: "update", ID: "cfg-2"}
		for b.Loop() {
			match(e)
		}
	})
}

func BenchmarkStackMatcher(b *testing.B) {
	c := cache.New(nil)
	populateCache(c, 100) // 5 stacks with 20 services each
	match := stackMatcher(c, "stack-0")

	b.Run("service_hit", func(b *testing.B) {
		e := cache.Event{Type: "service", Action: "update", ID: "id-0"}
		for b.Loop() {
			match(e)
		}
	})
	b.Run("service_miss", func(b *testing.B) {
		e := cache.Event{Type: "service", Action: "update", ID: "id-1"}
		for b.Loop() {
			match(e)
		}
	})
	b.Run("task_hit", func(b *testing.B) {
		e := cache.Event{
			Type:     "task",
			Action:   "update",
			ID:       "t1",
			Resource: swarm.Task{ServiceID: "id-0"},
		}
		for b.Loop() {
			match(e)
		}
	})
	b.Run("sync", func(b *testing.B) {
		e := cache.Event{Type: "sync", Action: "sync"}
		for b.Loop() {
			match(e)
		}
	})
}

// =============================================================================
// SSE broadcast
// =============================================================================

func BenchmarkBroadcast(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0)
			defer br.Close()

			for range nClients {
				client := &sseClient{
					events: make(chan cache.Event, 64),
					done:   make(chan struct{}),
				}
				br.clients[client] = struct{}{}
				go func(c *sseClient) {
					for {
						select {
						case <-c.done:
							return
						case <-c.events:
						}
					}
				}(client)
			}

			event := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

func BenchmarkBroadcastWithPayload(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0)
			defer br.Close()

			for range nClients {
				client := &sseClient{
					events: make(chan cache.Event, 64),
					done:   make(chan struct{}),
				}
				br.clients[client] = struct{}{}
				go func(c *sseClient) {
					for {
						select {
						case <-c.done:
							return
						case <-c.events:
						}
					}
				}(client)
			}

			event := realisticServiceEvent()
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

func BenchmarkBroadcastWithFiltering(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0)
			defer br.Close()

			// Half the clients filter for "service", half for "node".
			// A service event should reach only half.
			for i := range nClients {
				var match func(cache.Event) bool
				if i%2 == 0 {
					match = typeMatcher("service")
				} else {
					match = typeMatcher("node")
				}
				client := &sseClient{
					events: make(chan cache.Event, 64),
					match:  match,
					done:   make(chan struct{}),
				}
				br.clients[client] = struct{}{}
				go func(c *sseClient) {
					for {
						select {
						case <-c.done:
							return
						case <-c.events:
						}
					}
				}(client)
			}

			event := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

func BenchmarkBroadcastWithResourceMatcher(b *testing.B) {
	for _, nClients := range []int{10, 100} {
		b.Run(fmt.Sprintf("clients=%d", nClients), func(b *testing.B) {
			br := NewBroadcaster(0)
			defer br.Close()

			// Each client watches a different resource — simulates per-detail-page SSE.
			for i := range nClients {
				match := resourceMatcher("service", fmt.Sprintf("svc-%d", i))
				client := &sseClient{
					events: make(chan cache.Event, 64),
					match:  match,
					done:   make(chan struct{}),
				}
				br.clients[client] = struct{}{}
				go func(c *sseClient) {
					for {
						select {
						case <-c.done:
							return
						case <-c.events:
						}
					}
				}(client)
			}

			// Only one client should match this event.
			event := cache.Event{
				Type: "task", Action: "update", ID: "t1",
				Resource: swarm.Task{ServiceID: "svc-0"},
			}
			b.ResetTimer()
			for b.Loop() {
				br.Broadcast(event)
			}
		})
	}
}

// =============================================================================
// SSE client registration churn
// =============================================================================

func BenchmarkClientRegistration(b *testing.B) {
	br := NewBroadcaster(0)
	defer br.Close()

	// Keep a steady-state client so fanOut stays busy.
	steady := &sseClient{
		events: make(chan cache.Event, 64),
		done:   make(chan struct{}),
	}
	br.mu.Lock()
	br.clients[steady] = struct{}{}
	br.mu.Unlock()
	go func() {
		for {
			select {
			case <-steady.done:
				return
			case <-steady.events:
			}
		}
	}()

	// Feed events at a controlled rate so the inbox doesn't overflow.
	ctx := b.Context()
	go func() {
		e := cache.Event{Type: "service", Action: "update", ID: "svc-1"}
		ticker := time.NewTicker(100 * time.Microsecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				br.Broadcast(e)
			}
		}
	}()

	b.ResetTimer()
	for b.Loop() {
		client := &sseClient{
			events: make(chan cache.Event, 64),
			match:  typeMatcher("service"),
			done:   make(chan struct{}),
		}
		br.mu.Lock()
		br.clients[client] = struct{}{}
		br.mu.Unlock()

		br.mu.Lock()
		delete(br.clients, client)
		br.mu.Unlock()
	}
}

// =============================================================================
// Full SSE HTTP path
// =============================================================================

func BenchmarkServeSSE(b *testing.B) {
	for _, nEvents := range []int{1, 10} {
		b.Run(fmt.Sprintf("events=%d", nEvents), func(b *testing.B) {
			for b.Loop() {
				br := NewBroadcaster(time.Millisecond) // fast flush

				ctx, cancel := context.WithCancel(context.Background())
				req := httptest.NewRequestWithContext(ctx, "GET", "/events", nil)
				w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

				var wg sync.WaitGroup
				wg.Go(func() {
					br.ServeHTTP(w, req)
				})

				// Wait for client registration.
				for {
					br.mu.RLock()
					n := len(br.clients)
					br.mu.RUnlock()
					if n > 0 {
						break
					}
					time.Sleep(time.Microsecond)
				}

				for i := range nEvents {
					br.Broadcast(cache.Event{
						Type:     "service",
						Action:   "update",
						ID:       fmt.Sprintf("svc-%d", i),
						Resource: realisticServiceEvent().Resource,
					})
				}

				// Give the batch ticker time to flush.
				time.Sleep(2 * time.Millisecond)
				cancel()
				wg.Wait()
				br.Close()
			}
		})
	}
}

func BenchmarkServeSSEFiltered(b *testing.B) {
	for b.Loop() {
		br := NewBroadcaster(time.Millisecond)

		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequestWithContext(ctx, "GET", "/events?types=service", nil)
		w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}

		var wg sync.WaitGroup
		wg.Go(func() {
			br.ServeHTTP(w, req)
		})

		for {
			br.mu.RLock()
			n := len(br.clients)
			br.mu.RUnlock()
			if n > 0 {
				break
			}
			time.Sleep(time.Microsecond)
		}

		// Send mix of types — only service events should be written.
		for i := range 5 {
			if i%2 == 0 {
				br.Broadcast(
					cache.Event{Type: "service", Action: "update", ID: fmt.Sprintf("svc-%d", i)},
				)
			} else {
				br.Broadcast(
					cache.Event{Type: "node", Action: "update", ID: fmt.Sprintf("n-%d", i)},
				)
			}
		}

		time.Sleep(2 * time.Millisecond)
		cancel()
		wg.Wait()
		br.Close()
	}
}

// =============================================================================
// Parallel HTTP benchmarks (contention under load)
// =============================================================================

func BenchmarkHandleListNodesParallel(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes", nil)
					w := httptest.NewRecorder()
					h.HandleListNodes(w, req)
					if w.Code != http.StatusOK {
						b.Fatalf("expected 200, got %d", w.Code)
					}
				}
			})
		})
	}
}

func BenchmarkHandleListServicesParallel(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequestWithContext(b.Context(), "GET", "/services", nil)
					h.HandleListServices(httptest.NewRecorder(), req)
				}
			})
		})
	}
}

func BenchmarkHandleSearchParallel(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequestWithContext(b.Context(), "GET", "/search?q=svc", nil)
					h.HandleSearch(httptest.NewRecorder(), req)
				}
			})
		})
	}
}

func BenchmarkHandleGetNodeParallel(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes/id-0", nil)
					req.SetPathValue("id", "id-0")
					h.HandleGetNode(httptest.NewRecorder(), req)
				}
			})
		})
	}
}

func BenchmarkHandleNetworkTopologyParallel(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					req := httptest.NewRequestWithContext(
						b.Context(),
						"GET",
						"/topology/networks",
						nil,
					)
					h.HandleNetworkTopology(httptest.NewRecorder(), req)
				}
			})
		})
	}
}

// BenchmarkMixedWorkloadParallel simulates realistic concurrent access patterns:
// list, detail, search, and topology requests interleaved.
func BenchmarkMixedWorkloadParallel(b *testing.B) {
	for _, n := range handlerSizes {
		c := cache.New(nil)
		populateCache(c, n)
		h := NewHandlers(c, nil, nil, nil, nil, closedReady(), nil, config.OpsImpactful)
		b.Run(fmt.Sprintf("size=%d", n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					switch i % 5 {
					case 0:
						req := httptest.NewRequestWithContext(b.Context(), "GET", "/nodes", nil)
						h.HandleListNodes(httptest.NewRecorder(), req)
					case 1:
						req := httptest.NewRequestWithContext(
							b.Context(),
							"GET",
							"/services/id-0",
							nil,
						)
						req.SetPathValue("id", "id-0")
						h.HandleGetService(httptest.NewRecorder(), req)
					case 2:
						req := httptest.NewRequestWithContext(
							b.Context(),
							"GET",
							"/search?q=svc",
							nil,
						)
						h.HandleSearch(httptest.NewRecorder(), req)
					case 3:
						req := httptest.NewRequestWithContext(
							b.Context(),
							"GET",
							"/stacks/stack-0",
							nil,
						)
						req.SetPathValue("name", "stack-0")
						h.HandleGetStack(httptest.NewRecorder(), req)
					case 4:
						req := httptest.NewRequestWithContext(
							b.Context(),
							"GET",
							"/topology/networks",
							nil,
						)
						h.HandleNetworkTopology(httptest.NewRecorder(), req)
					}
					i++
				}
			})
		})
	}
}
