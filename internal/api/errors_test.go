package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/spec"
)

func TestHandleErrorIndex_JSON(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/errors", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	HandleErrorIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type=%s, want application/json", ct)
	}

	var resp CollectionResponse[ErrorDef]
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Items) != len(errorRegistry) {
		t.Errorf("got %d errors, want %d", len(resp.Items), len(errorRegistry))
	}
}

func TestHandleErrorDetail_JSON(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc9457/type-uri-dereferences-to-documentation",
		"http/rfc9457/type-uri-explains-the-resolution",
	)

	req := httptest.NewRequest("GET", "/api/errors/NOD001", nil)
	req.SetPathValue("code", "NOD001")
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	HandleErrorDetail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}

	var def ErrorDef
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if def.Code != "NOD001" {
		t.Errorf("code=%s, want NOD001", def.Code)
	}
	if def.Status != http.StatusConflict {
		t.Errorf("status=%d, want 409", def.Status)
	}
}

func TestHandleErrorDetail_NotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/errors/XXX999", nil)
	req.SetPathValue("code", "XXX999")
	w := httptest.NewRecorder()

	HandleErrorDetail(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestWriteErrorCode_KnownCode(t *testing.T) {
	spec.Satisfies(t,
		"http/rfc9457/status-matches-the-response",
		"http/rfc9457/type-uri-includes-the-full-path",
		"http/rfc9457/instance-uri-includes-the-full-path",
		"http/rfc9457/title-is-stable-per-type",
		"http/rfc9457/extension-member-names",
	)

	req := httptest.NewRequest("DELETE", "/nodes/node1", nil)
	w := httptest.NewRecorder()

	writeErrorCode(w, req, "NOD001", "node xyz is not down and can't be removed")

	if w.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["type"] != "/api/errors/NOD001" {
		t.Errorf("type=%v, want /api/errors/NOD001", body["type"])
	}
	if body["title"] != "Node Not Down" {
		t.Errorf("title=%v, want Node Not Down", body["title"])
	}
	if body["detail"] != "node xyz is not down and can't be removed" {
		t.Errorf("detail=%v", body["detail"])
	}

	// The body's status and the response's must agree, or generic HTTP software
	// that ignores this format behaves differently from one that reads it.
	if body["status"] != float64(http.StatusConflict) {
		t.Errorf("body status=%v, want %d", body["status"], http.StatusConflict)
	}

	// The instance names the request that failed, by full path.
	if inst, _ := body["instance"].(string); !strings.HasPrefix(inst, "/nodes/node1") {
		t.Errorf("instance=%v, want the request path", body["instance"])
	}

	// The title is the registry's, so it does not vary between occurrences.
	if body["title"] != errorRegistry["NOD001"].Title {
		t.Errorf("title=%v, want the registered title", body["title"])
	}

	// Every member outside RFC 9457's own set is an extension, and §4 wants a
	// name starting with a letter. @context is the one exception this API makes.
	defined := map[string]bool{
		"type": true, "title": true, "status": true,
		"detail": true, "instance": true,
	}
	for name := range body {
		if defined[name] || name == "@context" {
			continue
		}
		if !extensionName.MatchString(name) {
			t.Errorf("extension member %q does not follow RFC 9457 §4's naming rule", name)
		}
	}
}

// ALPHA followed by ALPHA / DIGIT / "_", three characters or longer.
var extensionName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{2,}$`)

func TestWriteErrorCode_UnknownCode(t *testing.T) {
	spec.Satisfies(t, "http/rfc9457/about-blank-title-is-the-status-phrase")

	req := httptest.NewRequest("DELETE", "/test", nil)
	w := httptest.NewRecorder()

	writeErrorCode(w, req, "XXX999", "something went wrong")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// An untyped problem falls back to about:blank, whose title is the status
	// phrase — there is no type URI to look a better one up under.
	if body["type"] != "about:blank" {
		t.Errorf("type=%v, want about:blank", body["type"])
	}
	if body["title"] != http.StatusText(http.StatusInternalServerError) {
		t.Errorf("title=%v, want the status phrase", body["title"])
	}
}

func TestErrorDomainsMatchTheRegistry(t *testing.T) {
	labelled := make(map[string]bool, len(ErrorDomains))

	for _, domain := range ErrorDomains {
		if labelled[domain.Prefix] {
			t.Errorf("ErrorDomains lists %q twice", domain.Prefix)
		}
		if domain.Label == "" {
			t.Errorf("domain %q has no label", domain.Prefix)
		}

		labelled[domain.Prefix] = true
	}

	used := make(map[string]bool, len(ErrorDomains))

	for code := range errorRegistry {
		if len(code) != 6 {
			t.Errorf("code %q is not three letters and three digits", code)
			continue
		}

		prefix := code[:3]
		used[prefix] = true

		if !labelled[prefix] {
			t.Errorf("code %s uses prefix %q, which ErrorDomains does not name", code, prefix)
		}
	}

	for _, domain := range ErrorDomains {
		if !used[domain.Prefix] {
			t.Errorf("ErrorDomains names %q, which no error code uses", domain.Prefix)
		}
	}
}

func TestErrorDefsAreCompleteAndSorted(t *testing.T) {
	spec.Satisfies(t, "http/rfc9457/new-problem-types-are-documented")

	defs := ErrorDefs()

	if len(defs) != len(errorRegistry) {
		t.Fatalf("ErrorDefs returned %d entries, want %d", len(defs), len(errorRegistry))
	}

	for i, def := range defs {
		if def.Code == "" || def.Title == "" || def.Description == "" || def.Suggestion == "" {
			t.Errorf("entry %d (%q) has an empty field: %+v", i, def.Code, def)
		}
		if def.Status < 400 || def.Status > 599 {
			t.Errorf("%s has status %d, want a 4xx or 5xx", def.Code, def.Status)
		}
		if i > 0 && defs[i-1].Code >= def.Code {
			t.Errorf("ErrorDefs is not sorted: %q precedes %q", defs[i-1].Code, def.Code)
		}
	}
}

// Pins the other half of the SSE connection-limit contract. internal/api/sse
// sets Retry-After and asks for the SSE001 code, but cannot see the status that
// code maps to: the writer lives here, and sse cannot import this package. Both
// endpoints document a 429, so the mapping is asserted rather than assumed.
func TestWriteErrorCodeSSE001Is429(t *testing.T) {
	for _, code := range []string{"SSE001", "LOG001"} {
		t.Run(code, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/events", nil)
			w := httptest.NewRecorder()

			writeErrorCode(w, req, code, "too many connections")

			if w.Code != http.StatusTooManyRequests {
				t.Errorf("status=%d, want 429", w.Code)
			}

			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if body["type"] != "/api/errors/"+code {
				t.Errorf("type=%v, want /api/errors/%s", body["type"], code)
			}
		})
	}
}
