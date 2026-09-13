package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
)

// composeResourceCache is a one-service stack plus the network the service
// attaches to, which is what resolves the attachment's ID to a name.
func composeResourceCache() *cache.Cache {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{
		ID:     "netid-internal",
		Name:   "web_internal",
		Driver: "overlay",
		Labels: map[string]string{"com.docker.stack.namespace": "web"},
	})
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "web_api",
				Labels: map[string]string{"com.docker.stack.namespace": "web"},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.27"},
				Networks: []swarm.NetworkAttachmentConfig{
					{Target: "netid-internal"},
				},
			},
		},
	})

	return c
}

// Every other resource is JSON-marshalled, which would deliver the document as
// one quoted string with escaped newlines — unusable as a compose file.
func TestComposeResourceIsYAMLNotJSONEncoded(t *testing.T) {
	srv := newResourceTestServer(t, composeResourceCache())

	body, err := srv.readResource(context.Background(), "cetacean://stacks/web/compose")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	if strings.HasPrefix(body, `"`) {
		t.Error("the document was JSON-encoded; it must be returned as raw text")
	}
	if !strings.HasPrefix(body, "# Exported from Cetacean.") {
		t.Errorf("body does not open with the compose header:\n%s", body)
	}
}

// The MIME type is what tells a client this is not the JSON every other
// resource returns.
func TestComposeResourceReportsYAMLMediaType(t *testing.T) {
	srv := newResourceTestServer(t, composeResourceCache())

	for _, uri := range []string{
		"cetacean://stacks/web/compose",
		"cetacean://services/svc1/compose",
	} {
		_, mime, err := srv.readResourceContents(context.Background(), uri)
		if err != nil {
			t.Fatalf("%s: %v", uri, err)
		}
		if mime != composeMIMEType {
			t.Errorf("%s: mime = %q, want %q", uri, mime, composeMIMEType)
		}
	}

	// The digest resources must be unaffected.
	_, mime, err := srv.readResourceContents(context.Background(), "cetacean://stacks/web")
	if err != nil {
		t.Fatalf("stack digest: %v", err)
	}
	if mime != mcpMIMEType {
		t.Errorf("stack digest mime = %q, want %q", mime, mcpMIMEType)
	}
}

func TestComposeResourceRefusesAnUnreadableStack(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("stack:other"))

	srv := newResourceTestServer(t, composeResourceCache(), func(o *Options) { o.ACL = e })

	if _, err := srv.readResource(ctxWithIdentity(), "cetacean://stacks/web/compose"); err == nil {
		t.Error("a stack this identity cannot read must not export")
	}
}

func TestComposeResourceRefusesAnUnreadableService(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:public-*"))

	srv := newResourceTestServer(t, composeResourceCache(), func(o *Options) { o.ACL = e })

	_, err := srv.readResource(ctxWithIdentity(), "cetacean://services/svc1/compose")
	if err == nil {
		t.Error("a service this identity cannot read must not export")
	}
}

// The service resource resolves its attachment the same way the stack one
// does; an ID reaching the document would match no declaration.
func TestComposeResourceResolvesNetworkNames(t *testing.T) {
	srv := newResourceTestServer(t, composeResourceCache())

	body, err := srv.readResource(context.Background(), "cetacean://services/svc1/compose")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	if strings.Contains(body, "netid-internal") {
		t.Errorf("document leaks a network ID:\n%s", body)
	}
	if !strings.Contains(body, "web_internal") {
		t.Errorf("document does not name the network:\n%s", body)
	}
}
