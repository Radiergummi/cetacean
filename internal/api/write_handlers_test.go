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

	"github.com/radiergummi/cetacean/internal/cache"
)

type mockWriteClient struct {
	scaleServiceFn func(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
}

func (m *mockWriteClient) ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
	if m.scaleServiceFn != nil {
		return m.scaleServiceFn(ctx, id, replicas)
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
