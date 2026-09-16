package cluster

import (
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func stackFixture() cache.StackDetail {
	return cache.StackDetail{
		Name: "web",
		Services: []swarm.Service{
			{Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web_api"}}},
			{Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web_admin"}}},
		},
		Secrets: []swarm.Secret{
			{Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "web_token"}}},
		},
		Networks: []network.Summary{{Name: "web_internal"}},
		Volumes:  []volume.Volume{{Name: "web_data"}},
	}
}

// A stack grant used to imply its members. A resource label narrows per
// resource, and the stack itself carries none, so the grant must not hand back
// what the label withholds.
func TestFilterStackDetailDropsUnreadableMembers(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"stack:web", "service:web_api"},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	got := FilterStackDetail(e, &auth.Identity{Subject: "alice"}, stackFixture())

	if len(got.Services) != 1 || got.Services[0].Spec.Name != "web_api" {
		t.Errorf("services = %v, want only web_api", names(got.Services))
	}
	if len(got.Secrets) != 0 {
		t.Error("a secret the identity may not read survived the stack read")
	}
	if len(got.Networks) != 0 || len(got.Volumes) != 0 {
		t.Error("a network or volume the identity may not read survived the stack read")
	}
	if got.Name != "web" {
		t.Errorf("name = %q, want it untouched", got.Name)
	}
}

// With no policy the evaluator allows everything, and the projection must not
// quietly become a filter.
func TestFilterStackDetailKeepsEverythingWithoutAPolicy(t *testing.T) {
	got := FilterStackDetail(acl.NewEvaluator(), nil, stackFixture())

	if len(got.Services) != 2 || len(got.Secrets) != 1 {
		t.Errorf("an unpolicied evaluator dropped members: %+v", got)
	}
}

func names(services []swarm.Service) []string {
	out := make([]string, 0, len(services))
	for _, s := range services {
		out = append(out, s.Spec.Name)
	}

	return out
}
