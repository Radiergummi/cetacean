package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/spec"
)

// annotationHandlers serves service "web" in stack "shop" and node "worker".
// alice writes the service, bob only reads it, and carol writes everything.
func annotationHandlers(t *testing.T, timeline bool) *Handlers {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{ID: "svc1", Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{
		Name: "web", Labels: map[string]string{cache.StackLabel: "shop"},
	}}})
	c.SetNode(swarm.Node{ID: "node1", Description: swarm.NodeDescription{Hostname: "worker"}})
	if timeline {
		c.RestoreAnnotations(nopAnnotationStore{}, nil)
	}

	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources:   []string{"service:web"},
			Audience:    []string{"user:alice"},
			Permissions: []string{"write"},
		},
		{
			Resources:   []string{"service:web"},
			Audience:    []string{"user:bob"},
			Permissions: []string{"read"},
		},
		{
			Resources:   []string{"*"},
			Audience:    []string{"user:carol"},
			Permissions: []string{"write"},
		},
		{
			Resources:   []string{"service:web", "config:*"},
			Audience:    []string{"user:dave"},
			Permissions: []string{"read"},
		},
		{
			Resources:   []string{"config:*"},
			Audience:    []string{"user:dave"},
			Permissions: []string{"write"},
		},
	}})

	return newTestHandlers(t, withCache(c), withACL(e))
}

func newAnnotationRequest(method, path, subject, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: subject}))
	if id, ok := strings.CutPrefix(path, "/annotations/"); ok {
		r.SetPathValue("id", id)
	}

	return r
}

func serveAnnotation(handler http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	attributeWrite(handler).ServeHTTP(w, r)

	return w
}

func createAnnotation(t *testing.T, h *Handlers, subject, body string) (string, string) {
	t.Helper()

	r := newAnnotationRequest("POST", "/annotations", subject, body)
	r.Header.Set("Content-Type", "application/json")

	w := serveAnnotation(h.HandleCreateAnnotation, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201", w.Code, w.Body)
	}

	return strings.TrimPrefix(w.Header().Get("Location"), "/annotations/"), w.Header().Get("ETag")
}

func TestCreateAnnotationAnswersItsLocationAndVersion(t *testing.T) {
	h := annotationHandlers(t, true)

	r := newAnnotationRequest("POST", "/annotations", "alice",
		`{"text":"load test","resources":[{"type":"service","id":"web"}]}`)
	w := serveAnnotation(h.HandleCreateAnnotation, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d %s, want 201", w.Code, w.Body)
	}

	var body struct {
		ID         string           `json:"@id"`
		Type       string           `json:"@type"`
		Annotation cache.Annotation `json:"annotation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	location := w.Header().Get("Location")
	if location == "" || body.ID != location || body.Type != "Annotation" {
		t.Errorf("Location = %q, @id = %q, @type = %q; want the annotation's own path",
			location, body.ID, body.Type)
	}
	if etag := w.Header().Get("ETag"); etag != `"`+strings.TrimPrefix(
		location,
		"/annotations/",
	)+`"` {
		t.Errorf("ETag = %s, want the creation ID as the first version", etag)
	}
	if body.Annotation.Actor == nil || body.Annotation.Actor.Subject != "alice" ||
		body.Annotation.Tags == nil {
		t.Errorf("annotation = %+v, want alice as author and tags as []", body.Annotation)
	}
	if !strings.Contains(w.Body.String(), `"tags":[]`) {
		t.Errorf("body = %s, want empty tags as []", w.Body)
	}
}

func TestCreateAnnotationRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		timeline      bool
		subject, body string
		status        int
		code          string
	}{
		"unknown member": {true, "alice", `{"text":"x","colour":"red"}`, 400, "ANN001"},
		"no text":        {true, "alice", `{"resources":[{"type":"service","id":"web"}]}`, 400, "ANN001"},
		"reader":         {true, "bob", `{"text":"x","resources":[{"type":"service","id":"web"}]}`, 403, "ACL002"},
		"cluster-wide":   {true, "alice", `{"text":"x"}`, 403, "ACL002"},
		"timeline off":   {false, "carol", `{"text":"x"}`, 409, "ANN003"},
	} {
		h := annotationHandlers(t, tc.timeline)

		w := serveAnnotation(h.HandleCreateAnnotation,
			newAnnotationRequest("POST", "/annotations", tc.subject, tc.body))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Errorf("%s: %d %s, want %d %s", name, w.Code, w.Body, tc.status, tc.code)
		}
	}
}

