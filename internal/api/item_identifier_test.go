package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
)

// An entry that has no URL of its own is identified by a fragment of the
// collection holding it, so the @id dereferences to the document it is in.
func TestAnItemWithoutARouteIsAFragmentOfItsCollection(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{ID: "n1"})
	router := newTestRouterWithCache(t, c, withSystemClient(&mockSystemClient{
		diskUsageFn: func(context.Context) (types.DiskUsage, error) { return types.DiskUsage{}, nil },
	}))

	for path, prefix := range map[string]string{"/history": "/history#", "/disk-usage": "/disk-usage#"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			var list struct {
				Items []struct {
					ID string `json:"@id"`
				} `json:"items"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Items) == 0 {
				t.Fatalf("no items (%v): %s", err, w.Body.String())
			}

			for _, item := range list.Items {
				if !strings.HasPrefix(item.ID, prefix) || len(item.ID) == len(prefix) {
					t.Errorf("@id = %q, want a fragment of %s", item.ID, path)
				}
			}
		})
	}
}
