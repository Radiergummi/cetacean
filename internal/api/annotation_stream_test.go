package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
)

func TestAclMatchWrap_AnnotationNeedsEveryResourceItNames(t *testing.T) {
	h := annotationHandlers(t, true)
	r := newAnnotationRequest("GET", "/events", "bob", "")
	match := h.aclMatchWrap(r, nil)

	web := cache.AnnotationResource{Type: cache.EventService, ID: "svc1", Name: "web"}
	worker := cache.AnnotationResource{Type: cache.EventNode, ID: "node1", Name: "worker"}

	for _, tt := range []struct {
		name      string
		resources []cache.AnnotationResource
		want      bool
	}{
		{"on a service bob reads", []cache.AnnotationResource{web}, true},
		{"cluster-wide", nil, true},
		{"also on a node bob cannot read", []cache.AnnotationResource{web, worker}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ev := cache.Event{
				Type:     cache.EventAnnotation,
				Action:   "create",
				ID:       "7",
				Resource: cache.Annotation{ID: 7, Resources: tt.resources},
			}
			if got := match(ev); got != tt.want {
				t.Errorf("match = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHistoryFeedShowsAnEditAsAnUpdate(t *testing.T) {
	h := annotationHandlers(t, true)
	id, etag := createAnnotation(t, h, "carol", `{"text":"maintenance <b>window</b>"}`)

	time.Sleep(10 * time.Millisecond)
	r := newAnnotationRequest(
		"PATCH",
		"/annotations/"+id,
		"carol",
		`{"text":"maintenance, extended"}`,
	)
	r.Header.Set("Content-Type", "application/merge-patch+json")
	r.Header.Set("If-Match", etag)
	serveAnnotation(h.HandlePatchAnnotation, r)

	a, _ := h.cache.Annotation(mustParseID(t, id))

	req := withContentType(newAnnotationRequest("GET", "/history", "carol", ""), ContentTypeAtom)
	w := httptest.NewRecorder()
	h.feedHistoryHandler(renderAtom)(w, req)
	body := w.Body.String()

	for _, want := range []string{
		"<id>urn:cetacean:annotation:" + h.cache.History().Instance() + ":" + id + "</id>",
		"<title>maintenance, extended</title>",
		"<updated>" + a.Updated.Format(time.RFC3339Nano) + "</updated>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("feed lacks %s:\n%s", want, body)
		}
	}
}

func TestHistoryFeedEscapesAnAnnotationsText(t *testing.T) {
	h := annotationHandlers(t, true)
	createAnnotation(t, h, "carol", `{"text":"<script>alert(1)</script>"}`)

	req := withContentType(
		newAnnotationRequest("GET", "/history", "carol", ""),
		ContentTypeJSONFeed,
	)
	w := httptest.NewRecorder()
	h.feedHistoryHandler(renderJSONFeed)(w, req)

	var feed struct {
		Items []struct {
			ContentHTML string `json:"content_html"`
		} `json:"items"` // newest first: the annotation
	}
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil || len(feed.Items) == 0 {
		t.Fatalf("feed: %v\n%s", err, w.Body)
	}
	if html := feed.Items[0].ContentHTML; strings.Contains(html, "<script>") ||
		!strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("content_html = %s, want the text escaped", html)
	}
}

func TestHistoryFeedTitlesAnAnnotationByItsFirstLine(t *testing.T) {
	h := annotationHandlers(t, true)
	createAnnotation(t, h, "carol", `{"text":"Deploy freeze\r\nuntil Monday"}`)

	req := withContentType(
		newAnnotationRequest("GET", "/history", "carol", ""),
		ContentTypeJSONFeed,
	)
	w := httptest.NewRecorder()
	h.feedHistoryHandler(renderJSONFeed)(w, req)

	var feed struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil || len(feed.Items) == 0 {
		t.Fatalf("feed: %v\n%s", err, w.Body)
	}
	if title := feed.Items[0].Title; title != "Deploy freeze" {
		t.Errorf("title = %q, want the first line without its carriage return", title)
	}
}

func TestHistoryFeedTakesTheTagAndKindItsJSONDoes(t *testing.T) {
	h := annotationHandlers(t, true)
	createAnnotation(t, h, "carol", `{"text":"tagged","tags":["deploy"]}`)
	createAnnotation(t, h, "carol", `{"text":"untagged"}`)

	req := withContentType(
		newAnnotationRequest("GET", "/history?kind=annotation&tag=deploy", "carol", ""),
		ContentTypeJSONFeed,
	)
	w := httptest.NewRecorder()
	h.feedHistoryHandler(renderJSONFeed)(w, req)

	var feed struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("feed: %v\n%s", err, w.Body)
	}
	if len(feed.Items) != 1 || feed.Items[0].Title != "tagged" {
		t.Errorf("items = %+v, want only the note tagged deploy", feed.Items)
	}
}

func TestEveryDetailStreamHearsClusterWideAnnotations(t *testing.T) {
	h := annotationHandlers(t, true)
	clusterWide := cache.Event{
		Type: cache.EventAnnotation, Action: "create", Resource: cache.Annotation{ID: 1},
	}

	for _, typ := range []cache.EventType{
		cache.EventTask, cache.EventConfig, cache.EventSecret, cache.EventNetwork, cache.EventVolume,
	} {
		match := h.detailStreamMatch(typ, "x", sse.ResourceMatcher(typ, "x"))
		if !match(clusterWide) {
			t.Errorf("a %s's stream misses a cluster-wide annotation its history lists", typ)
		}
	}
}

func TestTimelineFeedsTakeTheSearchTheirJSONDoes(t *testing.T) {
	h := annotationHandlers(t, true)
	createAnnotation(t, h, "carol", `{"text":"Deploy freeze","tags":["deploy"]}`)
	createAnnotation(t, h, "carol", `{"text":"Load test"}`)

	for path, handler := range map[string]http.HandlerFunc{
		"/history?kind=annotation&search=freeze": h.feedHistoryHandler(renderJSONFeed),
		"/annotations?search=freeze":             h.feedAnnotationsHandler(renderJSONFeed),
	} {
		req := withContentType(newAnnotationRequest("GET", path, "carol", ""), ContentTypeJSONFeed)
		w := httptest.NewRecorder()
		handler(w, req)

		var feed struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
			t.Fatalf("%s: %v\n%s", path, err, w.Body)
		}
		if len(feed.Items) != 1 || feed.Items[0].Title != "Deploy freeze" {
			t.Errorf("%s: items = %+v, want only the note mentioning freeze", path, feed.Items)
		}
	}
}

func TestTheHistoryFeedNarrowsToTheResourceItsJSONDoes(t *testing.T) {
	h := annotationHandlers(t, true)
	createAnnotation(t, h, "carol",
		`{"text":"on web","resources":[{"type":"service","id":"web"}]}`)
	createAnnotation(t, h, "carol",
		`{"text":"on worker","resources":[{"type":"node","id":"worker"}]}`)

	req := withContentType(newAnnotationRequest(
		"GET", "/history?kind=annotation&type=service&resourceId=web", "carol", "",
	), ContentTypeJSONFeed)
	w := httptest.NewRecorder()
	h.feedHistoryHandler(renderJSONFeed)(w, req)

	var feed struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &feed); err != nil {
		t.Fatalf("feed: %v\n%s", err, w.Body)
	}
	if len(feed.Items) != 1 || feed.Items[0].Title != "on web" {
		t.Errorf("items = %+v, want only the note on web", feed.Items)
	}
}
