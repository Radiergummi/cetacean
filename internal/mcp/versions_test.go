package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// versionsServer holds db_v1 and db_v2, the newer used by one readable and one
// denied service, and hidden_v1 and hidden_v2, of which only the newer is
// readable.
func versionsServer(t *testing.T) (*Server, context.Context) {
	t.Helper()

	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "hidden_v1", "hidden_v2"} {
		c.SetSecret(swarm.Secret{
			ID:        name,
			CreatedAt: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC),
			Spec:      swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	for _, name := range []string{"allowed-svc", "denied-svc"} {
		c.SetService(swarm.Service{
			ID: name,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: name},
				TaskTemplate: swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{
					Secrets: []*swarm.SecretReference{{SecretID: "db_v2"}},
				}},
			},
		})
	}

	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"secret:db_*", "secret:hidden_v2", "service:allowed-*"},
		Audience:    []string{"user:agent@example.com"},
		Permissions: []string{"read"},
	}}})

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = evaluator })
	ctx := auth.ContextWithIdentity(
		context.Background(),
		&auth.Identity{Subject: "agent@example.com"},
	)

	return srv, ctx
}

func TestFindGroupsSecretVersions(t *testing.T) {
	srv, ctx := versionsServer(t)

	found, err := srv.toolFind(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{Arguments: map[string]any{"type": "secrets"}},
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	type row struct {
		ID              string `json:"id"`
		VersionOf       string `json:"versionOf"`
		VersionInferred bool   `json:"versionInferred"`
		UsedBy          *int   `json:"usedBy"`
	}

	var result struct {
		Items []row `json:"items"`
	}
	if err := json.Unmarshal([]byte(found), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := map[string]row{}
	for _, r := range result.Items {
		if r.UsedBy == nil {
			t.Fatalf("%s carries no usedBy: %s", r.ID, found)
		}
		got[r.ID] = r
	}

	want := map[string]struct {
		of     string
		usedBy int
	}{
		"db_v1": {"db", 0},
		// The denied service is not counted.
		"db_v2": {"db", 1},
		// Its other version is not readable, so it stands alone.
		"hidden_v2": {"", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %+v, want %v", got, want)
	}
	for id, w := range want {
		r := got[id]
		if r.VersionOf != w.of || *r.UsedBy != w.usedBy || r.VersionInferred != (w.of != "") {
			t.Errorf("%s = %+v, want versionOf %q usedBy %d", id, r, w.of, w.usedBy)
		}
	}
}

func TestDescribeCarriesSecretVersion(t *testing.T) {
	srv, ctx := versionsServer(t)

	described, err := srv.toolDescribe(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{"type": "secret", "id": "db_v2"},
		},
	})
	if err != nil {
		t.Fatalf("describe: %v", err)
	}

	var result struct {
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal([]byte(described), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	d := result.Details
	if d["versionOf"] != "db" || d["versionInferred"] != true || d["usedBy"] != 1.0 {
		t.Errorf("details = %v, want a version of db used by 1", d)
	}
}
