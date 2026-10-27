package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/ipam"
)

// addressesCache holds an overlay with one task on it, an empty overlay, and
// a bridge network, which has no figure.
func addressesCache() *cache.Cache {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{
		ID:     "busy",
		Name:   "busy",
		Driver: "overlay",
		Scope:  "swarm",
		IPAM:   network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.1.0/29"}}},
	})
	c.SetNetwork(network.Summary{
		ID:     "empty",
		Name:   "empty",
		Driver: "overlay",
		Scope:  "swarm",
		IPAM:   network.IPAM{Config: []network.IPAMConfig{{Subnet: "10.0.2.0/24"}}},
	})
	c.SetNetwork(network.Summary{ID: "bridge", Name: "bridge", Driver: "bridge", Scope: "local"})
	c.SetTask(swarm.Task{
		ID:     "t1",
		NodeID: "n1",
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
		NetworksAttachments: []swarm.NetworkAttachment{{
			Network:   swarm.Network{ID: "busy"},
			Addresses: []string{"10.0.1.2/29"},
		}},
	})

	return c
}

func TestHandleListNetworks_Addresses(t *testing.T) {
	h := newTestHandlers(t, withCache(addressesCache()))

	req := httptest.NewRequest("GET", "/networks?sort=addresses&dir=desc", nil)
	w := httptest.NewRecorder()
	h.HandleListNetworks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp CollectionResponse[NetworkListItem]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 3 {
		t.Fatalf("expected 3 networks, got %d", len(resp.Items))
	}

	order := []string{resp.Items[0].ID, resp.Items[1].ID, resp.Items[2].ID}
	if order[0] != "busy" || order[1] != "empty" || order[2] != "bridge" {
		t.Errorf("order by addresses desc = %v, want [busy empty bridge]", order)
	}

	want := ipam.Addresses{Usable: 5, Used: 2, Tasks: 1, LoadBalancers: 1}
	if got := resp.Items[0].Addresses; got == nil || *got != want {
		t.Errorf("busy: Addresses = %+v, want %+v", got, want)
	}

	if resp.Items[2].Addresses != nil {
		t.Errorf("bridge: Addresses = %+v, want none", resp.Items[2].Addresses)
	}
}

func TestHandleGetNetwork_Addresses(t *testing.T) {
	h := newTestHandlers(t, withCache(addressesCache()))

	for id, want := range map[string]*ipam.Addresses{
		"busy":   {Usable: 5, Used: 2, Tasks: 1, LoadBalancers: 1},
		"bridge": nil,
	} {
		req := httptest.NewRequest("GET", "/networks/"+id, nil)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.HandleGetNetwork(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", id, w.Code)
		}

		var resp struct {
			Addresses *ipam.Addresses `json:"addresses"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}

		if (resp.Addresses == nil) != (want == nil) ||
			(want != nil && *resp.Addresses != *want) {
			t.Errorf("%s: addresses = %+v, want %+v", id, resp.Addresses, want)
		}
	}
}
