package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/docker/docker/api/types/swarm"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"cetacean/internal/cache"
)

func TestResponsesMatchOpenAPISpec(t *testing.T) {
	specPath := "../../api/openapi.yaml"
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("failed to read openapi spec: %v", err)
	}

	// Load and validate the spec.
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("failed to load openapi spec: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("openapi spec validation failed: %v", err)
	}

	// Create a kin-openapi router to match requests to spec operations.
	specRouter, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("failed to create spec router: %v", err)
	}

	// Set up a populated cache.
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "node-1",
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
		},
		Description: swarm.NodeDescription{
			Hostname: "manager-1",
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetNode(swarm.Node{
		ID: "node-2",
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Description: swarm.NodeDescription{
			Hostname: "worker-1",
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	replicas := uint64(1)
	c.SetService(swarm.Service{
		ID: "svc-1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			Mode:        swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &replicas}},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "task-1",
		ServiceID: "svc-1",
		NodeID:    "node-1",
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	h := NewHandlers(c, nil, nil, nil, closedReady(), nil)
	b := NewBroadcaster(0)
	defer b.Close()
	noopSPA := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html></html>"))
	})
	promProxy := http.NotFoundHandler()
	router := NewRouter(h, b, promProxy, noopSPA, specBytes, nil, false)

	tests := []struct {
		name       string
		method     string
		path       string
		accept     string
		wantStatus int
		// If true, we skip response body validation (for HTML responses).
		skipBodyValidation bool
	}{
		{
			name:       "health endpoint",
			method:     "GET",
			path:       "/-/health",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "list nodes (paginated collection)",
			method:     "GET",
			path:       "/nodes",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "node detail",
			method:     "GET",
			path:       "/nodes/node-1",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "node not found",
			method:     "GET",
			path:       "/nodes/nonexistent",
			accept:     "application/json",
			wantStatus: 404,
		},
		{
			name:       "list services",
			method:     "GET",
			path:       "/services",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "list tasks",
			method:     "GET",
			path:       "/tasks",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "task detail",
			method:     "GET",
			path:       "/tasks/task-1",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "search",
			method:     "GET",
			path:       "/search?q=web",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "history",
			method:     "GET",
			path:       "/history",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "topology networks",
			method:     "GET",
			path:       "/topology/networks",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:       "topology placement",
			method:     "GET",
			path:       "/topology/placement",
			accept:     "application/json",
			wantStatus: 200,
		},
		{
			name:               "content negotiation: HTML does not return JSON",
			method:             "GET",
			path:               "/nodes",
			accept:             "text/html",
			wantStatus:         200,
			skipBodyValidation: true,
		},
		{
			name:       "paginated response with limit=1",
			method:     "GET",
			path:       "/nodes?limit=1",
			accept:     "application/json",
			wantStatus: 200,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			resp := w.Result()

			if resp.StatusCode != tc.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status=%d, want %d; body=%s", resp.StatusCode, tc.wantStatus, string(body))
			}

			// For HTML responses, just verify we got HTML, not JSON.
			if tc.skipBodyValidation {
				ct := resp.Header.Get("Content-Type")
				if ct != "text/html" && ct != "text/html; charset=utf-8" {
					t.Errorf("expected HTML content type, got %q", ct)
				}
				return
			}

			// Find the route in the spec and validate the response.
			route, pathParams, err := specRouter.FindRoute(req)
			if err != nil {
				t.Fatalf("route not found in spec: %v", err)
			}

			requestValidationInput := &openapi3filter.RequestValidationInput{
				Request:    req,
				PathParams: pathParams,
				Route:      route,
				Options: &openapi3filter.Options{
					// Skip request body validation (we have no request bodies).
					SkipSettingDefaults: true,
				},
			}

			responseValidationInput := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: requestValidationInput,
				Status:                 resp.StatusCode,
				Header:                 resp.Header,
				Body:                   resp.Body,
				Options: &openapi3filter.Options{
					SkipSettingDefaults: true,
				},
			}

			if err := openapi3filter.ValidateResponse(req.Context(), responseValidationInput); err != nil {
				body, _ := io.ReadAll(w.Body)
				t.Errorf("response validation failed: %v\nresponse body: %s", err, string(body))
			}

			// Additional check for paginated responses: Link header when there are more results.
			if tc.name == "paginated response with limit=1" {
				linkHeader := resp.Header.Get("Link")
				if linkHeader == "" {
					t.Error("expected Link header for paginated response with more results")
				}
			}
		})
	}
}
