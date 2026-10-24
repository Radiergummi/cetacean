package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

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
