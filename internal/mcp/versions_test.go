package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// versionsServer holds db_v1..3, db_v2 used by one readable and one denied
// service, and app_v1..3, of which app_v3 is unreadable.
func versionsServer(t *testing.T) (*Server, context.Context) {
	t.Helper()

	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "db_v3", "app_v1", "app_v2", "app_v3"} {
		c.SetSecret(swarm.Secret{
			ID:        name,
			CreatedAt: time.Date(2026, 1, 1+i%3, 0, 0, 0, 0, time.UTC),
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
		Resources: []string{
			"secret:db_*", "secret:app_v1", "secret:app_v2", "service:allowed-*",
		},
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

type versionRow struct {
	ID              string `json:"id"`
	VersionOf       string `json:"versionOf"`
	VersionInferred bool   `json:"versionInferred"`
	NewestVersion   bool   `json:"newestVersion"`
	OlderVersions   int    `json:"olderVersions"`
	UsedBy          *int   `json:"usedBy"`
}

func findSecrets(t *testing.T, args map[string]any) ([]versionRow, int) {
	t.Helper()

	srv, ctx := versionsServer(t)
	args["type"] = "secrets"

	found, err := srv.toolFind(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{Arguments: args},
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}

	var result struct {
		Items []versionRow `json:"items"`
		Total int          `json:"total"`
	}
	if err := json.Unmarshal([]byte(found), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return result.Items, result.Total
}

func TestFindListsTheNewestSecretVersions(t *testing.T) {
	rows, total := findSecrets(t, map[string]any{})

	got := map[string]versionRow{}
	for _, r := range rows {
		if r.UsedBy == nil {
			t.Fatalf("%s carries no usedBy", r.ID)
		}
		got[r.ID] = r
	}

	// app_v3 is unreadable, so app_v2 heads its group.
	if total != 2 || got["db_v3"].OlderVersions != 2 || !got["db_v3"].NewestVersion ||
		got["app_v2"].OlderVersions != 1 || got["db_v3"].VersionOf != "db" {
		t.Errorf("rows = %+v (total %d), want db_v3 and app_v2 heading their groups", rows, total)
	}
}

func TestFindListsEverySecretVersion(t *testing.T) {
	rows, total := findSecrets(t, map[string]any{"versions": "all"})

	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)

		// The denied service is not counted.
		if r.ID == "db_v2" && (*r.UsedBy != 1 || r.NewestVersion) {
			t.Errorf("db_v2 = %+v, want an older version used by 1", r)
		}
	}

	slices.Sort(ids)
	if want := []string{"app_v1", "app_v2", "db_v1", "db_v2", "db_v3"}; total != 5 ||
		!slices.Equal(ids, want) {
		t.Errorf("ids = %v (total %d), want %v", ids, total, want)
	}
}

func TestFindSearchesOlderVersionsToo(t *testing.T) {
	rows, _ := findSecrets(t, map[string]any{"query": "db_v1"})

	if len(rows) != 1 || rows[0].ID != "db_v1" || rows[0].VersionOf != "db" ||
		rows[0].NewestVersion {
		t.Errorf("rows = %+v, want the older db_v1 with its version fields", rows)
	}
}

// The listing resource shows what find shows by default.
func TestSecretsResourceListsTheNewestVersions(t *testing.T) {
	srv, ctx := versionsServer(t)

	body, err := srv.readResource(ctx, "cetacean://secrets")
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var items []struct {
		ID            string
		OlderVersions int
		UsedBy        int
	}
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := map[string]int{}
	for _, item := range items {
		got[item.ID] = item.OlderVersions
	}
	if len(got) != 2 || got["db_v3"] != 2 || got["app_v2"] != 1 {
		t.Errorf("items = %+v, want db_v3 and app_v2 heading their groups", items)
	}
}

func TestFindRefusesAnUnknownVersionsValue(t *testing.T) {
	srv, ctx := versionsServer(t)

	_, err := srv.toolFind(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{"type": "secrets", "versions": "some"},
		},
	})
	if err == nil {
		t.Error("find accepted versions=some")
	}
}

func TestDescribeCarriesSecretVersion(t *testing.T) {
	srv, ctx := versionsServer(t)

	described, err := srv.toolDescribe(ctx, mcplib.CallToolRequest{
		Params: mcplib.CallToolParams{
			Arguments: map[string]any{"type": "secret", "id": "app_v2"},
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
	if d["versionOf"] != "app" || d["newestVersion"] != true || d["olderVersions"] != 1.0 ||
		d["usedBy"] != 0.0 {
		t.Errorf("details = %v, want the newest readable app version", d)
	}
}

// Without an identity a loaded policy still applies; only an absent policy
// lets everything through.
func TestRecommendationsWithoutAnIdentityFollowThePolicy(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"service:*"},
		Audience:    []string{"user:agent@example.com"},
		Permissions: []string{"read"},
	}}})

	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.ACL = evaluator })

	got := srv.filterRecommendations(context.Background(), []recommendations.Recommendation{
		{Scope: recommendations.ScopeService, TargetName: "web"},
	})
	if len(got) != 0 {
		t.Errorf("an anonymous caller saw %+v", got)
	}
}

// A created version must read as the newest of its group straight away, not as
// a zero-time record older than every version it replaces.
func TestCreatedVersionsAreSeededFromDocker(t *testing.T) {
	created := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	meta := swarm.Meta{CreatedAt: created, UpdatedAt: created}

	c := cache.New(nil)
	writeClient := &fakeWriteClient{
		createSecretFn: func(context.Context, swarm.SecretSpec) (string, error) { return "sec", nil },
		createConfigFn: func(context.Context, swarm.ConfigSpec) (string, error) { return "cfg", nil },
		inspectSecretFn: func(_ context.Context, id string) (swarm.Secret, error) {
			return swarm.Secret{ID: id, Meta: meta, Spec: swarm.SecretSpec{Name: "s_v2"}}, nil
		},
		inspectConfigFn: func(_ context.Context, id string) (swarm.Config, error) {
			return swarm.Config{ID: id, Meta: meta, Spec: swarm.ConfigSpec{
				Name:   "c_v2",
				Labels: map[string]string{"com.docker.stack.namespace": "app"},
				Data:   []byte("x"),
			}}, nil
		},
	}
	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"create_secret","arguments":{"name":"s_v2","data":"x",`+
		`"labels":{"com.docker.stack.namespace":"app"}}}`)
	callTool(t, handler, `{"name":"create_config","arguments":{"name":"c_v2","data":"x",`+
		`"labels":{"com.docker.stack.namespace":"app"}}}`)

	sec, _ := c.GetSecret("sec")
	cfg, _ := c.GetConfig("cfg")

	for name, seeded := range map[string]swarm.Meta{"secret": sec.Meta, "config": cfg.Meta} {
		if !seeded.CreatedAt.Equal(created) {
			t.Errorf("seeded %s Meta = %+v, want Docker's", name, seeded)
		}
	}

	if cfg.Spec.Labels["com.docker.stack.namespace"] != "app" || len(cfg.Spec.Data) != 0 {
		t.Errorf("seeded config spec = %+v, want Docker's labels and no payload", cfg.Spec)
	}
}
