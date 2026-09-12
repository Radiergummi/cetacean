package api

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

func TestNegotiateCSV(t *testing.T) {
	t.Run("Accept text/csv resolves to CSV", func(t *testing.T) {
		if got := parseAccept("text/csv"); got != ContentTypeCSV {
			t.Errorf("parseAccept(%q) = %v, want CSV", "text/csv", got)
		}
	})

	t.Run("a csv suffix resolves to CSV and leaves the path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/services.csv", nil)

		got, suffix := resolveExtension(req)
		if got != ContentTypeCSV {
			t.Errorf("resolveExtension(/services.csv) = %v, want CSV", got)
		}

		if suffix != ".csv" {
			t.Errorf("suffix = %q, want %q", suffix, ".csv")
		}

		if req.URL.Path != "/services" {
			t.Errorf("path = %q, want %q", req.URL.Path, "/services")
		}
	})

	t.Run("a text wildcard still resolves to HTML", func(t *testing.T) {
		if got := parseAccept("text/*"); got != ContentTypeHTML {
			t.Errorf("parseAccept(%q) = %v, want HTML", "text/*", got)
		}
	})
}

// TestRenderCSVEscapes holds the renderer to RFC 4180 §2.
func TestRenderCSVEscapes(t *testing.T) {
	got := string(renderCSV(csvTable{
		header: []string{"name", "note"},
		records: [][]string{
			{"plain", "nothing special"},
			{"comma", "a,b"},
			{"quote", `he said "hi"`},
			{"break", "line1\nline2"},
		},
	}))

	want := "name,note\r\n" +
		"plain,nothing special\r\n" +
		"comma,\"a,b\"\r\n" +
		"quote,\"he said \"\"hi\"\"\"\r\n" +
		"break,\"line1\r\nline2\"\r\n"

	if got != want {
		t.Errorf("renderCSV:\n got %q\nwant %q", got, want)
	}
}

func TestCSVFilename(t *testing.T) {
	at := time.Date(2026, 9, 11, 23, 30, 0, 0, time.UTC)

	if got := csvFilename("services", at); got != "services-2026-09-11.csv" {
		t.Errorf("csvFilename = %q, want %q", got, "services-2026-09-11.csv")
	}

	// A task export carries its parent's name, and a hostname is neither ASCII
	// nor quote-free by construction.
	const want = `tasks-b-cker-01--2026-09-11.csv`
	if got := csvFilename(`tasks-bäcker-01"`, at); got != want {
		t.Errorf("csvFilename = %q, want %q", got, want)
	}
}

func TestWriteCSV(t *testing.T) {
	table := csvTable{header: []string{"name"}, records: [][]string{{"api"}}}

	req := httptest.NewRequest("GET", "/services", nil)
	rec := httptest.NewRecorder()
	writeCSV(rec, req, "services", table)

	const wantType = "text/csv; charset=utf-8; header=present"
	if got := rec.Header().Get("Content-Type"); got != wantType {
		t.Errorf("Content-Type = %q, want %q", got, wantType)
	}

	disposition := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, `attachment; filename="services-`) ||
		!strings.HasSuffix(disposition, `.csv"`) {
		t.Errorf("Content-Disposition = %q, want an attachment named after the type", disposition)
	}

	if got := rec.Body.String(); got != "name\r\napi\r\n" {
		t.Errorf("body = %q, want %q", got, "name\r\napi\r\n")
	}

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on a CSV response")
	}

	conditional := httptest.NewRequest("GET", "/services", nil)
	conditional.Header.Set("If-None-Match", etag)
	again := httptest.NewRecorder()
	writeCSV(again, conditional, "services", table)

	if again.Code != http.StatusNotModified {
		t.Errorf("If-None-Match got %d, want 304", again.Code)
	}
}

func csvRecords(t *testing.T, body string) [][]string {
	t.Helper()

	records, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("response is not valid CSV: %v", err)
	}

	return records
}

func TestListEndpointCSV(t *testing.T) {
	c := cache.New(nil)
	for _, hostname := range []string{"swarm-2", "swarm-3", "swarm-1"} {
		c.SetNode(swarm.Node{
			ID: "id-" + hostname,
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleWorker,
				Availability: swarm.NodeAvailabilityActive,
			},
			Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
			Description: swarm.NodeDescription{Hostname: hostname},
		})
	}

	router := newTestRouterWithCache(t, c)

	get := func(t *testing.T, target string) *httptest.ResponseRecorder {
		t.Helper()

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200; body: %s", target, rec.Code, rec.Body.String())
		}

		return rec
	}

	// Every row builder sorts by name, which would otherwise throw away the
	// order the endpoint was asked for.
	t.Run("the requested sort survives the row conversion", func(t *testing.T) {
		records := csvRecords(t, get(t, "/nodes.csv?sort=hostname&dir=desc").Body.String())

		var names []string
		for _, record := range records[1:] {
			names = append(names, record[0])
		}

		want := []string{"swarm-3", "swarm-2", "swarm-1"}
		if !slices.Equal(names, want) {
			t.Errorf("order = %v, want %v", names, want)
		}
	})

	t.Run("Accept text/csv renders the same thing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/nodes", nil)
		req.Header.Set("Accept", "text/csv")
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
			t.Errorf("Content-Type = %q, want text/csv", got)
		}
	})
}

