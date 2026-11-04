package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/spec"
)

// serveAnnotationEdit serves r through the chain the router puts in front of
// an annotation edit, preconditions included.
func serveAnnotationEdit(
	h *Handlers,
	handler http.HandlerFunc,
	r *http.Request,
) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.annotationEdit().ThenFunc(handler).ServeHTTP(w, r)

	return w
}

// annotationEditAs sends a PATCH (with a body) or a DELETE to a fresh note of
// alice's on the service, with the headers given, and returns the handlers,
// the note's id and ETag, and the answer.
func annotationEditAs(
	t *testing.T,
	subject, method, contentType, body string,
	headers func(etag string) map[string]string,
) (*Handlers, string, *httptest.ResponseRecorder) {
	t.Helper()

	h := annotationHandlers(t, true)
	id, etag := createAnnotation(t, h, "alice",
		`{"text":"x","resources":[{"type":"service","id":"web"}]}`)

	r := newAnnotationRequest(method, "/annotations/"+id, subject, body)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for name, value := range headers(etag) {
		r.Header.Set(name, value)
	}

	handler := h.HandlePatchAnnotation
	if method == http.MethodDelete {
		handler = h.HandleDeleteAnnotation
	}

	return h, id, serveAnnotationEdit(h, handler, r)
}

func patchAnnotationWith(
	t *testing.T,
	headers func(etag string) map[string]string,
) (*Handlers, string, *httptest.ResponseRecorder) {
	t.Helper()

	return annotationEditAs(t, "alice", http.MethodPatch, AcceptMergePatch,
		`{"text":"y"}`, headers)
}

func assertAnnotationText(t *testing.T, h *Handlers, id, want string) {
	t.Helper()

	if a, _ := h.cache.Annotation(mustParseID(t, id)); a.Text != want {
		t.Errorf("text = %q, want %q", a.Text, want)
	}
}

