package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// RFC 9110 §8.3.1: type and subtype are case-insensitive, and a parameter
// follows a semicolon; a longer subtype that merely starts the same is another type.
func TestAPatchContentTypeIsReadAsAMediaType(t *testing.T) {
	cases := map[string]bool{
		"application/merge-patch+json":                true,
		"Application/Merge-Patch+JSON":                true,
		"application/merge-patch+json; charset=utf-8": true,
		"application/merge-patch+json ;charset=UTF-8": true,
		"application/merge-patch+jsonx":               false,
		"application/merge-patch+json-but-not-really": false,
		"application/merge-patch+json; charset=\"utf": false,
		"application/json":                            false,
	}

	for contentType, accepted := range cases {
		t.Run(contentType, func(t *testing.T) {
			c := cache.New(nil)
			c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{
				Labels: map[string]string{"a": "b"},
			}})
			h := newTestHandlers(t, withCache(c), withWriteClient(&mockWriteClient{}))

			for name, handler := range map[string]http.HandlerFunc{
				"ports":  h.HandlePatchServicePorts,
				"labels": h.HandlePatchServiceLabels,
			} {
				req := httptest.NewRequest(
					http.MethodPatch,
					"/services/svc1/"+name,
					strings.NewReader(`{}`),
				)
				req.Header.Set("Content-Type", contentType)
				req.SetPathValue("id", "svc1")
				w := httptest.NewRecorder()
				handler(w, req)

				if refused := w.Code == http.StatusUnsupportedMediaType; refused == accepted {
					t.Errorf("%s: status = %d, accepted = %v", name, w.Code, accepted)
				}
			}
		})
	}
}
