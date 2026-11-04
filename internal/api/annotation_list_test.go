package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
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
			ID        string            `json:"@id"`
			Type      string            `json:"@type"`
			Kind      *string           `json:"kind"`
			Tags      []string          `json:"tags"`
			Resources []json.RawMessage `json:"resources"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body, err)
	}

	var ids []string
	for _, item := range body.Items {
		if item.Type != "Annotation" || item.Kind != nil || item.Tags == nil ||
			item.Resources == nil {
			t.Errorf("item %+v, want an Annotation in its detail shape", item)
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
	if got := records[1][12:]; !slices.Equal(got, []string{"'=1+1", "deploy,rollback", "cluster"}) {
		t.Fatalf(
			"text, tags and resources = %q, want the formula neutralised and the tags joined",
			got,
		)
	}
	if records[1][4] == "" {
		t.Fatalf("id = %q, want the annotation's", records[1][4])
	}
}

func TestUnpagedHistoryCSVKeepsTheRingAndEveryAnnotation(t *testing.T) {
	c := cache.New(nil, cache.WithHistorySize(3))
	c.RestoreAnnotations(nopAnnotationStore{}, nil)
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources: []string{"*"}, Audience: []string{"user:carol"}, Permissions: []string{"write"},
	}}})
	h := newTestHandlers(t, withCache(c), withACL(e))

	createAnnotation(t, h, "carol", `{"text":"one"}`)
	createAnnotation(t, h, "carol", `{"text":"two"}`)
	for i := range 3 {
		c.History().Append(cache.HistoryEntry{
			Type: cache.EventService, Action: "update", ResourceID: fmt.Sprint("svc", i),
		})
	}

	w := httptest.NewRecorder()
	h.HandleHistory(w, withContentType(
		newAnnotationRequest("GET", "/history", "carol", ""), ContentTypeCSV,
	))

	if rows := len(csvRecords(t, w.Body.String())) - 1; rows != 5 {
		t.Fatalf("%d rows, want the ring's 3 changes and both annotations", rows)
	}
}

func TestHistoryCSVAlternateKeepsTheKind(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := contentNegotiated(ok, feedHandlers{csv: true, csvParams: historyCSVParams}, ok)
	w := httptest.NewRecorder()
	handler(w, withContentType(
		httptest.NewRequest("GET", "/history?kind=annotation", nil), ContentTypeJSON,
	))

	if links := strings.Join(w.Header().Values("Link"), ", "); !strings.Contains(
		links, "kind=annotation",
	) {
		t.Fatalf("the CSV alternate drops the kind: %q", links)
	}
}
