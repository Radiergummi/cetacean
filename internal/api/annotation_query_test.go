package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/spec"
)

func listedIDs(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	var body struct {
		Items []struct {
			ID string `json:"@id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	ids := []string{}
	for _, item := range body.Items {
		ids = append(ids, strings.TrimPrefix(item.ID, "/annotations/"))
	}

	return ids
}

func TestListAnnotationsFiltersByTagsAndAuthor(t *testing.T) {
	h := annotationHandlers(t, true)
	both, _ := createAnnotation(t, h, "carol", `{"text":"a","tags":["deploy","web"]}`)
	deploy, _ := createAnnotation(t, h, "carol", `{"text":"b","tags":["deploy"]}`)
	byAlice, _ := createAnnotation(t, h, "alice",
		`{"text":"c","tags":["web"],"resources":[{"type":"service","id":"web"}]}`)

	for _, tt := range []struct {
		query string
		want  []string
	}{
		{"tag=deploy&tag=web", []string{both}},
		{"tag=deploy&tag=web&matchAny=true", []string{byAlice, deploy, both}},
		{"author=alice", []string{byAlice}},
		{"author=carol&tag=deploy", []string{deploy, both}},
		{"author=bob&author=alice", []string{byAlice}},
		{"tag=", []string{byAlice, deploy, both}},
	} {
		w := httptest.NewRecorder()
		h.HandleAnnotations(w, newAnnotationRequest("GET", "/annotations?"+tt.query, "carol", ""))

		if got := listedIDs(t, w); !slices.Equal(got, tt.want) {
			t.Errorf("?%s lists %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestListAnnotationsRefusals(t *testing.T) {
	on := annotationHandlers(t, true)
	off := annotationHandlers(t, false)

	for _, tt := range []struct {
		name   string
		h      *Handlers
		query  string
		status int
		code   string
	}{
		{"annotations off", off, "", http.StatusConflict, "ANN003"},
		{"a kind", on, "kind=change", http.StatusBadRequest, "API016"},
		{"tasks", on, "tasks=false", http.StatusBadRequest, "API016"},
		{"matchAny not a boolean", on, "tag=a&matchAny=maybe", http.StatusBadRequest, "API016"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.h.HandleAnnotations(
				w,
				newAnnotationRequest("GET", "/annotations?"+tt.query, "carol", ""),
			)

			if w.Code != tt.status || !strings.Contains(w.Body.String(), tt.code) {
				t.Fatalf("status %d %s, want %d %s", w.Code, w.Body, tt.status, tt.code)
			}
		})
	}
}

func TestHistoryAnswersAnEmptyListWhenAnnotationsAreOff(t *testing.T) {
	h := annotationHandlers(t, false)

	w := httptest.NewRecorder()
	h.HandleHistory(w, newAnnotationRequest("GET", "/history?kind=annotation", "carol", ""))

	if got := listedIDs(t, w); len(got) != 0 {
		t.Fatalf("history lists %v, want nothing", got)
	}
}

func TestHistoryRefusesAKindItDoesNotKnow(t *testing.T) {
	h := annotationHandlers(t, true)

	w := httptest.NewRecorder()
	h.HandleHistory(w, newAnnotationRequest(
		"GET", "/history?kind=change&kind=annotations", "carol", "",
	))

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `\"annotations\"`) {
		t.Fatalf("status %d %s, want 400 naming the unknown kind", w.Code, w.Body)
	}
}

func TestHistoryTakesSeveralKinds(t *testing.T) {
	h := annotationHandlers(t, true)
	note, _ := createAnnotation(t, h, "carol", `{"text":"a"}`)

	w := httptest.NewRecorder()
	h.HandleHistory(w, newAnnotationRequest(
		"GET", "/history?kind=change&kind=annotation", "carol", "",
	))

	got := listedIDs(t, w)
	if len(got) < 2 || got[0] != note {
		t.Fatalf("history lists %v, want the note above the changes", got)
	}
}

func TestTimelinesLinkAnnotationsForWhoMayCreateOne(t *testing.T) {
	h := annotationHandlers(t, true)

	for _, tt := range []struct {
		subject string
		want    bool
	}{
		{"alice", true},
		{"bob", false},
	} {
		for _, list := range []struct {
			path    string
			handler http.HandlerFunc
		}{
			{"/history", h.HandleHistory},
			{"/annotations", h.HandleAnnotations},
		} {
			w := httptest.NewRecorder()
			list.handler(w, newAnnotationRequest("GET", list.path, tt.subject, ""))

			linked := strings.Contains(w.Header().Get("Link"), `</annotations>; rel="edit"`)
			if linked != tt.want {
				t.Errorf("%s %s: Link = %q, want rel=edit %v",
					tt.subject, list.path, w.Header().Get("Link"), tt.want)
			}
		}
	}
}

func TestAnnotationTagsCountWhatTheCallerMayRead(t *testing.T) {
	h := annotationHandlers(t, true)
	createAnnotation(t, h, "carol", `{"text":"a","tags":["deploy","web"]}`)
	createAnnotation(t, h, "carol", `{"text":"b","tags":["deploy"]}`)
	createAnnotation(t, h, "carol",
		`{"text":"c","tags":["kernel"],"resources":[{"type":"node","id":"worker"}]}`)

	tags := func(subject, query string) (map[string]int, []string, int) {
		w := httptest.NewRecorder()
		h.HandleAnnotationTags(w, newAnnotationRequest(
			"GET", "/annotations/tags?"+query, subject, "",
		))
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}

		var body struct {
			Items []struct {
				ID    string `json:"@id"`
				Type  string `json:"@type"`
				Tag   string `json:"tag"`
				Count int    `json:"count"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}

		counts, order := map[string]int{}, []string{}
		for _, item := range body.Items {
			if item.Type != "AnnotationTag" || item.ID != "/annotations?tag="+item.Tag {
				t.Errorf("item %+v, want an AnnotationTag linking its list", item)
			}
			counts[item.Tag] = item.Count
			order = append(order, item.Tag)
		}

		return counts, order, body.Total
	}

	if counts, order, _ := tags(
		"carol",
		"",
	); !slices.Equal(
		order,
		[]string{"deploy", "kernel", "web"},
	) ||
		counts["deploy"] != 2 {
		t.Errorf("carol's tags = %v %v, want deploy twice, then by name", order, counts)
	}

	// Bob may read the service alone, so no note is his to count: every one is
	// cluster-wide or on the node.
	if counts, _, _ := tags("bob", ""); counts["kernel"] != 0 {
		t.Errorf("bob's tags = %v, want the node's note left out", counts)
	}

	if _, order, _ := tags("carol", "q=k"); !slices.Equal(order, []string{"kernel"}) {
		t.Errorf("tags starting with k = %v", order)
	}

	if _, order, total := tags("carol", "limit=1"); len(order) != 1 || total != 3 {
		t.Errorf("limit 1 = %v of %d, want one of three", order, total)
	}

	w := httptest.NewRecorder()
	h.HandleAnnotationTags(w, newAnnotationRequest("GET", "/annotations/tags?limit=1", "carol", ""))
	if !strings.Contains(w.Header().Get("Link"), `rel="next"`) {
		t.Errorf("limit 1: Link = %q, want a next page", w.Header().Get("Link"))
	}

	w = httptest.NewRecorder()
	h.HandleAnnotationTags(w, newAnnotationRequest("GET", "/annotations/tags?limit=0", "carol", ""))
	if w.Code != http.StatusBadRequest {
		t.Errorf("limit 0 = %d, want 400", w.Code)
	}
}

func TestAnAnnotationMarksAResourceRemovedAndChangesItsETag(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/an-etag-is-sent-when-changes-are-detectable")

	h := annotationHandlers(t, true)
	id, created := createAnnotation(t, h, "carol",
		`{"text":"a","resources":[{"type":"service","id":"web"},{"type":"node","id":"worker"}]}`)

	get := func() (*httptest.ResponseRecorder, []cache.AnnotationResource) {
		w := serveAnnotation(h.HandleGetAnnotation,
			newAnnotationRequest("GET", "/annotations/"+id, "carol", ""))

		var body struct {
			Annotation cache.Annotation `json:"annotation"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}

		return w, body.Annotation.Resources
	}

	if w, resources := get(); w.Header().Get("ETag") != created ||
		resources[0].Removed || resources[1].Removed {
		t.Fatalf("before = %s %+v, want the created version and nothing removed",
			w.Header().Get("ETag"), resources)
	}

	h.cache.DeleteService("svc1")

	w, resources := get()
	if !resources[0].Removed || resources[1].Removed {
		t.Fatalf("resources = %+v, want the service alone removed", resources)
	}
	if etag := w.Header().Get("ETag"); etag == created {
		t.Fatalf("ETag = %s after a removal, want it changed", etag)
	}

	// The validator a write is checked against is the one a read answers.
	patch := newAnnotationRequest("PATCH", "/annotations/"+id, "carol", `{"text":"b"}`)
	patch.Header.Set("Content-Type", "application/merge-patch+json")
	patch.Header.Set("If-Match", created)
	if w := serveAnnotation(
		h.HandlePatchAnnotation,
		patch,
	); w.Code != http.StatusPreconditionFailed {
		t.Fatalf("PATCH with the old ETag = %d, want 412", w.Code)
	}

	patch = newAnnotationRequest("PATCH", "/annotations/"+id, "carol", `{"text":"b"}`)
	patch.Header.Set("Content-Type", "application/merge-patch+json")
	patch.Header.Set("If-Match", w.Header().Get("ETag"))
	if w := serveAnnotation(h.HandlePatchAnnotation, patch); w.Code != http.StatusOK {
		t.Fatalf("PATCH with the current ETag = %d %s, want 200", w.Code, w.Body)
	}
}

func TestAnnotationFeedsListAnnotationsAlone(t *testing.T) {
	h := annotationHandlers(t, true)
	note, _ := createAnnotation(t, h, "carol", `{"text":"deploy"}`)

	w := httptest.NewRecorder()
	h.feedAnnotationsHandler(renderJSONFeed)(w,
		newAnnotationRequest("GET", "/annotations.json", "carol", ""))

	var feed struct {
		Title string `json:"title"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}
	if feed.Title != "Annotations" || len(feed.Items) != 1 ||
		!strings.Contains(feed.Items[0].ID, note) {
		t.Fatalf("feed = %+v, want the one annotation", feed)
	}

	for _, query := range []string{"kind=change", "tasks=false"} {
		w = httptest.NewRecorder()
		h.feedAnnotationsHandler(renderAtom)(w,
			newAnnotationRequest("GET", "/annotations.atom?"+query, "carol", ""))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s on the feed = %d, want 400", query, w.Code)
		}
	}
}