func TestAnnotationPreconditionsComeBeforeTheBody(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/preconditions-are-evaluated-before-the-action")

	for _, tt := range []struct{ name, contentType, body string }{
		{"an unknown member", AcceptMergePatch, `{"bogus":1}`},
		{"the wrong media type", "application/json", `{"text":"y"}`},
		{"an invalid edit", AcceptMergePatch, `{"text":""}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, w := annotationEditAs(t, "alice", http.MethodPatch, tt.contentType, tt.body,
				func(string) map[string]string { return map[string]string{"If-Match": `"999999"`} })
			if w.Code != http.StatusPreconditionFailed {
				t.Errorf("stale If-Match with %s: %d %s, want 412", tt.name, w.Code, w.Body)
			}
		})
	}
}

func TestAnnotationPreconditionsYieldToARefusal(t *testing.T) {
	spec.Satisfies(t, "http/rfc9110/preconditions-are-ignored-when-the-answer-is-not-2xx")

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		_, _, w := annotationEditAs(t, "bob", method, AcceptMergePatch, `{"text":"y"}`,
			func(string) map[string]string { return map[string]string{"If-Match": `"999999"`} })
		if w.Code != http.StatusForbidden {
			t.Errorf("%s by a reader with a stale If-Match: %d, want 403", method, w.Code)
		}
	}
}

func TestAnnotationWritesHonourIfNoneMatch(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc9110/if-none-match-is-evaluated-before-the-method",
		"http/rfc9110/a-false-if-none-match-answers-304",
		"http/rfc9110/if-none-match-uses-weak-comparison",
	)

	for name, tag := range map[string]func(string) string{
		"the current tag": func(etag string) string { return etag },
		"any":             func(string) string { return "*" },
		"its weak form":   func(etag string) string { return "W/" + etag },
		"a coded form": func(etag string) string {
			return strings.TrimSuffix(etag, `"`) + `-gzip"`
		},
		"one in a list": func(etag string) string { return `"999999", ` + etag },
	} {
		t.Run(name, func(t *testing.T) {
			h, id, w := patchAnnotationWith(t, func(etag string) map[string]string {
				return map[string]string{"If-None-Match": tag(etag)}
			})
			if w.Code != http.StatusPreconditionFailed {
				t.Errorf("If-None-Match: %s = %d %s, want 412", name, w.Code, w.Body)
			}
			assertAnnotationText(t, h, id, "x")
		})
	}

	t.Run("another tag", func(t *testing.T) {
		h, id, w := patchAnnotationWith(t, func(string) map[string]string {
			return map[string]string{"If-None-Match": `"999999"`}
		})
		if w.Code != http.StatusOK {
			t.Errorf("If-None-Match on another version = %d %s, want 200", w.Code, w.Body)
		}
		assertAnnotationText(t, h, id, "y")
	})
}

func TestAnnotationWritesHonourIfUnmodifiedSince(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc9110/if-unmodified-since-is-evaluated",
		"http/rfc9110/a-false-if-unmodified-since-stops-the-method",
	)

	before := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	after := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		h, id, w := annotationEditAs(
			t,
			"alice",
			method,
			AcceptMergePatch,
			`{"text":"y"}`,
			func(string) map[string]string { return map[string]string{"If-Unmodified-Since": before} },
		)
		if w.Code != http.StatusPreconditionFailed {
			t.Errorf("%s since before its last edit = %d, want 412", method, w.Code)
		}
		assertAnnotationText(t, h, id, "x")
	}

	if _, _, w := patchAnnotationWith(t, func(string) map[string]string {
		return map[string]string{"If-Unmodified-Since": after}
	}); w.Code != http.StatusOK {
		t.Errorf("PATCH since after its last edit = %d %s, want 200", w.Code, w.Body)
	}
}

func TestAnnotationPreconditionsAreEvaluatedInOrder(t *testing.T) {
	t.Run("If-Match passes on to If-None-Match", func(t *testing.T) {
		spec.Satisfies(t, "http/rfc9110/preconditions-are-evaluated-in-order")

		_, _, w := patchAnnotationWith(t, func(etag string) map[string]string {
			return map[string]string{"If-Match": etag, "If-None-Match": etag}
		})
		if w.Code != http.StatusPreconditionFailed {
			t.Errorf("a true If-Match and a false If-None-Match = %d, want 412", w.Code)
		}
	})

	t.Run("If-Match overrides If-Unmodified-Since", func(t *testing.T) {
		spec.Satisfies(t, "http/rfc9110/if-unmodified-since-yields-to-if-match")

		before := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
		_, _, w := patchAnnotationWith(t, func(etag string) map[string]string {
			return map[string]string{"If-Match": etag, "If-Unmodified-Since": before}
		})
		if w.Code != http.StatusOK {
			t.Errorf("a true If-Match and a stale If-Unmodified-Since = %d, want 200", w.Code)
		}
	})
}

// A removal changes an annotation's representation at a time nothing records,
// so from then on it has no modification date for a precondition to compare.
func TestAnAnnotationARemovalMarkedHasNoModificationDate(t *testing.T) {
	h := annotationHandlers(t, true)
	id, _ := createAnnotation(t, h, "carol",
		`{"text":"x","resources":[{"type":"service","id":"web"},{"type":"node","id":"node1"}]}`)

	w := httptest.NewRecorder()
	h.HandleGetAnnotation(w, newAnnotationRequest("GET", "/annotations/"+id, "carol", ""))
	lastModified := w.Header().Get("Last-Modified")

	h.cache.DeleteNode("node1")

	t.Run("If-Modified-Since", func(t *testing.T) {
		spec.Satisfies(t, "http/rfc9110/if-modified-since-needs-a-modification-date")

		r := newAnnotationRequest("GET", "/annotations/"+id, "carol", "")
		r.Header.Set("If-Modified-Since", lastModified)

		w := httptest.NewRecorder()
		h.HandleGetAnnotation(w, r)
		if w.Code != http.StatusOK || w.Header().Get("Last-Modified") != "" ||
			!strings.Contains(w.Body.String(), `"removed":true`) {
			t.Errorf("after a removal: %d, Last-Modified %q; want 200 with the removal and no date",
				w.Code, w.Header().Get("Last-Modified"))
		}
	})

	t.Run("If-Unmodified-Since", func(t *testing.T) {
		spec.Satisfies(t, "http/rfc9110/if-unmodified-since-needs-a-modification-date")

		r := newAnnotationRequest("PATCH", "/annotations/"+id, "carol", `{"text":"y"}`)
		r.Header.Set("Content-Type", AcceptMergePatch)
		r.Header.Set(
			"If-Unmodified-Since",
			time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat),
		)
		if w := serveAnnotationEdit(h, h.HandlePatchAnnotation, r); w.Code != http.StatusOK {
			t.Errorf(
				"If-Unmodified-Since with no date to compare = %d %s, want 200",
				w.Code,
				w.Body,
			)
		}
	})
}

func TestAnEmptyMergePatchChangesNothing(t *testing.T) {
	h := annotationHandlers(t, true)
	id, etag := createAnnotation(t, h, "alice",
		`{"text":"x","tags":["deploy"],"resources":[{"type":"service","id":"web"}]}`)
	count := h.cache.History().Count()

	for _, body := range []string{`{}`, `{"text":"x","tags":["deploy"]}`} {
		r := newAnnotationRequest("PATCH", "/annotations/"+id, "carol", body)
		r.Header.Set("Content-Type", AcceptMergePatch)

		w := serveAnnotationEdit(h, h.HandlePatchAnnotation, r)
		if w.Code != http.StatusOK || w.Header().Get("ETag") != etag ||
			strings.Contains(w.Body.String(), `"editor"`) {
			t.Errorf("PATCH %s = %d, ETag %s, %s; want 200 under %s with no editor",
				body, w.Code, w.Header().Get("ETag"), w.Body, etag)
		}
	}

	if got := h.cache.History().Count(); got != count {
		t.Errorf("history moved from %d to %d, want no item for an edit that changed nothing",
			count, got)
	}
}
