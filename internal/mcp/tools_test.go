package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// fakeWriteClient is a no-op implementation of DockerWriteClient used as a base
// for table-driven tool tests. Individual tests override the specific method
// they exercise so unrelated tools can panic if they're ever called.
type fakeWriteClient struct {
	scaleServiceFn        func(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	updateServiceImageFn  func(ctx context.Context, id, image string) (swarm.Service, error)
	rollbackServiceFn     func(ctx context.Context, id string) (swarm.Service, error)
	restartServiceFn      func(ctx context.Context, id string) (swarm.Service, error)
	removeServiceFn       func(ctx context.Context, id string) error
	updateServiceEnvFn    func(ctx context.Context, id string, env map[string]string) (swarm.Service, error)
	updateServiceLabelsFn func(ctx context.Context, id string, labels map[string]string) (swarm.Service, error)
	updateNodeLabelsFn    func(ctx context.Context, id string, labels map[string]string) (swarm.Node, error)
	updateNodeAvailFn     func(ctx context.Context, id string, a swarm.NodeAvailability) (swarm.Node, error)
	updateNodeRoleFn      func(ctx context.Context, id string, r swarm.NodeRole) (swarm.Node, error)
	removeTaskFn          func(ctx context.Context, id string) error
	removeConfigFn        func(ctx context.Context, id string) error
	removeSecretFn        func(ctx context.Context, id string) error
	removeNetworkFn       func(ctx context.Context, id string) error
	removeVolumeFn        func(ctx context.Context, name string, force bool) error
	updateServiceResFn    func(ctx context.Context, id string, r *swarm.ResourceRequirements) (swarm.Service, error)
	updateServicePlaceFn  func(ctx context.Context, id string, p *swarm.Placement) (swarm.Service, error)
	updateServicePortsFn  func(ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error)
	updateServiceUpdateFn func(ctx context.Context, id string, p *swarm.UpdateConfig) (swarm.Service, error)
	updateServiceRbackFn  func(ctx context.Context, id string, p *swarm.UpdateConfig) (swarm.Service, error)
	updateServiceLogDrvFn func(ctx context.Context, id string, d *swarm.Driver) (swarm.Service, error)
}

var errNotImplemented = errors.New("fakeWriteClient: method not stubbed")

func (f *fakeWriteClient) ScaleService(
	ctx context.Context,
	id string,
	replicas uint64,
) (swarm.Service, error) {
	if f.scaleServiceFn != nil {
		return f.scaleServiceFn(ctx, id, replicas)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServiceImage(
	ctx context.Context,
	id, image string,
) (swarm.Service, error) {
	if f.updateServiceImageFn != nil {
		return f.updateServiceImageFn(ctx, id, image)
	}
	return swarm.Service{}, errNotImplemented
}
func (f *fakeWriteClient) RollbackService(ctx context.Context, id string) (swarm.Service, error) {
	if f.rollbackServiceFn != nil {
		return f.rollbackServiceFn(ctx, id)
	}
	return swarm.Service{}, errNotImplemented
}
func (f *fakeWriteClient) RestartService(ctx context.Context, id string) (swarm.Service, error) {
	if f.restartServiceFn != nil {
		return f.restartServiceFn(ctx, id)
	}
	return swarm.Service{}, errNotImplemented
}
func (f *fakeWriteClient) RemoveService(ctx context.Context, id string) error {
	if f.removeServiceFn != nil {
		return f.removeServiceFn(ctx, id)
	}
	return errNotImplemented
}

func (f *fakeWriteClient) UpdateServiceEnv(
	ctx context.Context,
	id string,
	env map[string]string,
) (swarm.Service, error) {
	if f.updateServiceEnvFn != nil {
		return f.updateServiceEnvFn(ctx, id, env)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServiceLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Service, error) {
	if f.updateServiceLabelsFn != nil {
		return f.updateServiceLabelsFn(ctx, id, labels)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServiceResources(
	ctx context.Context,
	id string,
	r *swarm.ResourceRequirements,
) (swarm.Service, error) {
	if f.updateServiceResFn != nil {
		return f.updateServiceResFn(ctx, id, r)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServicePlacement(
	ctx context.Context,
	id string,
	p *swarm.Placement,
) (swarm.Service, error) {
	if f.updateServicePlaceFn != nil {
		return f.updateServicePlaceFn(ctx, id, p)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServicePorts(
	ctx context.Context,
	id string,
	ports []swarm.PortConfig,
) (swarm.Service, error) {
	if f.updateServicePortsFn != nil {
		return f.updateServicePortsFn(ctx, id, ports)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServiceUpdatePolicy(
	ctx context.Context,
	id string,
	p *swarm.UpdateConfig,
) (swarm.Service, error) {
	if f.updateServiceUpdateFn != nil {
		return f.updateServiceUpdateFn(ctx, id, p)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServiceRollbackPolicy(
	ctx context.Context,
	id string,
	p *swarm.UpdateConfig,
) (swarm.Service, error) {
	if f.updateServiceRbackFn != nil {
		return f.updateServiceRbackFn(ctx, id, p)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateServiceLogDriver(
	ctx context.Context,
	id string,
	d *swarm.Driver,
) (swarm.Service, error) {
	if f.updateServiceLogDrvFn != nil {
		return f.updateServiceLogDrvFn(ctx, id, d)
	}
	return swarm.Service{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateNodeAvailability(
	ctx context.Context,
	id string,
	a swarm.NodeAvailability,
) (swarm.Node, error) {
	if f.updateNodeAvailFn != nil {
		return f.updateNodeAvailFn(ctx, id, a)
	}
	return swarm.Node{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateNodeLabels(
	ctx context.Context,
	id string,
	labels map[string]string,
) (swarm.Node, error) {
	if f.updateNodeLabelsFn != nil {
		return f.updateNodeLabelsFn(ctx, id, labels)
	}
	return swarm.Node{}, errNotImplemented
}

func (f *fakeWriteClient) UpdateNodeRole(
	ctx context.Context,
	id string,
	r swarm.NodeRole,
) (swarm.Node, error) {
	if f.updateNodeRoleFn != nil {
		return f.updateNodeRoleFn(ctx, id, r)
	}
	return swarm.Node{}, errNotImplemented
}
func (f *fakeWriteClient) RemoveTask(ctx context.Context, id string) error {
	if f.removeTaskFn != nil {
		return f.removeTaskFn(ctx, id)
	}
	return errNotImplemented
}
func (f *fakeWriteClient) RemoveConfig(ctx context.Context, id string) error {
	if f.removeConfigFn != nil {
		return f.removeConfigFn(ctx, id)
	}
	return errNotImplemented
}
func (f *fakeWriteClient) RemoveSecret(ctx context.Context, id string) error {
	if f.removeSecretFn != nil {
		return f.removeSecretFn(ctx, id)
	}
	return errNotImplemented
}
func (f *fakeWriteClient) RemoveNetwork(ctx context.Context, id string) error {
	if f.removeNetworkFn != nil {
		return f.removeNetworkFn(ctx, id)
	}
	return errNotImplemented
}
func (f *fakeWriteClient) RemoveVolume(ctx context.Context, name string, force bool) error {
	if f.removeVolumeFn != nil {
		return f.removeVolumeFn(ctx, name, force)
	}
	return errNotImplemented
}

func newToolTestServer(
	t *testing.T,
	c *cache.Cache,
	wc DockerWriteClient,
	opsLevel config.OperationsLevel,
) *Server {
	t.Helper()
	cfg := config.DefaultMCPConfig()
	cfg.Enabled = true
	srv, err := New(c, Options{
		Config:         cfg,
		GlobalOpsLevel: opsLevel,
		WriteClient:    wc,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

// findTool returns the toolDef registered under name. Tests use this to invoke
// handlers directly with constructed CallToolRequests, skipping mcp-go's
// transport layer (covered separately in the integration test in Task 13).
func (s *Server) findTool(name string) (toolDef, bool) {
	for _, td := range s.registeredTools {
		if td.tool.Name == name {
			return td, true
		}
	}
	return toolDef{}, false
}

func newCallToolRequest(name string, args map[string]any) mcplib.CallToolRequest {
	return mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
}

func TestTierThreeNodeToolsCarryDestructiveHint(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsImpactful)

	for _, name := range []string{"update_node_availability", "update_node_role"} {
		td, ok := srv.findTool(name)
		if !ok {
			t.Fatalf("%s should be registered at OpsImpactful", name)
		}
		hint := td.tool.Annotations.DestructiveHint
		if hint == nil || !*hint {
			t.Errorf("%s: destructiveHint = %v, want true", name, hint)
		}
	}
}

func TestToolCatalogTierFilter(t *testing.T) {
	c := cache.New(nil)
	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsReadOnly)

	if _, ok := srv.findTool("scale_service"); ok {
		t.Error("scale_service should not be registered at OpsReadOnly")
	}
	if _, ok := srv.findTool("search"); !ok {
		t.Error("search (read tool) should be registered at OpsReadOnly")
	}

	srv = newToolTestServer(t, c, &fakeWriteClient{}, config.OpsOperational)
	if _, ok := srv.findTool("scale_service"); !ok {
		t.Error("scale_service should be registered at OpsOperational")
	}
	if _, ok := srv.findTool("update_service_env"); ok {
		t.Error("update_service_env (tier 2) should not be registered at OpsOperational")
	}
	if _, ok := srv.findTool("remove_service"); ok {
		t.Error("remove_service (tier 3) should not be registered at OpsOperational")
	}

	srv = newToolTestServer(t, c, &fakeWriteClient{}, config.OpsImpactful)
	for _, name := range []string{"scale_service", "update_service_env", "remove_service", "remove_volume"} {
		if _, ok := srv.findTool(name); !ok {
			t.Errorf("%s should be registered at OpsImpactful", name)
		}
	}
}

func TestToolScaleService(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	var calledWith uint64
	wc := &fakeWriteClient{
		scaleServiceFn: func(_ context.Context, _ string, replicas uint64) (swarm.Service, error) {
			calledWith = replicas
			return swarm.Service{ID: "svc1"}, nil
		},
	}
	srv := newToolTestServer(t, c, wc, config.OpsOperational)

	td, ok := srv.findTool("scale_service")
	if !ok {
		t.Fatal("scale_service not registered")
	}
	out, err := td.handler(context.Background(), newCallToolRequest("scale_service", map[string]any{
		"id":       "svc1",
		"replicas": float64(5),
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if calledWith != 5 {
		t.Errorf("scaled to %d, want 5", calledWith)
	}
	if !strings.Contains(out, `"ID":"svc1"`) {
		t.Errorf("output missing service ID: %s", out)
	}
}

func TestToolScaleServiceRejectsNegativeReplicas(t *testing.T) {
	c := cache.New(nil)
	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsOperational)
	td, _ := srv.findTool("scale_service")

	_, err := td.handler(context.Background(), newCallToolRequest("scale_service", map[string]any{
		"id":       "svc1",
		"replicas": float64(-1),
	}))
	if err == nil {
		t.Fatal("expected error for negative replicas")
	}
}

func TestToolScaleServiceMissingArgs(t *testing.T) {
	c := cache.New(nil)
	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsOperational)
	td, _ := srv.findTool("scale_service")

	_, err := td.handler(context.Background(), newCallToolRequest("scale_service", map[string]any{
		"id": "svc1",
	}))
	if err == nil {
		t.Fatal("expected error when replicas missing")
	}
}

func TestToolSearch(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web-frontend"}},
	})

	srv := newToolTestServer(t, c, nil, config.OpsReadOnly)
	td, _ := srv.findTool("search")

	out, err := td.handler(context.Background(), newCallToolRequest("search", map[string]any{
		"query": "web",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(out, "web-frontend") {
		t.Errorf("search output missing match: %s", out)
	}
}

func TestToolSearchRejectsEmptyQuery(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), nil, config.OpsReadOnly)
	td, _ := srv.findTool("search")

	for _, q := range []string{"", "   ", "\t\n"} {
		_, err := td.handler(context.Background(), newCallToolRequest("search", map[string]any{
			"query": q,
		}))
		if err == nil {
			t.Errorf("query %q: expected error, got nil", q)
		}
	}
}

func TestToolRemoveTask(t *testing.T) {
	c := cache.New(nil)
	var removed string
	wc := &fakeWriteClient{
		removeTaskFn: func(_ context.Context, id string) error {
			removed = id
			return nil
		},
	}
	srv := newToolTestServer(t, c, wc, config.OpsOperational)
	td, _ := srv.findTool("remove_task")

	out, err := td.handler(context.Background(), newCallToolRequest("remove_task", map[string]any{
		"id": "task1",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if removed != "task1" {
		t.Errorf("removed = %q, want task1", removed)
	}
	if !strings.Contains(out, `"removed":true`) {
		t.Errorf("output unexpected: %s", out)
	}
}

func TestToolUpdateServiceEnv(t *testing.T) {
	c := cache.New(nil)
	var got map[string]string
	wc := &fakeWriteClient{
		updateServiceEnvFn: func(_ context.Context, _ string, env map[string]string) (swarm.Service, error) {
			got = env
			return swarm.Service{}, nil
		},
	}
	srv := newToolTestServer(t, c, wc, config.OpsConfiguration)
	td, _ := srv.findTool("update_service_env")

	_, err := td.handler(
		context.Background(),
		newCallToolRequest("update_service_env", map[string]any{
			"id":  "svc1",
			"env": map[string]any{"DEBUG": "true", "PORT": "8080"},
		}),
	)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got["DEBUG"] != "true" || got["PORT"] != "8080" {
		t.Errorf("got env = %v", got)
	}
}

func TestToolUpdateServiceEnvRejectsNonStringValues(t *testing.T) {
	c := cache.New(nil)
	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsConfiguration)
	td, _ := srv.findTool("update_service_env")

	_, err := td.handler(
		context.Background(),
		newCallToolRequest("update_service_env", map[string]any{
			"id":  "svc1",
			"env": map[string]any{"PORT": float64(8080)},
		}),
	)
	if err == nil {
		t.Fatal("expected error for non-string env value")
	}
}

func TestToolUpdateNodeAvailability(t *testing.T) {
	c := cache.New(nil)
	var got swarm.NodeAvailability
	wc := &fakeWriteClient{
		updateNodeAvailFn: func(_ context.Context, _ string, a swarm.NodeAvailability) (swarm.Node, error) {
			got = a
			return swarm.Node{}, nil
		},
	}
	srv := newToolTestServer(t, c, wc, config.OpsImpactful)
	td, _ := srv.findTool("update_node_availability")

	_, err := td.handler(
		context.Background(),
		newCallToolRequest("update_node_availability", map[string]any{
			"id":           "node1",
			"availability": "drain",
		}),
	)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got != swarm.NodeAvailabilityDrain {
		t.Errorf("availability = %q, want drain", got)
	}
}

func TestToolUpdateNodeAvailabilityRejectsInvalid(t *testing.T) {
	c := cache.New(nil)
	srv := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsImpactful)
	td, _ := srv.findTool("update_node_availability")

	_, err := td.handler(
		context.Background(),
		newCallToolRequest("update_node_availability", map[string]any{
			"id":           "node1",
			"availability": "bogus",
		}),
	)
	if err == nil {
		t.Fatal("expected error for invalid availability")
	}
}

func TestToolUpdateServiceResourcesRoundtripsJSON(t *testing.T) {
	c := cache.New(nil)
	var got *swarm.ResourceRequirements
	wc := &fakeWriteClient{
		updateServiceResFn: func(_ context.Context, _ string, r *swarm.ResourceRequirements) (swarm.Service, error) {
			got = r
			return swarm.Service{}, nil
		},
	}
	srv := newToolTestServer(t, c, wc, config.OpsConfiguration)
	td, _ := srv.findTool("update_service_resources")

	_, err := td.handler(
		context.Background(),
		newCallToolRequest("update_service_resources", map[string]any{
			"id": "svc1",
			"resources": map[string]any{
				"Limits": map[string]any{
					"NanoCPUs":    float64(500_000_000),
					"MemoryBytes": float64(256 * 1024 * 1024),
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got == nil || got.Limits == nil {
		t.Fatal("resources not decoded")
	}
	if got.Limits.NanoCPUs != 500_000_000 {
		t.Errorf("NanoCPUs = %d, want 500_000_000", got.Limits.NanoCPUs)
	}
}

func TestToolRequireWriteClient(t *testing.T) {
	c := cache.New(nil)
	srv := newToolTestServer(t, c, nil, config.OpsOperational)
	td, _ := srv.findTool("scale_service")

	_, err := td.handler(context.Background(), newCallToolRequest("scale_service", map[string]any{
		"id":       "svc1",
		"replicas": float64(2),
	}))
	if err == nil || !strings.Contains(err.Error(), "no write client") {
		t.Fatalf("expected no-write-client error, got %v", err)
	}
}

func TestToolPropagatesWriteClientError(t *testing.T) {
	c := cache.New(nil)
	wc := &fakeWriteClient{
		scaleServiceFn: func(context.Context, string, uint64) (swarm.Service, error) {
			return swarm.Service{}, fmt.Errorf("boom")
		},
	}
	srv := newToolTestServer(t, c, wc, config.OpsOperational)
	td, _ := srv.findTool("scale_service")

	_, err := td.handler(context.Background(), newCallToolRequest("scale_service", map[string]any{
		"id":       "svc1",
		"replicas": float64(1),
	}))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestToolListRegisteredCountMatchesTier(t *testing.T) {
	c := cache.New(nil)
	srvT1 := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsOperational)
	srvT3 := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsImpactful)

	if len(srvT3.registeredTools) <= len(srvT1.registeredTools) {
		t.Errorf("T3 registered %d tools, T1 registered %d — expected T3 strictly greater",
			len(srvT3.registeredTools), len(srvT1.registeredTools))
	}
}

// Sanity check on JSON shape — ensures we don't accidentally double-encode.
func TestToolMarshalResultIsValidJSON(t *testing.T) {
	out, err := marshalResult(swarm.Service{ID: "svc1"})
	if err != nil {
		t.Fatalf("marshalResult: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
}
