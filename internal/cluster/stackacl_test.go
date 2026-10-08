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

// labelledStack is a cache holding stack "web": web_api carries no ACL label,
// web_admin is readable by group:ops alone.
func labelledStack(t *testing.T) (*cache.Cache, *acl.Evaluator) {
	t.Helper()

	c := cache.New(nil)
	for id, labels := range map[string]map[string]string{
		"svc-api":   {},
		"svc-admin": {acl.LabelRead: "group:ops"},
	} {
		labels["com.docker.stack.namespace"] = "web"
		c.SetService(swarm.Service{
			ID: id,
			Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
				Name:   "web_" + id[len("svc-"):],
				Labels: labels,
			}},
		})
	}

	e := acl.NewEvaluator()
	e.SetLabelsEnabled(true)
	e.SetResolver(c)

	return c, e
}

var (
	ops = &auth.Identity{Subject: "o", Groups: []string{"ops"}}
	dev = &auth.Identity{Subject: "d", Groups: []string{"devs"}}
)

// With no policy, a label is the only grant its resource has, so a stack read
// must not hand back a member whose label names someone else.
func TestFilterStackDetailDropsAMemberItsLabelWithholds(t *testing.T) {
	c, e := labelledStack(t)
	detail, _ := c.GetStackDetail("web")

	if got := names(
		FilterStackDetail(e, dev, detail).Services,
	); len(got) != 1 ||
		got[0] != "web_api" {
		t.Errorf("dev sees %v, want only web_api", got)
	}
	if got := FilterStackDetail(e, ops, detail).Services; len(got) != 2 {
		t.Errorf("ops sees %v, want both services", names(got))
	}
}

// A label does not revoke an explicit grant, and a stack grant is one: a member
// whose label names nobody relevant stays readable through it.
func TestFilterStackDetailKeepsAMemberAStackGrantCovers(t *testing.T) {
	c, e := labelledStack(t)
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"stack:web"},
		Audience:    []string{"group:devs"},
		Permissions: []string{"read"},
	}}})
	detail, _ := c.GetStackDetail("web")

	if got := FilterStackDetail(e, dev, detail).Services; len(got) != 2 {
		t.Errorf("dev sees %v, want both services through the stack grant", names(got))
	}
}

// The stack listings carry member IDs and counts, so they withhold the same
// members the detail does.
func TestStackListingsWithholdTheMembersTheDetailDoes(t *testing.T) {
	c, e := labelledStack(t)

	withheld := WithheldStackMembers(e, dev, c)
	if !withheld["service:svc-admin"] || len(withheld) != 1 {
		t.Fatalf("withheld = %v, want only service:svc-admin", withheld)
	}
	if got := WithheldStackMembers(e, ops, c); len(got) != 0 {
		t.Errorf("ops has members withheld: %v", got)
	}

	stacks := FilterStacks(c.ListStacks(), withheld)
	if len(stacks) != 1 || len(stacks[0].Services) != 1 || stacks[0].Services[0] != "svc-api" {
		t.Errorf("stacks = %+v, want web holding only svc-api", stacks)
	}

	summaries := c.ListStackSummaries(withheld)
	if len(summaries) != 1 || summaries[0].ServiceCount != 1 || summaries[0].DesiredTasks != 1 {
		t.Errorf("summaries = %+v, want one service and its one task", summaries)
	}

	results := Search(t.Context(), c, "web", 10, withheld)
	for _, result := range results.Hits["stacks"] {
		if result.Detail != "1 services" {
			t.Errorf("search detail = %q, want the one readable service counted", result.Detail)
		}
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