// TestEveryListEndpointRendersCSV drives all eight lists: listFeeds offers CSV
// for every one of them, and only the spec beside each handler builds its rows.
func TestEveryListEndpointRendersCSV(t *testing.T) {
	stackLabel := map[string]string{"com.docker.stack.namespace": "shop"}

	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID: "n1",
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{Hostname: "swarm-1"},
	})
	c.SetService(swarm.Service{
		ID: "s1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "shop_api", Labels: stackLabel},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:1.27"},
			},
		},
	})
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "s1",
		NodeID:    "n1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})
	c.SetConfig(swarm.Config{
		ID: "c1",
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: "shop_nginx", Labels: stackLabel},
		},
	})
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "shop_token", Labels: stackLabel},
		},
	})
	c.SetNetwork(network.Summary{
		ID:     "net1",
		Name:   "shop_default",
		Driver: "overlay",
		Labels: stackLabel,
	})
	c.SetVolume(volume.Volume{Name: "shop_data", Driver: "local", Labels: stackLabel})

	router := newTestRouterWithCache(t, c)

	// Spelled out rather than read off csvRowColumns, which would make the
	// assertion agree with whatever the map says.
	want := map[string][][]string{
		"node": {
			{"name", "state", "role", "id"},
			{"swarm-1", "ready", "manager", "n1"},
		},
		"service": {
			{"name", "stack", "state", "image", "desired", "running", "id"},
			{"shop_api", "shop", "running", "nginx:1.27", "0", "1", "s1"},
		},
		"task": {
			{"name", "state", "node", "id"},
			{"shop_api.1", "running", "swarm-1", "t1"},
		},
		"stack": {
			{"name", "services", "id"},
			{"shop", "1", "shop"},
		},
		"config": {
			{"name", "stack", "id"},
			{"shop_nginx", "shop", "c1"},
		},
		"secret": {
			{"name", "stack", "id"},
			{"shop_token", "shop", "sec1"},
		},
		"network": {
			{"name", "stack", "driver", "id"},
			{"shop_default", "shop", "overlay", "net1"},
		},
		"volume": {
			{"name", "stack", "driver", "id"},
			{"shop_data", "shop", "local", "shop_data"},
		},
	}

	for resourceType, want := range want {
		t.Run(resourceType, func(t *testing.T) {
			target := "/" + resourceType + "s.csv"

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body: %s", target, rec.Code, rec.Body.String())
			}

			records := csvRecords(t, rec.Body.String())

			if len(records) != 2 {
				t.Fatalf("got %d records, want the one seeded resource", len(records)-1)
			}

			if !slices.Equal(records[0], want[0]) {
				t.Errorf("header = %v, want %v", records[0], want[0])
			}

			if !slices.Equal(records[1], want[1]) {
				t.Errorf("record = %v, want %v", records[1], want[1])
			}
		})
	}
}

// TestTaskSubListCSV covers the two task lists hanging off a parent. They
// render the same rows /tasks does, and name the file after the parent so
// three downloads do not collide.
func TestTaskSubListCSV(t *testing.T) {
	c := cache.New(nil)
	c.SetNode(swarm.Node{
		ID:          "n1",
		Spec:        swarm.NodeSpec{Availability: swarm.NodeAvailabilityActive},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		Description: swarm.NodeDescription{Hostname: "swarm-1"},
	})
	c.SetService(swarm.Service{
		ID:   "s1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "api"}},
	})
	c.SetTask(swarm.Task{
		ID:        "t1",
		ServiceID: "s1",
		NodeID:    "n1",
		Slot:      1,
		Status:    swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	router := newTestRouterWithCache(t, c)

	for target, wantName := range map[string]string{
		"/nodes/n1/tasks.csv":    "tasks-swarm-1-",
		"/services/s1/tasks.csv": "tasks-api-",
	} {
		t.Run(target, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body: %s", target, rec.Code, rec.Body.String())
			}

			records := csvRecords(t, rec.Body.String())

			wantRecord := []string{"api.1", "running", "swarm-1", "t1"}
			if !slices.Equal(records[1], wantRecord) {
				t.Errorf("record = %v, want %v", records[1], wantRecord)
			}

			disposition := rec.Header().Get("Content-Disposition")
			if !strings.Contains(disposition, wantName) {
				t.Errorf(
					"Content-Disposition = %q, want a filename starting %q",
					disposition,
					wantName,
				)
			}
		})
	}
}

