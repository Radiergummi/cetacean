package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func portsCache() *cache.Cache {
	c := cache.New(nil)
	for _, svc := range []struct {
		id, name string
		port     uint32
	}{{"s1", "web", 8080}, {"s2", "admin", 9000}, {"s3", "api", 443}} {
		c.SetService(swarm.Service{
			ID:   svc.id,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: svc.name}},
			Endpoint: swarm.Endpoint{Ports: []swarm.PortConfig{{
				Protocol:      swarm.PortConfigProtocolTCP,
				TargetPort:    80,
				PublishedPort: svc.port,
				PublishMode:   swarm.PortConfigPublishModeIngress,
			}}},
		})
	}

	return c
}

type portsPage struct {
	Total int `json:"total"`
	Items []struct {
		ID            string   `json:"@id"`
		Type          string   `json:"@type"`
		PublishedPort int      `json:"PublishedPort"`
		ServiceName   string   `json:"ServiceName"`
		Nodes         []string `json:"Nodes"`
	} `json:"items"`
}

func listPorts(t *testing.T, h *Handlers, target string, identity *auth.Identity) portsPage {
	t.Helper()

	req := httptest.NewRequest("GET", target, nil)
	if identity != nil {
		req = req.WithContext(auth.ContextWithIdentity(req.Context(), identity))
	}

	w := httptest.NewRecorder()
	h.HandleListPorts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, body %s", target, w.Code, w.Body)
	}

	var page portsPage
	if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return page
}

func TestHandleListPorts(t *testing.T) {
	h := newTestHandlers(t, withCache(portsCache()))

	page := listPorts(t, h, "/ports", nil)
	if page.Total != 3 || page.Items[0].PublishedPort != 443 {
		t.Fatalf("expected 3 ports from 443 up, got %+v", page)
	}

	if page.Items[0].ID != "/services/s3#port-443-tcp-80" || page.Items[0].Type != "PublishedPort" {
		t.Errorf("item = %+v, want a fragment of its service typed PublishedPort", page.Items[0])
	}
}

func TestHandleListPorts_SortsPortsAsNumbers(t *testing.T) {
	h := newTestHandlers(t, withCache(portsCache()))

	page := listPorts(t, h, "/ports?sort=published&dir=desc", nil)

	var got []int
	for _, item := range page.Items {
		got = append(got, item.PublishedPort)
	}

	if len(got) != 3 || got[0] != 9000 || got[1] != 8080 || got[2] != 443 {
		t.Errorf("ports = %v, want [9000 8080 443]", got)
	}
}

func TestHandleListPorts_Filter(t *testing.T) {
	h := newTestHandlers(t, withCache(portsCache()))

	page := listPorts(t, h, "/ports?filter="+strings.ReplaceAll("published > 1024", " ", "+"), nil)
	if page.Total != 2 {
		t.Errorf("expected 2 ports above 1024, got %d", page.Total)
	}
}

func TestHandleListPorts_ACLFiltering(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:web"},
		Audience:    []string{"*"},
		Permissions: []string{"read"},
	}}})
	h := newTestHandlers(t, withCache(portsCache()), withACL(e))

	page := listPorts(t, h, "/ports", &auth.Identity{Subject: "user1"})
	if page.Total != 1 || page.Items[0].ServiceName != "web" {
		t.Errorf("expected only web's port, got %+v", page)
	}
}

func TestHandleListPorts_CSV(t *testing.T) {
	h := newTestHandlers(t, withCache(portsCache()))

	req := withContentType(httptest.NewRequest("GET", "/ports", nil), ContentTypeCSV)
	w := httptest.NewRecorder()
	h.HandleListPorts(w, req)

	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\r\n")
	if len(lines) != 4 ||
		lines[0] != "published,target,protocol,mode,service,stack,nodes,service id" {
		t.Fatalf("CSV =\n%s", w.Body)
	}

	if lines[1] != "443,80,tcp,ingress,api,,,s3" {
		t.Errorf("first record = %q", lines[1])
	}
}

// A host-mode port names the nodes binding it only where the caller may read
// them, as a task names its node.
func TestHandleListPorts_NamesOnlyReadableNodes(t *testing.T) {
	c := cache.New(nil)
	c.SetService(
		swarm.Service{
			ID:   "edge",
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "edge"}},
		},
	)
	for _, node := range []string{"n1", "n2"} {
		c.SetNode(
			swarm.Node{ID: node, Description: swarm.NodeDescription{Hostname: "host-" + node}},
		)
		c.SetTask(
			swarm.Task{ID: "t-" + node, ServiceID: "edge", NodeID: node, Status: swarm.TaskStatus{
				State: swarm.TaskStateRunning,
				PortStatus: swarm.PortStatus{Ports: []swarm.PortConfig{{
					Protocol: swarm.PortConfigProtocolTCP, TargetPort: 443, PublishedPort: 443,
					PublishMode: swarm.PortConfigPublishModeHost,
				}}},
			}},
		)
	}

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:edge", "node:host-n1"},
		Audience:    []string{"*"},
		Permissions: []string{"read"},
	}}})
	h := newTestHandlers(t, withCache(c), withACL(e))

	page := listPorts(t, h, "/ports", &auth.Identity{Subject: "user1"})
	if page.Total != 1 || len(page.Items[0].Nodes) != 1 || page.Items[0].Nodes[0] != "n1" {
		t.Errorf("expected the port with only n1 named, got %+v", page)
	}
}
