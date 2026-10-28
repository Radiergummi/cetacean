package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

// Resources use `type`, `status` and `detail` for values of their own, so the
// RFC 9457 mappings may only reach a problem document.
func TestOnlyProblemDocumentsMapTheProblemMembers(t *testing.T) {
	members := []string{"type", "title", "status", "detail", "instance"}

	var shared struct {
		Context map[string]json.RawMessage `json:"@context"`
	}
	if err := json.Unmarshal([]byte(jsonLDContextDoc), &shared); err != nil {
		t.Fatalf("context document: %v", err)
	}
	for _, term := range members {
		if _, ok := shared.Context[term]; ok {
			t.Errorf("the shared context maps %q for every response", term)
		}
	}

	w := httptest.NewRecorder()
	problem.WriteStatus(
		w,
		httptest.NewRequest(http.MethodGet, "/nodes/x", nil),
		http.StatusNotFound,
		"",
	)

	var body struct {
		Context []json.RawMessage `json:"@context"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("problem @context is not an array: %v\n%s", err, w.Body)
	}
	if len(body.Context) != 2 {
		t.Fatalf("problem @context = %s, want the shared URL and the problem terms", w.Body)
	}

	var url string
	if err := json.Unmarshal(body.Context[0], &url); err != nil || url != jsonLDContext {
		t.Errorf("problem @context[0] = %s, want %q", body.Context[0], jsonLDContext)
	}

	var terms map[string]json.RawMessage
	if err := json.Unmarshal(body.Context[1], &terms); err != nil {
		t.Fatalf("problem @context[1]: %v", err)
	}
	for _, term := range members {
		if _, ok := terms[term]; !ok {
			t.Errorf("a problem document does not map %q", term)
		}
	}
}
