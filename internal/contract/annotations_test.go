package contract

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func annotate(
	t *testing.T,
	w *World,
	persona, method, path, contentType, ifMatch, body string,
) annotationAnswer {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), method, w.Server.URL+path, strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	setPersonaHeaders(t, req, persona)

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	read, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read: %v", method, path, err)
	}

	return annotationAnswer{resp.StatusCode, resp.Header, read}
}

type annotationAnswer struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// The annotation routes hold the dev-auth personas to the design's rules: write
// on every named resource to create, and the author or a writer to change one.
func TestAnnotationsOverRESTFollowTheGrants(t *testing.T) {
	w := NewWorld(t)
	onService := `{"text":"load test","resources":[{"type":"service","id":"` + SeededServiceID + `"}]}`
	onNode := `{"text":"kernel update","resources":[{"type":"node","id":"` + SeededNodeID + `"}]}`

	if resp := annotate(
		t,
		w,
		"viewers",
		"POST",
		"/annotations",
		"application/json",
		"",
		onService,
	); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewers create: %d, want 403", resp.StatusCode)
	}
	if resp := annotate(
		t,
		w,
		"oncall",
		"POST",
		"/annotations",
		"application/json",
		"",
		onNode,
	); resp.StatusCode != http.StatusForbidden {
		t.Errorf("oncall create on a node it only reads: %d, want 403", resp.StatusCode)
	}

	created := annotate(t, w, "ops", "POST", "/annotations", "application/json", "", onService)
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("ops create: %d %s, want 201", created.StatusCode, created.Body)
	}
	path := created.Header.Get("Location")

	read := annotate(t, w, "viewers", "GET", path, "", "", "")
	if read.StatusCode != http.StatusOK || read.Header.Get("Allow") != "GET, HEAD" {
		t.Errorf(
			"viewers read: %d, Allow %q; want 200 and no writes",
			read.StatusCode,
			read.Header.Get("Allow"),
		)
	}

	current := annotate(t, w, "oncall", "GET", path, "", "", "")
	if current.Header.Get("Allow") != "GET, HEAD, PATCH, DELETE" {
		t.Errorf("oncall Allow = %q, want PATCH and DELETE offered", current.Header.Get("Allow"))
	}

	edited := annotate(t, w, "oncall", "PATCH", path, "application/merge-patch+json",
		current.Header.Get("ETag"), `{"text":"load test, stopped early"}`)
	if edited.StatusCode != http.StatusOK {
		t.Fatalf("oncall edit: %d, want 200", edited.StatusCode)
	}

	var body struct {
		Annotation struct {
			Text   string `json:"text"`
			Actor  struct{ Subject string }
			Editor struct{ Subject string }
		} `json:"annotation"`
	}
	if err := json.Unmarshal(edited.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Annotation.Actor.Subject != personas["ops"].Subject ||
		body.Annotation.Editor.Subject != personas["oncall"].Subject {
		t.Errorf("edited = %+v, want ops as author and oncall as editor", body.Annotation)
	}

	if resp := annotate(
		t,
		w,
		"viewers",
		"DELETE",
		path,
		"",
		"",
		"",
	); resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewers delete: %d, want 403", resp.StatusCode)
	}
	if resp := annotate(
		t,
		w,
		"ops",
		"DELETE",
		path,
		"",
		"",
		"",
	); resp.StatusCode != http.StatusNoContent {
		t.Errorf("ops delete: %d, want 204", resp.StatusCode)
	}
}
