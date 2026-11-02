package api

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	json "github.com/goccy/go-json"
)

func TestListAnnotationsKeepsWhatTheCallerMayRead(t *testing.T) {
	h := annotationHandlers(t, true)
	onService, _ := createAnnotation(t, h, "alice",
		`{"text":"load test","resources":[{"type":"service","id":"web"}]}`)
	clusterWide, _ := createAnnotation(t, h, "carol", `{"text":"maintenance"}`)
	createAnnotation(t, h, "carol",
		`{"text":"kernel","resources":[{"type":"node","id":"worker"}]}`)

	w := httptest.NewRecorder()
	h.HandleAnnotations(w, newAnnotationRequest("GET", "/annotations", "bob", ""))

	var body struct {
		Items []struct {
			ID   string `json:"@id"`
			Type string `json:"@type"`
			Kind string `json:"kind"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}

	var ids []string
	for _, item := range body.Items {
		if item.Type != "Annotation" || item.Kind != "annotation" {
			t.Errorf("item %+v, want an Annotation", item)
		}
		ids = append(ids, item.ID)
	}

	want := []string{"/annotations/" + clusterWide, "/annotations/" + onService}
	if !slices.Equal(ids, want) {
		t.Fatalf("bob lists %v, want %v", ids, want)
	}
}

func TestHistoryCSVCarriesAnAnnotationsTextAndTags(t *testing.T) {
	h := annotationHandlers(t, true)
	createAnnotation(t, h, "carol", `{"text":"=1+1","tags":["deploy","rollback"]}`)

	w := httptest.NewRecorder()
	h.HandleHistory(w, withContentType(
		newAnnotationRequest("GET", "/history?kind=annotation", "carol", ""), ContentTypeCSV,
	))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}

	records := csvRecords(t, w.Body.String())
	if got := records[1][12:]; !slices.Equal(got, []string{"'=1+1", "deploy,rollback"}) {
		t.Fatalf("text and tags = %q, want the formula neutralised and the tags joined", got)
	}
}
