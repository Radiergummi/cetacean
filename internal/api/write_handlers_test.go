package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
