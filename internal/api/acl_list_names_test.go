package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// Every list endpoint pairs its resourceType with an aclName, and the ACL only
// admits an item when the two together spell what the grant names. A node is
// granted by hostname and a task by id, so a wrong pairing shows up as a list
// that is empty or complete rather than filtered.
func TestListEndpointsFilterByTheirACLName(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n-keep", Description: swarm.NodeDescription{Hostname: "keeper"}})
	c.SetNode(swarm.Node{ID: "n-drop", Description: swarm.NodeDescription{Hostname: "other"}})
	c.SetService(swarm.Service{
		ID:   "s-keep",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "keeper"}},
	})
	c.SetService(swarm.Service{
		ID:   "s-drop",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "other"}},
	})
	c.SetTask(swarm.Task{ID: "keeper", ServiceID: "s-keep"})
	c.SetTask(swarm.Task{ID: "other", ServiceID: "s-drop"})
	c.SetConfig(swarm.Config{
		ID:   "c-keep",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "keeper"}},
	})
	c.SetConfig(swarm.Config{
		ID:   "c-drop",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "other"}},
	})
	c.SetSecret(swarm.Secret{
		ID:   "x-keep",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "keeper"}},
	})
	c.SetSecret(swarm.Secret{
		ID:   "x-drop",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "other"}},
	})
	c.SetNetwork(network.Summary{ID: "net-keep", Name: "keeper"})
	c.SetNetwork(network.Summary{ID: "net-drop", Name: "other"})
	c.SetVolume(volume.Volume{Name: "keeper"})
	c.SetVolume(volume.Volume{Name: "other"})

	cases := []struct {
		resourceType string
		path         string
		handler      func(*Handlers) http.HandlerFunc
	}{
		{"node", "/api/nodes", func(h *Handlers) http.HandlerFunc { return h.HandleListNodes }},
		{
			"service",
			"/api/services",
			func(h *Handlers) http.HandlerFunc { return h.HandleListServices },
		},
		{"task", "/api/tasks", func(h *Handlers) http.HandlerFunc { return h.HandleListTasks }},
		{
			"config",
			"/api/configs",
			func(h *Handlers) http.HandlerFunc { return h.HandleListConfigs },
		},
		{
			"secret",
			"/api/secrets",
			func(h *Handlers) http.HandlerFunc { return h.HandleListSecrets },
		},
		{
			"network",
			"/api/networks",
			func(h *Handlers) http.HandlerFunc { return h.HandleListNetworks },
		},
		{
			"volume",
			"/api/volumes",
			func(h *Handlers) http.HandlerFunc { return h.HandleListVolumes },
		},
	}

	for _, tc := range cases {
		t.Run(tc.resourceType, func(t *testing.T) {
			e := acl.NewEvaluator()
			e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
				Resources:   []string{tc.resourceType + ":keeper"},
				Permissions: []string{"read"},
			}}})
			h := newTestHandlers(t, withCache(c), withACL(e))

			req := httptest.NewRequest("GET", tc.path+"?per_page=100", nil)
			req = req.WithContext(
				auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "u"}),
			)
			w := httptest.NewRecorder()
			tc.handler(h)(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("%s returned %d: %s", tc.path, w.Code, w.Body.String())
			}

			var resp struct {
				Total int `json:"total"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Total != 1 {
				t.Errorf("a grant naming one %s admitted %d of 2 — resourceType and "+
					"aclName do not spell what the grant names",
					tc.resourceType, resp.Total)
			}
		})
	}
}
