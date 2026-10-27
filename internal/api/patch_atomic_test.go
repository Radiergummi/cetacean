package api

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/spec"
)

// The first operation applies and the second cannot, so a partial apply shows
// up as a write reaching Docker or as the live label set changing underneath.
func TestPatchLabels_LaterFailingOpChangesNothing(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc5789/patch-is-atomic",
		"http/rfc5789/a-failed-patch-changes-nothing",
	)

	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: "web", Labels: map[string]string{"a": "1"}},
	}})

	live := map[string]string{"a": "1"}
	written := false
	wc := &mockWriteClient{}
	wc.simulatedLabels = live
	wc.updateServiceLabelsFn = func(
		_ context.Context, id string, _ map[string]string,
	) (swarm.Service, error) {
		written = true
		return swarm.Service{ID: id}, nil
	}

	h := newTestHandlers(t, withCache(c), withWriteClient(wc),
		withOpsLevel(config.OpsConfiguration))

	body := `[{"op":"add","path":"/b","value":"2"},{"op":"remove","path":"/missing"}]`
	req := httptest.NewRequest(http.MethodPatch, "/services/svc1/labels", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()

	h.HandlePatchServiceLabels(w, req)

	if w.Code < 400 || w.Code >= 500 {
		t.Fatalf("status=%d, want 4xx; body: %s", w.Code, w.Body.String())
	}

	if written {
		t.Error("the update reached Docker although the patch failed")
	}

	if want := map[string]string{"a": "1"}; !maps.Equal(live, want) {
		t.Errorf("live labels = %v, want %v untouched", live, want)
	}
}