func TestGetAnnotationAllowsWritesToWhoMayMakeThem(t *testing.T) {
	h := annotationHandlers(t, true)
	id, etag := createAnnotation(t, h, "alice",
		`{"text":"x","resources":[{"type":"service","id":"svc1"}]}`)

	for subject, want := range map[string]string{
		"alice": "GET, HEAD, PATCH, DELETE",
		"carol": "GET, HEAD, PATCH, DELETE",
		"bob":   "GET, HEAD",
	} {
		w := httptest.NewRecorder()
		h.HandleGetAnnotation(w, newAnnotationRequest("GET", "/annotations/"+id, subject, ""))

		if w.Code != http.StatusOK || w.Header().Get("Allow") != want {
			t.Errorf(
				"%s: %d, Allow = %q; want 200 and %q",
				subject,
				w.Code,
				w.Header().Get("Allow"),
				want,
			)
		}
		if w.Header().Get("ETag") != etag {
			t.Errorf("%s: ETag = %s, want %s", subject, w.Header().Get("ETag"), etag)
		}
	}

	w := httptest.NewRecorder()
	h.HandleGetAnnotation(w, newAnnotationRequest("GET", "/annotations/"+id, "mallory", ""))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "ANN004") {
		t.Errorf("a caller who cannot read it: %d %s, want 404 ANN004", w.Code, w.Body)
	}
}

