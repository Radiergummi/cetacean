package api

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// attributedHistory holds two entries: "external", which nobody made through
// Cetacean, and then "web", which alice made over MCP.
func attributedHistory(t *testing.T) http.Handler {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "s0",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "external"}},
	})

	c.Attributions().Expect(
		cache.Expectation{Type: cache.EventService, Key: "web", Action: "create"},
		cache.Actor{
			Subject:   "u-1",
			Name:      "alice",
			Provider:  "oauth",
			Client:    "claude",
			Transport: cache.TransportMCP,
		},
	)
	c.SetService(swarm.Service{
		ID:   "s1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	return newTestRouterWithCache(t, c)
}

func getHistory(t *testing.T, router http.Handler, path string) string {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d; body: %s", path, rec.Code, rec.Body)
	}

	if rec.Header().Get("ETag") == "" {
		t.Errorf("GET %s carries no ETag", path)
	}

	return rec.Body.String()
}

func TestHistoryJSONCarriesTheActor(t *testing.T) {
	body := getHistory(t, attributedHistory(t), "/history")

	var page struct {
		Items []struct {
			Name  string          `json:"name"`
			Actor json.RawMessage `json:"actor"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}

	if len(page.Items) != 2 {
		t.Fatalf("got %d entries, want 2", len(page.Items))
	}

	want := `{"subject":"u-1","name":"alice","provider":"oauth","client":"claude","transport":"mcp"}`
	if page.Items[0].Name != "web" || string(page.Items[0].Actor) != want {
		t.Errorf("newest entry = %s %s, want web %s", page.Items[0].Name, page.Items[0].Actor, want)
	}

	if page.Items[1].Actor != nil {
		t.Errorf("the external change carries an actor: %s", page.Items[1].Actor)
	}
}

func TestHistoryCSVCarriesTheActor(t *testing.T) {
	records := csvRecords(t, getHistory(t, attributedHistory(t), "/history.csv"))

	wantHeader := []string{
		"timestamp", "type", "action", "name", "id", "summary",
		"actor", "provider", "client", "transport",
	}
	if !slices.Equal(records[0], wantHeader) {
		t.Fatalf("header = %v, want %v", records[0], wantHeader)
	}

	if got := records[1][6:]; !slices.Equal(got, []string{"alice", "oauth", "claude", "mcp"}) {
		t.Errorf("attributed actor columns = %v", got)
	}

	if got := records[2][6:]; !slices.Equal(got, []string{"", "", "", ""}) {
		t.Errorf("external actor columns = %v, want them empty", got)
	}
}

func TestHistoryAtomNamesTheActorAsAuthor(t *testing.T) {
	body := getHistory(t, attributedHistory(t), "/history.atom")

	var feed struct {
		Author struct {
			Name string `xml:"name"`
		} `xml:"author"`
		Entries []struct {
			Author *struct {
				Name string `xml:"name"`
			} `xml:"author"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatal(err)
	}

	if len(feed.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(feed.Entries))
	}

	attributed := feed.Entries[0]
	if attributed.Author == nil || attributed.Author.Name != "alice" {
		t.Errorf("attributed entry author = %+v, want alice", attributed.Author)
	}

	if !strings.Contains(attributed.Content, "by alice via MCP") {
		t.Errorf("content = %q, want it to say who made the change", attributed.Content)
	}

	// RFC 4287 §4.2.1: an entry without one inherits the feed's author.
	if feed.Entries[1].Author != nil {
		t.Errorf("the external change names an author: %+v", feed.Entries[1].Author)
	}
	if feed.Author.Name != "Cetacean" {
		t.Errorf("feed author = %q, want Cetacean", feed.Author.Name)
	}
}

func TestHistoryJSONFeedNamesTheActorAsAuthor(t *testing.T) {
	body := getHistory(t, attributedHistory(t), "/history.feed")

	var feed struct {
		Items []struct {
			Authors []struct {
				Name string `json:"name"`
			} `json:"authors"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatal(err)
	}

	if len(feed.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(feed.Items))
	}

	if a := feed.Items[0].Authors; len(a) != 1 || a[0].Name != "alice" {
		t.Errorf("attributed item authors = %+v, want alice", a)
	}

	if a := feed.Items[1].Authors; len(a) != 0 {
		t.Errorf("the external change names authors: %+v", a)
	}
}
