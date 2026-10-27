package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// versionedCache holds two versions of db, one used by a readable and a
// denied service, and a hidden version of it the caller may not read.
func versionedCache() *cache.Cache {
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }

	c := cache.New(nil)
	for i, name := range []string{"db_v1", "db_v2", "hidden_v1", "hidden_v2"} {
		c.SetSecret(swarm.Secret{
			ID:        name,
			CreatedAt: day(i + 1),
			Spec:      swarm.SecretSpec{Annotations: swarm.Annotations{Name: name}},
		})
		c.SetConfig(swarm.Config{
			ID:        name,
			CreatedAt: day(i + 1),
			Spec:      swarm.ConfigSpec{Annotations: swarm.Annotations{Name: name}},
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

// versionsRequest asks as a caller who may read db_v* and hidden_v2, and one
// of the two services.
func versionsRequest(t *testing.T, path string) (*Handlers, *http.Request) {
	t.Helper()

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources: []string{
			"secret:db_*", "secret:hidden_v2", "config:db_*", "config:hidden_v2",
			"service:allowed-*",
		},
		Audience:    []string{"*"},
		Permissions: []string{"read"},
	}}})

	req := httptest.NewRequest("GET", path, nil)
	req = req.WithContext(auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "u"}))

	return newTestHandlers(t, withCache(versionedCache()), withACL(e)), req
}

type versionedItem struct {
	ID              string
	VersionOf       string
	VersionInferred bool
	UsedBy          int
}

func TestListVersions(t *testing.T) {
	for _, path := range []string{"/secrets", "/configs"} {
		t.Run(path, func(t *testing.T) {
			h, req := versionsRequest(t, path)
			w := httptest.NewRecorder()
			if path == "/secrets" {
				h.HandleListSecrets(w, req)
			} else {
				h.HandleListConfigs(w, req)
			}

			if w.Code != http.StatusOK {
				t.Fatalf("status=%d, want 200", w.Code)
			}

			var resp struct {
				Items []versionedItem `json:"items"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}

			got := map[string]versionedItem{}
			for _, item := range resp.Items {
				got[item.ID] = item
			}

			want := map[string]versionedItem{
				"db_v1": {ID: "db_v1", VersionOf: "db", VersionInferred: true},
				// The denied service is not counted.
				"db_v2": {ID: "db_v2", VersionOf: "db", VersionInferred: true, UsedBy: 1},
				// Its other version is not readable, so it stands alone.
				"hidden_v2": {ID: "hidden_v2"},
			}
			if len(got) != len(want) {
				t.Fatalf("items = %+v, want %+v", got, want)
			}
			for id, item := range want {
				if got[id] != item {
					t.Errorf("%s = %+v, want %+v", id, got[id], item)
				}
			}
		})
	}
}

func TestGetVersions(t *testing.T) {
	var resp struct {
		VersionOf       string `json:"versionOf"`
		VersionInferred bool   `json:"versionInferred"`
		UsedBy          int    `json:"usedBy"`
	}

	h, req := versionsRequest(t, "/secrets/db_v2")
	req.SetPathValue("id", "db_v2")
	w := httptest.NewRecorder()
	h.HandleGetSecret(w, req)

	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.VersionOf != "db" || !resp.VersionInferred || resp.UsedBy != 1 {
		t.Errorf("secret = %+v, want a version of db used by 1", resp)
	}

	h, req = versionsRequest(t, "/configs/hidden_v2")
	req.SetPathValue("id", "hidden_v2")
	w = httptest.NewRecorder()
	h.HandleGetConfig(w, req)

	resp.VersionOf, resp.UsedBy = "unset", -1
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.VersionOf != "unset" || resp.UsedBy != 0 {
		t.Errorf("config = %+v, want no versionOf and usedBy 0", resp)
	}
}