func TestPatchAnnotationMergesAndHonoursIfMatch(t *testing.T) {
	h := annotationHandlers(t, true)
	id, etag := createAnnotation(t, h, "alice",
		`{"text":"x","tags":["deploy"],"until":"2030-01-01T00:00:00Z",`+
			`"resources":[{"type":"service","id":"web"}]}`)

	patch := func(subject, body, ifMatch, contentType string) *httptest.ResponseRecorder {
		r := newAnnotationRequest("PATCH", "/annotations/"+id, subject, body)
		r.Header.Set("Content-Type", contentType)
		if ifMatch != "" {
			r.Header.Set("If-Match", ifMatch)
		}

		return serveAnnotation(h.HandlePatchAnnotation, r)
	}

	if w := patch(
		"alice",
		`{"text":"y"}`,
		"",
		"application/json",
	); w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("plain JSON: %d, want 415", w.Code)
	}
	if w := patch("alice", `{"text":"y"}`, `"1"`, "application/merge-patch+json"); w.Code != 412 {
		t.Errorf("stale If-Match: %d %s, want 412", w.Code, w.Body)
	}
	if w := patch("alice", `{"text":null}`, "", "application/merge-patch+json"); w.Code != 400 {
		t.Errorf("null text: %d, want 400", w.Code)
	}
	if w := patch("bob", `{"text":"y"}`, "", "application/merge-patch+json"); w.Code != 403 {
		t.Errorf("a reader: %d, want 403", w.Code)
	}

	w := patch(
		"alice",
		`{"text":"y","tags":null,"until":null}`,
		etag,
		"application/merge-patch+json",
	)
	if w.Code != http.StatusOK {
		t.Fatalf("patch = %d %s, want 200", w.Code, w.Body)
	}

	var body struct {
		Annotation cache.Annotation `json:"annotation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	a := body.Annotation
	if a.Text != "y" || len(a.Tags) != 0 || !a.Until.IsZero() || a.Editor == nil ||
		len(a.Resources) != 1 {
		t.Errorf("patched = %+v, want new text, no tags, no until, an editor, the resource kept", a)
	}
	if got := w.Header().Get("ETag"); got == etag || got == "" {
		t.Errorf("ETag = %s, want a new version past %s", got, etag)
	}
}

func TestDeleteAnnotation(t *testing.T) {
	h := annotationHandlers(t, true)
	id, _ := createAnnotation(t, h, "alice",
		`{"text":"x","resources":[{"type":"service","id":"web"}]}`)

	if w := serveAnnotation(h.HandleDeleteAnnotation,
		newAnnotationRequest("DELETE", "/annotations/"+id, "bob", "")); w.Code != 403 {
		t.Errorf("a reader: %d, want 403", w.Code)
	}

	if w := serveAnnotation(h.HandleDeleteAnnotation,
		newAnnotationRequest("DELETE", "/annotations/"+id, "carol", "")); w.Code != 204 {
		t.Fatalf("a writer: %d %s, want 204", w.Code, w.Body)
	}

	w := httptest.NewRecorder()
	h.HandleGetAnnotation(w, newAnnotationRequest("GET", "/annotations/"+id, "carol", ""))
	if w.Code != http.StatusNotFound {
		t.Errorf("after delete: %d, want 404", w.Code)
	}
}

func TestServiceDetailLinksAnnotationsForAWriter(t *testing.T) {
	for subject, want := range map[string]bool{"alice": true, "bob": false} {
		h := annotationHandlers(t, true)

		r := newAnnotationRequest("GET", "/services/svc1", subject, "")
		r.SetPathValue("id", "svc1")

		w := httptest.NewRecorder()
		h.HandleGetService(w, r)

		linked := strings.Contains(strings.Join(w.Header().Values("Link"), ","),
			`</annotations>; rel="edit"`)
		if linked != want {
			t.Errorf(
				"%s: Link = %q, want annotations linked %v",
				subject,
				w.Header().Values("Link"),
				want,
			)
		}
	}
}

func TestAnnotationWritesHonourIfMatch(t *testing.T) {
	write := func(t *testing.T, ifMatch string) (*Handlers, string, int) {
		t.Helper()

		h := annotationHandlers(t, true)
		id, _ := createAnnotation(t, h, "alice",
			`{"text":"x","resources":[{"type":"service","id":"web"}]}`)

		r := newAnnotationRequest("PATCH", "/annotations/"+id, "alice", `{"text":"y"}`)
		r.Header.Set("Content-Type", "application/merge-patch+json")
		r.Header.Set("If-Match", ifMatch)

		return h, id, serveAnnotation(h.HandlePatchAnnotation, r).Code
	}

	t.Run("a false If-Match changes nothing", func(t *testing.T) {
		spec.Satisfies(t, "http/rfc9110/a-false-if-match-stops-the-method")

		h, id, status := write(t, `"999999"`)
		a, _ := h.cache.Annotation(mustParseID(t, id))
		if status != http.StatusPreconditionFailed || a.Text != "x" {
			t.Errorf("status = %d, text = %q; want 412 and the text unchanged", status, a.Text)
		}
	})

	t.Run("a weak tag never matches", func(t *testing.T) {
		spec.Satisfies(t, "http/rfc9110/if-match-uses-strong-comparison")

		h := annotationHandlers(t, true)
		id, etag := createAnnotation(t, h, "alice",
			`{"text":"x","resources":[{"type":"service","id":"web"}]}`)

		r := newAnnotationRequest("DELETE", "/annotations/"+id, "alice", "")
		r.Header.Set("If-Match", "W/"+etag)
		if w := serveAnnotation(
			h.HandleDeleteAnnotation,
			r,
		); w.Code != http.StatusPreconditionFailed {
			t.Errorf("weak If-Match: %d, want 412", w.Code)
		}
	})
}

func mustParseID(t *testing.T, id string) uint64 {
	t.Helper()

	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		t.Fatal(err)
	}

	return n
}

func TestGetAnnotationHonoursIfModifiedSince(t *testing.T) {
	h := annotationHandlers(t, true)
	id, _ := createAnnotation(t, h, "alice",
		`{"text":"x","resources":[{"type":"service","id":"web"}]}`)

	r := newAnnotationRequest("GET", "/annotations/"+id, "alice", "")
	r.Header.Set("If-Modified-Since", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))

	w := httptest.NewRecorder()
	h.HandleGetAnnotation(w, r)
	if w.Code != http.StatusNotModified || w.Header().Get("Last-Modified") == "" {
		t.Errorf(
			"status = %d, Last-Modified = %q; want 304 with the date",
			w.Code,
			w.Header().Get("Last-Modified"),
		)
	}
}

func TestListAnnotationsAllowsPostToWhoMayAnnotate(t *testing.T) {
	for _, tc := range []struct {
		timeline bool
		subject  string
		want     string
	}{
		{true, "alice", "GET, HEAD, POST"},
		{true, "carol", "GET, HEAD, POST"},
		{true, "bob", "GET, HEAD"},
		{true, "dave", "GET, HEAD"},
		{false, "alice", "GET, HEAD"},
	} {
		h := annotationHandlers(t, tc.timeline)

		for _, path := range []string{"/annotations", "/annotations.csv"} {
			r := newAnnotationRequest("GET", path, tc.subject, "")
			if strings.HasSuffix(path, ".csv") {
				r = withContentType(r, ContentTypeCSV)
			}

			w := httptest.NewRecorder()
			h.HandleAnnotations(w, r)

			if got := w.Header().Get("Allow"); got != tc.want {
				t.Errorf("%s %s (timeline %v): Allow = %q, want %q",
					tc.subject, path, tc.timeline, got, tc.want)
			}
		}
	}
}

func TestClusterLinksAnnotationsForClusterWideWriters(t *testing.T) {
	for _, tc := range []struct {
		timeline bool
		subject  string
		want     bool
	}{
		{true, "carol", true},
		{true, "alice", false},
		{true, "bob", false},
		{false, "carol", false},
	} {
		h := annotationHandlers(t, tc.timeline)
		w := httptest.NewRecorder()
		h.HandleCluster(w, newAnnotationRequest("GET", "/cluster", tc.subject, ""))

		got := strings.Contains(w.Header().Get("Link"), `</annotations>; rel="edit"`)
		if w.Code != http.StatusOK || got != tc.want {
			t.Errorf("%s (timeline %v): %d, links annotations = %v, want %v",
				tc.subject, tc.timeline, w.Code, got, tc.want)
		}
	}
}
