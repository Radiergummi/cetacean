package api

import (
	"context"
	json "github.com/goccy/go-json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/errdefs"

	"github.com/radiergummi/cetacean/internal/cache"
)

type mockWriteClient struct {
	scaleServiceFn             func(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	updateServiceImageFn       func(ctx context.Context, id string, image string) (swarm.Service, error)
	rollbackServiceFn          func(ctx context.Context, id string) (swarm.Service, error)
	restartServiceFn           func(ctx context.Context, id string) (swarm.Service, error)
	updateNodeAvailabilityFn   func(ctx context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error)
	removeTaskFn               func(ctx context.Context, id string) error
	updateServiceEnvFn         func(ctx context.Context, id string, env map[string]string) (swarm.Service, error)
	updateNodeLabelsFn         func(ctx context.Context, id string, labels map[string]string) (swarm.Node, error)
	updateServiceLabelsFn      func(ctx context.Context, id string, labels map[string]string) (swarm.Service, error)
	updateServiceResourcesFn   func(ctx context.Context, id string, resources *swarm.ResourceRequirements) (swarm.Service, error)
	updateServiceModeFn            func(ctx context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error)
	updateServiceEndpointModeFn    func(ctx context.Context, id string, mode swarm.ResolutionMode) (swarm.Service, error)
	updateServiceHealthcheckFn     func(ctx context.Context, id string, hc *container.HealthConfig) (swarm.Service, error)
}

func (m *mockWriteClient) ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
	if m.scaleServiceFn != nil {
		return m.scaleServiceFn(ctx, id, replicas)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error) {
	if m.updateServiceImageFn != nil {
		return m.updateServiceImageFn(ctx, id, image)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) RollbackService(ctx context.Context, id string) (swarm.Service, error) {
	if m.rollbackServiceFn != nil {
		return m.rollbackServiceFn(ctx, id)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) RestartService(ctx context.Context, id string) (swarm.Service, error) {
	if m.restartServiceFn != nil {
		return m.restartServiceFn(ctx, id)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateNodeAvailability(ctx context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error) {
	if m.updateNodeAvailabilityFn != nil {
		return m.updateNodeAvailabilityFn(ctx, id, availability)
	}
	return swarm.Node{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) RemoveTask(ctx context.Context, id string) error {
	if m.removeTaskFn != nil {
		return m.removeTaskFn(ctx, id)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceEnv(ctx context.Context, id string, env map[string]string) (swarm.Service, error) {
	if m.updateServiceEnvFn != nil {
		return m.updateServiceEnvFn(ctx, id, env)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateNodeLabels(ctx context.Context, id string, labels map[string]string) (swarm.Node, error) {
	if m.updateNodeLabelsFn != nil {
		return m.updateNodeLabelsFn(ctx, id, labels)
	}
	return swarm.Node{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceLabels(ctx context.Context, id string, labels map[string]string) (swarm.Service, error) {
	if m.updateServiceLabelsFn != nil {
		return m.updateServiceLabelsFn(ctx, id, labels)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceResources(ctx context.Context, id string, resources *swarm.ResourceRequirements) (swarm.Service, error) {
	if m.updateServiceResourcesFn != nil {
		return m.updateServiceResourcesFn(ctx, id, resources)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceEndpointMode(ctx context.Context, id string, mode swarm.ResolutionMode) (swarm.Service, error) {
	if m.updateServiceEndpointModeFn != nil {
		return m.updateServiceEndpointModeFn(ctx, id, mode)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceMode(ctx context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error) {
	if m.updateServiceModeFn != nil {
		return m.updateServiceModeFn(ctx, id, mode)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func (m *mockWriteClient) UpdateServiceHealthcheck(ctx context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
	if m.updateServiceHealthcheckFn != nil {
		return m.updateServiceHealthcheckFn(ctx, id, hc)
	}
	return swarm.Service{}, fmt.Errorf("not implemented")
}

func replicatedService(id string) swarm.Service {
	replicas := uint64(1)
	return swarm.Service{
		ID: id,
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	}
}

func TestHandleScaleService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		scaleServiceFn: func(_ context.Context, id string, replicas uint64) (swarm.Service, error) {
			svc := replicatedService(id)
			svc.Spec.Mode.Replicated.Replicas = &replicas
			return svc, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"replicas":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleScaleService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"replicas":2}`
	req := httptest.NewRequest("PUT", "/services/missing/scale", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleScaleService_GlobalMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svcglobal",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Global: &swarm.GlobalService{},
			},
		},
	})
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"replicas":2}`
	req := httptest.NewRequest("PUT", "/services/svcglobal/scale", strings.NewReader(body))
	req.SetPathValue("id", "svcglobal")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleScaleService_Conflict(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(3)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		},
	})

	wc := &mockWriteClient{
		scaleServiceFn: func(_ context.Context, _ string, _ uint64) (swarm.Service, error) {
			return swarm.Service{}, errdefs.Conflict(fmt.Errorf("update out of sequence"))
		},
	}

	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)
	body := `{"replicas": 5}`
	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}
}

func TestHandleScaleService_InvalidBody(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("PUT", "/services/svc1/scale", strings.NewReader("not json"))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleScaleService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceMode_ToGlobal(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		updateServiceModeFn: func(_ context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error) {
			return swarm.Service{
				ID:   id,
				Spec: swarm.ServiceSpec{Mode: mode},
			}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"mode":"global"}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateServiceMode_ToReplicated(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}}},
	})

	wc := &mockWriteClient{
		updateServiceModeFn: func(_ context.Context, id string, mode swarm.ServiceMode) (swarm.Service, error) {
			return swarm.Service{
				ID:   id,
				Spec: swarm.ServiceSpec{Mode: mode},
			}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"mode":"replicated","replicas":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateServiceMode_ReplicatedWithoutCount(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Mode: swarm.ServiceMode{Global: &swarm.GlobalService{}}},
	})

	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"mode":"replicated"}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceMode_InvalidMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"mode":"invalid"}`
	req := httptest.NewRequest("PUT", "/services/svc1/mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceMode_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"mode":"global"}`
	req := httptest.NewRequest("PUT", "/services/missing/mode", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceMode(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateServiceEndpointMode_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		updateServiceEndpointModeFn: func(_ context.Context, id string, mode swarm.ResolutionMode) (swarm.Service, error) {
			svc := replicatedService(id)
			svc.Spec.EndpointSpec = &swarm.EndpointSpec{Mode: mode}
			return svc, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"mode":"dnsrr"}`
	req := httptest.NewRequest("PUT", "/services/svc1/endpoint-mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceEndpointMode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateServiceEndpointMode_InvalidMode(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"mode":"invalid"}`
	req := httptest.NewRequest("PUT", "/services/svc1/endpoint-mode", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceEndpointMode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleUpdateServiceImage_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		updateServiceImageFn: func(_ context.Context, id string, image string) (swarm.Service, error) {
			svc := replicatedService(id)
			svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{Image: image}
			return svc, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"image":"nginx:latest"}`
	req := httptest.NewRequest("PUT", "/services/svc1/image", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceImage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleUpdateServiceImage_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"image":"nginx:latest"}`
	req := httptest.NewRequest("PUT", "/services/missing/image", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceImage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateServiceImage_EmptyImage(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"image":""}`
	req := httptest.NewRequest("PUT", "/services/svc1/image", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleUpdateServiceImage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func serviceWithPreviousSpec(id string) swarm.Service {
	spec := swarm.ServiceSpec{}
	svc := replicatedService(id)
	svc.PreviousSpec = &spec
	return svc
}

func TestHandleRollbackService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithPreviousSpec("svc1"))

	wc := &mockWriteClient{
		rollbackServiceFn: func(_ context.Context, id string) (swarm.Service, error) {
			return replicatedService(id), nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("POST", "/services/svc1/rollback", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRollbackService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleRollbackService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("POST", "/services/missing/rollback", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRollbackService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRollbackService_NoPreviousSpec(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("POST", "/services/svc1/rollback", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRollbackService(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleRestartService_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		restartServiceFn: func(_ context.Context, id string) (swarm.Service, error) {
			return replicatedService(id), nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("POST", "/services/svc1/restart", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleRestartService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Service" {
		t.Errorf("@type=%v, want Service", resp["@type"])
	}
}

func TestHandleRestartService_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("POST", "/services/missing/restart", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRestartService(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateNodeAvailability_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})

	wc := &mockWriteClient{
		updateNodeAvailabilityFn: func(_ context.Context, id string, availability swarm.NodeAvailability) (swarm.Node, error) {
			return swarm.Node{ID: id, Spec: swarm.NodeSpec{Availability: availability}}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"availability":"drain"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/availability", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeAvailability(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Node" {
		t.Errorf("@type=%v, want Node", resp["@type"])
	}
}

func TestHandleUpdateNodeAvailability_NotFound(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"availability":"drain"}`
	req := httptest.NewRequest("PUT", "/nodes/missing/availability", strings.NewReader(body))
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeAvailability(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleUpdateNodeAvailability_InvalidAvailability(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"availability":"invalid"}`
	req := httptest.NewRequest("PUT", "/nodes/node1/availability", strings.NewReader(body))
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleUpdateNodeAvailability(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleRemoveTask_OK(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1"})

	wc := &mockWriteClient{
		removeTaskFn: func(_ context.Context, id string) error {
			return nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	w := httptest.NewRecorder()
	h.HandleRemoveTask(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemoveTask_NotFoundInCache(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("DELETE", "/tasks/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandleRemoveTask(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleRemoveTask_NoContainer(t *testing.T) {
	c := cache.New(nil)
	c.SetTask(swarm.Task{ID: "task1"})

	wc := &mockWriteClient{
		removeTaskFn: func(_ context.Context, id string) error {
			return errdefs.NotFound(fmt.Errorf("task has no running container"))
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	req := httptest.NewRequest("DELETE", "/tasks/task1", nil)
	req.SetPathValue("id", "task1")
	w := httptest.NewRecorder()
	h.HandleRemoveTask(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func serviceWithEnv(id string, env []string) swarm.Service {
	svc := replicatedService(id)
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{Env: env}
	return svc
}

func TestHandleGetServiceEnv(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar", "BAZ=qux"}))
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/services/svc1/env", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceEnv" {
		t.Errorf("@type=%v, want ServiceEnv", resp["@type"])
	}
	envMap, ok := resp["env"].(map[string]any)
	if !ok {
		t.Fatal("expected env key in response")
	}
	if envMap["FOO"] != "bar" {
		t.Errorf("FOO=%v, want bar", envMap["FOO"])
	}
	if envMap["BAZ"] != "qux" {
		t.Errorf("BAZ=%v, want qux", envMap["BAZ"])
	}
}

func TestHandlePatchServiceEnv_Add(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar"}))

	wc := &mockWriteClient{
		updateServiceEnvFn: func(_ context.Context, id string, env map[string]string) (swarm.Service, error) {
			envSlice := make([]string, 0, len(env))
			for k, v := range env {
				envSlice = append(envSlice, k+"="+v)
			}
			return serviceWithEnv(id, envSlice), nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `[{"op":"add","path":"/NEW","value":"val"}]`
	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceEnv_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", nil))
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchServiceEnv_TestFailed(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar"}))
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	body := `[{"op":"test","path":"/FOO","value":"wrong"}]`
	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}
}

func TestHandlePatchServiceEnv_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	body := `[{"op":"add","path":"/K","value":"v"}]`
	req := httptest.NewRequest("PATCH", "/services/missing/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandlePatchServiceEnv_MergePatch(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithEnv("svc1", []string{"FOO=bar", "OLD=remove"}))

	wc := &mockWriteClient{
		updateServiceEnvFn: func(_ context.Context, id string, env map[string]string) (swarm.Service, error) {
			envSlice := make([]string, 0, len(env))
			for k, v := range env {
				envSlice = append(envSlice, k+"="+v)
			}
			return serviceWithEnv(id, envSlice), nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"NEW":"val","OLD":null}`
	req := httptest.NewRequest("PATCH", "/services/svc1/env", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetNodeLabels(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:   "node1",
		Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: map[string]string{"region": "us-east"}}},
	})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/nodes/node1/labels", nil)
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandleGetNodeLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "NodeLabels" {
		t.Errorf("@type=%v, want NodeLabels", resp["@type"])
	}
	labels, ok := resp["labels"].(map[string]any)
	if !ok {
		t.Fatal("expected labels key in response")
	}
	if labels["region"] != "us-east" {
		t.Errorf("region=%v, want us-east", labels["region"])
	}
}

func TestHandlePatchNodeLabels_Add(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:   "node1",
		Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: map[string]string{"existing": "value"}}},
	})

	wc := &mockWriteClient{
		updateNodeLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Node, error) {
			return swarm.Node{ID: id, Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: labels}}}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `[{"op":"add","path":"/new","value":"label"}]`
	req := httptest.NewRequest("PATCH", "/nodes/node1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandlePatchNodeLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchNodeLabels_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "node1"})
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("PATCH", "/nodes/node1/labels", strings.NewReader(`[]`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandlePatchNodeLabels(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func TestHandlePatchNodeLabels_MergePatch(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:   "node1",
		Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: map[string]string{"existing": "value", "remove": "me"}}},
	})

	wc := &mockWriteClient{
		updateNodeLabelsFn: func(_ context.Context, id string, labels map[string]string) (swarm.Node, error) {
			return swarm.Node{ID: id, Spec: swarm.NodeSpec{Annotations: swarm.Annotations{Labels: labels}}}, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"new":"label","remove":null}`
	req := httptest.NewRequest("PATCH", "/nodes/node1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "node1")
	w := httptest.NewRecorder()
	h.HandlePatchNodeLabels(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetServiceResources(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	svc.Spec.TaskTemplate.Resources = &swarm.ResourceRequirements{
		Limits: &swarm.Limit{NanoCPUs: 1000000000},
	}
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/services/svc1/resources", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "ServiceResources" {
		t.Errorf("@type=%v, want ServiceResources", resp["@type"])
	}
	resources, ok := resp["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources key in response")
	}
	if resources["Limits"] == nil {
		t.Error("expected Limits in resources")
	}
}

func TestHandlePatchServiceResources_Merge(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	svc.Spec.TaskTemplate.Resources = &swarm.ResourceRequirements{}
	c.SetService(svc)

	wc := &mockWriteClient{
		updateServiceResourcesFn: func(_ context.Context, id string, resources *swarm.ResourceRequirements) (swarm.Service, error) {
			s := replicatedService(id)
			s.Spec.TaskTemplate.Resources = resources
			return s, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"Limits":{"NanoCPUs":500000000}}`
	req := httptest.NewRequest("PATCH", "/services/svc1/resources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceResources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchServiceResources_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("PATCH", "/services/svc1/resources", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceResources(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status=%d, want 415", w.Code)
	}
}

func serviceWithHealthcheck(id string, hc *container.HealthConfig) swarm.Service {
	svc := replicatedService(id)
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{
		Healthcheck: hc,
	}
	return svc
}

func TestHandleGetServiceHealthcheck(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithHealthcheck("svc1", &container.HealthConfig{
		Test:     []string{"CMD", "curl", "-f", "http://localhost/"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  3,
	}))
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/services/svc1/healthcheck", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp["@type"] != "ServiceHealthcheck" {
		t.Errorf("@type=%v, want ServiceHealthcheck", resp["@type"])
	}
}

func TestHandleGetServiceHealthcheck_Nil(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("GET", "/services/svc1/healthcheck", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp["@type"] != "ServiceHealthcheck" {
		t.Errorf("@type=%v, want ServiceHealthcheck", resp["@type"])
	}
}

func TestHandlePutServiceHealthcheck(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	wc := &mockWriteClient{
		updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
			return serviceWithHealthcheck(id, hc), nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"Test":["CMD","curl","-f","http://localhost/"],"Interval":10000000000,"Timeout":5000000000,"Retries":3}`
	req := httptest.NewRequest("PUT", "/services/svc1/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if resp["@type"] != "ServiceHealthcheck" {
		t.Errorf("@type=%v, want ServiceHealthcheck", resp["@type"])
	}
}

func TestHandlePutServiceHealthcheck_Disable(t *testing.T) {
	c := cache.New(nil)
	c.SetService(replicatedService("svc1"))

	var captured *container.HealthConfig
	wc := &mockWriteClient{
		updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
			captured = hc
			return serviceWithHealthcheck(id, hc), nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"Test":["NONE"]}`
	req := httptest.NewRequest("PUT", "/services/svc1/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	if captured == nil {
		t.Fatal("expected healthcheck to be captured")
	}

	if len(captured.Test) == 0 || captured.Test[0] != "NONE" {
		t.Errorf("Test[0]=%v, want NONE", captured.Test)
	}
}

func TestHandlePatchServiceHealthcheck_Merge(t *testing.T) {
	c := cache.New(nil)
	c.SetService(serviceWithHealthcheck("svc1", &container.HealthConfig{
		Test:     []string{"CMD", "curl", "-f", "http://localhost/"},
		Interval: 10 * time.Second,
		Timeout:  3 * time.Second,
		Retries:  3,
	}))

	var captured *container.HealthConfig
	wc := &mockWriteClient{
		updateServiceHealthcheckFn: func(_ context.Context, id string, hc *container.HealthConfig) (swarm.Service, error) {
			captured = hc
			return serviceWithHealthcheck(id, hc), nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, wc, closedReady(), nil)

	body := `{"Timeout":5000000000}`
	req := httptest.NewRequest("PATCH", "/services/svc1/healthcheck", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceHealthcheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	if captured == nil {
		t.Fatal("expected healthcheck to be captured")
	}

	if captured.Timeout != 5*time.Second {
		t.Errorf("Timeout=%v, want 5s", captured.Timeout)
	}

	if len(captured.Test) == 0 || captured.Test[0] != "CMD" {
		t.Errorf("Test=%v, want [CMD curl -f http://localhost/]", captured.Test)
	}
}

func TestHandlePatchServiceHealthcheck_WrongContentType(t *testing.T) {
	c := cache.New(nil)
	svc := replicatedService("svc1")
	c.SetService(svc)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	req := httptest.NewRequest("PATCH", "/services/svc1/healthcheck", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandlePatchServiceHealthcheck(w, req)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d, want 415; body: %s", w.Code, w.Body.String())
	}
}

func TestHandlePutServiceHealthcheck_NotFound(t *testing.T) {
	c := cache.New(nil)
	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, closedReady(), nil)

	body := `{"Test":["CMD-SHELL","curl http://localhost/"]}`
	req := httptest.NewRequest("PUT", "/services/nonexistent/healthcheck", strings.NewReader(body))
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	h.HandlePutServiceHealthcheck(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body: %s", w.Code, w.Body.String())
	}
}
