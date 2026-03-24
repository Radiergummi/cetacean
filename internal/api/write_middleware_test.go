package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func TestRequireLevel_Allowed(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireLevel(config.OpsOperational, config.OpsImpactful)(inner)
	req := httptest.NewRequest("PUT", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler was not called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}

func TestRequireLevel_Denied(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := requireLevel(config.OpsImpactful, config.OpsOperational)(inner)
	req := httptest.NewRequest("PUT", "/nodes/abc/availability", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Error("inner handler should not be called when level is insufficient")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403", w.Code)
	}

	var p ProblemDetail
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("failed to decode problem: %v", err)
	}
	if p.Status != 403 {
		t.Errorf("problem status=%d, want 403", p.Status)
	}
}

func TestRequireLevel_ReadOnly(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := requireLevel(config.OpsOperational, config.OpsReadOnly)(inner)
	req := httptest.NewRequest("PUT", "/services/abc/scale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if called {
		t.Error("inner handler should not be called in read-only mode")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403", w.Code)
	}
}

func TestRequireLevel_ExactMatch(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := requireLevel(config.OpsImpactful, config.OpsImpactful)(inner)
	req := httptest.NewRequest("PUT", "/nodes/abc/availability", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("inner handler should be called when level exactly matches")
	}
}

func TestRequireLevel_Integration_ScaleBlockedAtLevel0(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	})

	h := NewHandlers(c, nil, nil, nil, &mockWriteClient{}, nil, closedReady(), nil, config.OpsReadOnly)
	handler := requireLevel(config.OpsOperational, config.OpsReadOnly)(h.HandleScaleService)

	body := strings.NewReader(`{"replicas": 3}`)
	req := httptest.NewRequest("PUT", "/services/svc1/scale", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403 in read-only mode", w.Code)
	}
}

func TestRequireLevel_Integration_ScaleAllowedAtLevel1(t *testing.T) {
	c := cache.New(nil)
	replicas := uint64(1)
	svc := swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	}
	c.SetService(svc)

	mock := &mockWriteClient{
		scaleServiceFn: func(ctx context.Context, id string, replicas uint64) (swarm.Service, error) {
			return svc, nil
		},
	}
	h := NewHandlers(c, nil, nil, nil, mock, nil, closedReady(), nil, config.OpsOperational)
	handler := requireLevel(config.OpsOperational, config.OpsOperational)(h.HandleScaleService)

	body := strings.NewReader(`{"replicas": 3}`)
	req := httptest.NewRequest("PUT", "/services/svc1/scale", body)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status=%d, want 200", w.Code)
	}
}
