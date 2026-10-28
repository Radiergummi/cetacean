package mcp

import (
	"encoding/json"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

func portsServer(t *testing.T, overrides ...func(*Options)) *Server {
	t.Helper()

	c := cache.New(nil)
	for _, svc := range []struct {
		id, name string
		port     uint32
	}{{"s1", "web", 8080}, {"s2", "admin", 9000}} {
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

	return newToolTestServer(t, c, &fakeWriteClient{}, config.OpsReadOnly, overrides...)
}

func callGetPublishedPorts(t *testing.T, srv *Server, args map[string]any) portsResult {
	t.Helper()

	td, ok := srv.findTool("get_published_ports")
	if !ok {
		t.Fatal("get_published_ports is not registered at OpsReadOnly")
	}

	text, err := td.handler(ctxWithIdentity(), newCallToolRequest("get_published_ports", args))
	if err != nil {
		t.Fatalf("get_published_ports: %v", err)
	}

	var result portsResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("decode result: %v: %s", err, text)
	}

	return result
}

func TestGetPublishedPorts(t *testing.T) {
	result := callGetPublishedPorts(t, portsServer(t), map[string]any{})

	if result.Total != 2 || result.Items[0].PublishedPort != 8080 {
		t.Errorf("result = %+v, want both ports from 8080 up", result)
	}
}

func TestGetPublishedPortsNarrowsToOnePort(t *testing.T) {
	result := callGetPublishedPorts(t, portsServer(t), map[string]any{"port": 9000})

	if result.Total != 1 || result.Items[0].ServiceName != "admin" {
		t.Errorf("result = %+v, want admin's 9000", result)
	}
}

// A port is read as part of its service, so a caller sees no trace of the
// ports of a service they may not read.
func TestGetPublishedPortsAppliesACL(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(readOnlyPolicy("service:web"))

	result := callGetPublishedPorts(
		t,
		portsServer(t, func(o *Options) { o.ACL = evaluator }),
		map[string]any{},
	)

	if result.Total != 1 || result.Items[0].ServiceName != "web" {
		t.Errorf("result = %+v, want only web's port", result)
	}
}

func TestGetPublishedPortsRefusesAnInvalidPort(t *testing.T) {
	td, _ := portsServer(t).findTool("get_published_ports")

	for _, port := range []any{"http", 0.0, -1.0, 80.5, 70000.0} {
		if _, err := td.handler(
			ctxWithIdentity(),
			newCallToolRequest("get_published_ports", map[string]any{"port": port}),
		); err == nil {
			t.Errorf("port %v was accepted", port)
		}
	}
}
