package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/imagecheck"
)

const (
	pinnedDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	movedDigest  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pinnedImage  = "nginx:1.27@" + pinnedDigest
)

type fakeImageDigests struct {
	results    map[string]imagecheck.Result
	generation uint64
}

func (f *fakeImageDigests) LatestDigest(image string) (imagecheck.Result, bool) {
	r, ok := f.results[image]
	return r, ok
}

func (f *fakeImageDigests) Generation() uint64 { return f.generation }

func imageUpdateHandlers(t *testing.T, since time.Time) (*Handlers, *fakeImageDigests) {
	t.Helper()

	c := cache.New(nil)
	svc := swarm.Service{ID: "svc1"}
	svc.Spec.Name = "web"
	svc.Spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{Image: pinnedImage}
	svc.UpdatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.SetService(svc)

	digests := &fakeImageDigests{results: map[string]imagecheck.Result{
		pinnedImage: {Digest: movedDigest, CheckedAt: since, Since: since},
	}, generation: 1}

	h := newTestHandlers(t, withCache(c))
	h.SetImageDigests(digests)

	return h, digests
}

func TestServiceDetailReportsANewerImage(t *testing.T) {
	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	h, _ := imageUpdateHandlers(t, since)

	req := httptest.NewRequest(http.MethodGet, "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}

	var body ServiceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.ImageUpdate == nil || body.ImageUpdate.SuggestedImage != "nginx:1.27@"+movedDigest {
		t.Errorf("imageUpdate = %+v", body.ImageUpdate)
	}

	// Dated by the update, which is newer than the service, or a client
	// revalidating on the date would keep a copy without it.
	if got := w.Header().Get("Last-Modified"); got != since.Format(http.TimeFormat) {
		t.Errorf("Last-Modified = %q, want %q", got, since.Format(http.TimeFormat))
	}
}

func TestServiceListReportsANewerImage(t *testing.T) {
	h, _ := imageUpdateHandlers(t, time.Now())

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	w := httptest.NewRecorder()
	h.HandleListServices(w, req)

	var resp CollectionResponse[ServiceListItem]
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	if len(resp.Items) != 1 || resp.Items[0].ImageUpdate == nil {
		t.Fatalf("items = %+v", resp.Items)
	}

	if got := resp.Items[0].ImageUpdate.LatestDigest; got != movedDigest {
		t.Errorf("latestDigest = %q", got)
	}
}

func TestServiceListValidatorFollowsTheImageCheck(t *testing.T) {
	h, digests := imageUpdateHandlers(t, time.Now())

	list := func(inm string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/services", nil)
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}

		w := httptest.NewRecorder()
		h.HandleListServices(w, req)

		return w
	}

	etag := list("").Header().Get("ETag")
	digests.generation++

	if got := list(etag); got.Code == http.StatusNotModified {
		t.Error("a new image check answer revalidated as unchanged")
	}
}

func TestNoImageUpdateWithTheCheckOff(t *testing.T) {
	h, _ := imageUpdateHandlers(t, time.Now())
	h.SetImageDigests(nil)

	req := httptest.NewRequest(http.MethodGet, "/services/svc1", nil)
	req.SetPathValue("id", "svc1")
	w := httptest.NewRecorder()
	h.HandleGetService(w, req)

	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if _, ok := body["imageUpdate"]; ok {
		t.Error("imageUpdate reported with the check off")
	}
}
