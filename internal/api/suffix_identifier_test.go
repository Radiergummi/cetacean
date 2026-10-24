package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/errdefs"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestADeleteNeverStripsASuffixOffTheVolumeName(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "backup"})
	c.SetVolume(volume.Volume{Name: "backup.json"})

	var removed string
	wc := &mockWriteClient{
		removeVolumeFn: func(_ context.Context, name string, _ bool) error {
			removed = name
			return nil
		},
	}
	router := newTestRouterWithCache(t, c, withWriteClient(wc))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/volumes/backup.json", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", w.Code, w.Body.String())
	}
	if removed != "backup.json" {
		t.Errorf("removed volume %q, want backup.json", removed)
	}
}

func TestADeleteOfAnUnknownSuffixedNameIsNotFound(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "backup"})

	wc := &mockWriteClient{
		removeVolumeFn: func(_ context.Context, name string, _ bool) error {
			t.Errorf("removed volume %q; nothing is named backup.json", name)
			return nil
		},
	}
	router := newTestRouterWithCache(t, c, withWriteClient(wc))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/volumes/backup.json", nil))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestAGetOfAnExactSuffixedNameAddressesThatVolume(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "backup", Driver: "plain"})
	c.SetVolume(volume.Volume{Name: "backup.json", Driver: "suffixed"})
	router := newTestRouterWithCache(t, c)

	for _, tc := range []struct{ path, accept, driver string }{
		{"/volumes/backup.json", "application/json", "suffixed"},
		{"/volumes/backup.json.json", "", "suffixed"},
		{"/volumes/backup", "application/json", "plain"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}

			var body struct {
				ID     string `json:"@id"`
				Volume struct{ Driver string }
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("not JSON: %v: %s", err, w.Body.String())
			}
			if body.Volume.Driver != tc.driver {
				t.Errorf("served the %q volume, want %q", body.Volume.Driver, tc.driver)
			}
			if tc.driver == "suffixed" && body.ID != "/volumes/backup.json" {
				t.Errorf("@id = %q, want /volumes/backup.json", body.ID)
			}
		})
	}
}

// Whether a suffixed name exists is only revealed to a caller who may read it.
func TestASuffixedNameTheCallerCannotReadStillNegotiates(t *testing.T) {
	c := cache.New(nil)
	c.SetVolume(volume.Volume{Name: "backup", Driver: "plain"})
	c.SetVolume(volume.Volume{Name: "backup.json", Driver: "suffixed"})

	eval := acl.NewEvaluator()
	eval.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"volume:backup"},
		Audience:    []string{"*"},
		Permissions: []string{"read"},
	}}})
	router := newTestRouterWithCache(t, c, withACL(eval))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/volumes/backup.json", nil))

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"plain"`) {
		t.Errorf("status = %d, want the backup volume as JSON: %s", w.Code, w.Body.String())
	}
}

// A name-addressed config is redirected to its ID; a suffix that is part of
// the name must not be read as a representation on the way.
func TestASuffixedConfigNameRedirectsToThatConfig(t *testing.T) {
	c := cache.New(nil)
	c.SetConfig(swarm.Config{ID: "plain-id", Spec: swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: "app"},
	}})
	c.SetConfig(swarm.Config{ID: "suffixed-id", Spec: swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: "app.json"},
	}})
	router := newTestRouterWithCache(t, c)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/configs/app.json", nil)
			req.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusTemporaryRedirect {
				t.Fatalf("status = %d, want 307: %s", w.Code, w.Body.String())
			}
			if got := w.Header().Get("Location"); got != "/configs/suffixed-id" {
				t.Errorf("Location = %q, want /configs/suffixed-id", got)
			}
		})
	}
}

// A plugin's tag can end like a suffix, foo:1.csv, and its name can carry an
// escaped slash; neither may lose the suffix on a write or a read of it.
func TestASuffixedPluginTagAddressesThatPlugin(t *testing.T) {
	installed := map[string]bool{"vieux/sshfs:1.csv": true, "vieux/sshfs:1": true}

	var removed string
	pc := &mockPluginClient{
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			if !installed[name] {
				return nil, errdefs.NotFound(errors.New("no such plugin"))
			}

			return &types.Plugin{Name: name}, nil
		},
		pluginRemoveFn: func(_ context.Context, name string, _ bool) error {
			removed = name
			return nil
		},
	}
	router := newTestRouterWithCache(t, cache.New(nil), withPluginClient(pc))

	w := httptest.NewRecorder()
	router.ServeHTTP(
		w,
		httptest.NewRequest(http.MethodDelete, "/plugins/vieux%2Fsshfs%3A1.csv", nil),
	)
	if w.Code != http.StatusNoContent || removed != "vieux/sshfs:1.csv" {
		t.Errorf("DELETE answered %d and removed %q, want 204 and vieux/sshfs:1.csv: %s",
			w.Code, removed, w.Body.String())
	}

	for path, want := range map[string]string{
		"/plugins/vieux%2Fsshfs%3A1.csv":  `"Name":"vieux/sshfs:1.csv"`,
		"/plugins/vieux%2Fsshfs%3A1.json": `"Name":"vieux/sshfs:1"`,
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf(
				"GET %s answered %d, want 200 with %s: %s",
				path,
				w.Code,
				want,
				w.Body.String(),
			)
		}
	}
}