// TestListCSVPagination pins the one place CSV diverges from the JSON: a
// request that named no page gets every row rather than the first fifty.
func TestListCSVPagination(t *testing.T) {
	c := cache.New(nil)
	for i := range 60 {
		hostname := fmt.Sprintf("swarm-%02d", i)
		c.SetNode(swarm.Node{
			ID:          "id-" + hostname,
			Description: swarm.NodeDescription{Hostname: hostname},
		})
	}

	router := newTestRouterWithCache(t, c)

	records := func(t *testing.T, target string) [][]string {
		t.Helper()

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))

		return csvRecords(t, rec.Body.String())
	}

	t.Run("an unpaginated request renders every row", func(t *testing.T) {
		if got := len(records(t, "/nodes.csv")) - 1; got != 60 {
			t.Errorf("got %d records, want all 60", got)
		}
	})

	t.Run("an explicit limit is honoured", func(t *testing.T) {
		if got := len(records(t, "/nodes.csv?limit=2")) - 1; got != 2 {
			t.Errorf("got %d records, want 2", got)
		}
	})

	t.Run("an explicit offset is honoured", func(t *testing.T) {
		rows := records(t, "/nodes.csv?sort=hostname&offset=58")
		if got := len(rows) - 1; got != 2 {
			t.Fatalf("got %d records, want 2", got)
		}
		if rows[1][0] != "swarm-58" {
			t.Errorf("first record = %q, want swarm-58", rows[1][0])
		}
	})

	// A CSV answers 200 with no Content-Range, so a partial body under it
	// would be a lie. The header is ignored rather than half-honoured.
	t.Run("a Range header renders every row", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nodes.csv", nil)
		req.Header.Set("Range", "items 0-9")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		if got := len(csvRecords(t, rec.Body.String())) - 1; got != 60 {
			t.Errorf("got %d records, want all 60", got)
		}
	})
}

// TestHistoryCSV covers the change feed, which is not a Row. Its names are
// free text, so this is also the escaping rule met through a real endpoint.
func TestHistoryCSV(t *testing.T) {
	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "s1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: `we,b "one"`}},
	})

	router := newTestRouterWithCache(t, c)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/history.csv", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	records := csvRecords(t, rec.Body.String())

	wantHeader := []string{"timestamp", "type", "action", "name", "id", "summary"}
	if !slices.Equal(records[0], wantHeader) {
		t.Errorf("header = %v, want %v", records[0], wantHeader)
	}

	if len(records) != 2 {
		t.Fatalf("got %d entries, want the one the seeded service recorded", len(records)-1)
	}

	entry := records[1]
	if _, err := time.Parse(time.RFC3339, entry[0]); err != nil {
		t.Errorf("timestamp %q is not RFC 3339: %v", entry[0], err)
	}
	if entry[1] != "service" || entry[2] != "create" {
		t.Errorf("type/action = %q/%q, want service/create", entry[1], entry[2])
	}

	if entry[3] != `we,b "one"` {
		t.Errorf("name = %q, want the name it was seeded with", entry[3])
	}
}

func TestRecommendationsCSV(t *testing.T) {
	suggested := 512.0
	engine := recommendations.NewEngine(&stubChecker{results: []recommendations.Recommendation{{
		Category:   recommendations.CategoryOverProvisioned,
		Severity:   recommendations.SeverityWarning,
		Scope:      recommendations.ScopeService,
		TargetID:   "s1",
		TargetName: "api",
		Resource:   "memory",
		Message:    "api is over-provisioned, by a lot",
		Current:    128,
		Configured: 1024,
		Suggested:  &suggested,
	}}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine.Run(ctx)

	router := newTestRouterWithCache(t, cache.New(nil), withRecEngine(engine))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/recommendations.csv", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	records := csvRecords(t, rec.Body.String())

	wantHeader := []string{
		"severity", "category", "scope", "target", "resource",
		"message", "current", "configured", "suggested",
	}
	if !slices.Equal(records[0], wantHeader) {
		t.Errorf("header = %v, want %v", records[0], wantHeader)
	}

	want := []string{
		"warning", "over-provisioned", "service", "api", "memory",
		"api is over-provisioned, by a lot", "128", "1024", "512",
	}
	if !slices.Equal(records[1], want) {
		t.Errorf("record = %v, want %v", records[1], want)
	}
}

func TestCSVFallbackColumns(t *testing.T) {
	table := csvTableForRows("kraken", []cluster.Row{{
		ID:     "k1",
		Name:   "release",
		Type:   "kraken",
		State:  "asleep",
		Detail: "deep",
	}})

	want := []string{"name", "state", "detail", "id"}
	if !slices.Equal(table.header, want) {
		t.Errorf("header = %v, want %v", table.header, want)
	}
}
