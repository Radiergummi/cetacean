package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// versionedCache holds db_v1..3 and app_v1..3 as both secrets and configs.
// db_v2 is used by a readable and a denied service.
func versionedCache() *cache.Cache {
	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "db_v3", "app_v1", "app_v2", "app_v3"} {
		meta := swarm.Meta{CreatedAt: time.Date(2026, 1, 1+i%3, 0, 0, 0, 0, time.UTC)}
		c.SetSecret(swarm.Secret{
			ID:   name,
			Meta: meta,
			Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
		})
		c.SetConfig(swarm.Config{
			ID:   name,
			Meta: meta,
			Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}},
		})
	}

	for _, name := range []string{"allowed-svc", "denied-svc"} {
		c.SetService(swarm.Service{
			ID: name,
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{Name: name},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{
						Secrets: []*swarm.SecretReference{{SecretID: "db_v2"}},
						Configs: []*swarm.ConfigReference{{ConfigID: "db_v2"}},
					},
				},
			},
		})
	}

	return c
}

// versionsHandlers asks as a caller who may read every db version, all but the
// newest app version, and one of the two services.
func versionsHandlers(t *testing.T) *Handlers {
	t.Helper()

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources: []string{
			"secret:db_*", "secret:app_v1", "secret:app_v2",
			"config:db_*", "config:app_v1", "config:app_v2",
			"service:allowed-*",
		},
		Audience:    []string{"*"},
		Permissions: []string{"read"},
	}}})

	return newTestHandlers(t, withCache(versionedCache()), withACL(e))
}

type versionedItem struct {
	ID              string
	VersionOf       string
	VersionInferred bool
	NewestVersion   bool
	OlderVersions   int
	UsedBy          int
}

type versionedPage struct {
	Items []versionedItem `json:"items"`
	Total int             `json:"total"`
}

func listVersions(t *testing.T, path string) (*httptest.ResponseRecorder, versionedPage) {
	t.Helper()

	h := versionsHandlers(t)
	req := httptest.NewRequest("GET", path, nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "u"}))
	w := httptest.NewRecorder()

	if req.URL.Path == "/secrets" {
		h.HandleListSecrets(w, req)
	} else {
		h.HandleListConfigs(w, req)
	}

	var page versionedPage
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}

	return w, page
}

func ids(page versionedPage) []string {
	out := make([]string, len(page.Items))
	for i, item := range page.Items {
		out[i] = item.ID
	}

	return out
}

func TestListVersions(t *testing.T) {
	for _, base := range []string{"/secrets", "/configs"} {
		t.Run(base, func(t *testing.T) {
			t.Run("newest only by default", func(t *testing.T) {
				_, page := listVersions(t, base+"?sort=name")

				// app_v3 is unreadable, so app_v2 is the newest the caller sees.
				want := []versionedItem{
					{"app_v2", "app", true, true, 1, 0},
					{"db_v3", "db", true, true, 2, 0},
				}
				if !slices.Equal(page.Items, want) || page.Total != 2 {
					t.Errorf("items = %+v (total %d), want %+v", page.Items, page.Total, want)
				}
			})

			t.Run("the total follows the selection", func(t *testing.T) {
				_, page := listVersions(t, base+"?sort=name&limit=1")
				if page.Total != 2 || len(page.Items) != 1 {
					t.Errorf("total = %d with %d items, want 2 with 1", page.Total, len(page.Items))
				}
			})

			t.Run("all", func(t *testing.T) {
				_, page := listVersions(t, base+"?versions=all&sort=name")

				want := []string{"app_v1", "app_v2", "db_v1", "db_v2", "db_v3"}
				if got := ids(page); !slices.Equal(got, want) || page.Total != 5 {
					t.Errorf("ids = %v (total %d), want %v", got, page.Total, want)
				}

				// The denied service is not counted.
				if item := page.Items[3]; item.UsedBy != 1 || item.NewestVersion {
					t.Errorf("db_v2 = %+v, want an older version used by 1", item)
				}
			})

			t.Run("one group, newest first", func(t *testing.T) {
				_, page := listVersions(t, base+"?versionsOf=db_v1")

				want := []string{"db_v3", "db_v2", "db_v1"}
				if got := ids(page); !slices.Equal(got, want) {
					t.Errorf("ids = %v, want %v", got, want)
				}
			})

			t.Run("an unreadable group member names no group", func(t *testing.T) {
				_, page := listVersions(t, base+"?versionsOf=app_v3")
				if len(page.Items) != 0 {
					t.Errorf("items = %+v, want none", page.Items)
				}
			})

			t.Run("an unknown value is refused", func(t *testing.T) {
				w, _ := listVersions(t, base+"?versions=some")
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", w.Code)
				}
				assertACLErrorCode(t, w, "API016")
			})
		})
	}
}

// A created version must read as the newest of its group straight away, not as
// a zero-time record older than every version it replaces.
func TestCreatedVersionsAreSeededAsNew(t *testing.T) {
	c := cache.New(nil)
	wc := &mockWriteClient{
		createConfigFn: func(context.Context, swarm.ConfigSpec) (string, error) { return "cfg", nil },
		createSecretFn: func(context.Context, swarm.SecretSpec) (string, error) { return "sec", nil },
	}
	h := newTestHandlers(
		t,
		withCache(c),
		withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration),
	)

	before := time.Now()
	for path, handle := range map[string]http.HandlerFunc{
		"/configs": h.HandleCreateConfig,
		"/secrets": h.HandleCreateSecret,
	} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"name":"x_v2","data":"eA=="}`))
		handle(httptest.NewRecorder(), req)
	}

	cfg, _ := c.GetConfig("cfg")
	sec, _ := c.GetSecret("sec")

	for name, meta := range map[string]swarm.Meta{"config": cfg.Meta, "secret": sec.Meta} {
		if meta.CreatedAt.Before(before) || meta.UpdatedAt.Before(before) {
			t.Errorf("seeded %s Meta = %+v, want the time of the create", name, meta)
		}
	}
}

func TestGetVersions(t *testing.T) {
	type detail struct {
		VersionOf       string `json:"versionOf"`
		VersionInferred bool   `json:"versionInferred"`
		NewestVersion   bool   `json:"newestVersion"`
		OlderVersions   int    `json:"olderVersions"`
		UsedBy          int    `json:"usedBy"`
	}

	get := func(t *testing.T, path, id string) detail {
		t.Helper()

		h := versionsHandlers(t)
		req := httptest.NewRequest("GET", path+id, nil)
		req.SetPathValue("id", id)
		req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "u"}))
		w := httptest.NewRecorder()

		if path == "/secrets/" {
			h.HandleGetSecret(w, req)
		} else {
			h.HandleGetConfig(w, req)
		}

		var d detail
		if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
			t.Fatalf("decode: %v", err)
		}

		return d
	}

	for _, path := range []string{"/secrets/", "/configs/"} {
		if got, want := get(t, path, "db_v2"), (detail{"db", true, false, 0, 1}); got != want {
			t.Errorf("%sdb_v2 = %+v, want %+v", path, got, want)
		}

		// The newest readable app version, with app_v3 unreadable.
		if got, want := get(t, path, "app_v2"), (detail{"app", true, true, 1, 0}); got != want {
			t.Errorf("%sapp_v2 = %+v, want %+v", path, got, want)
		}
	}
}
