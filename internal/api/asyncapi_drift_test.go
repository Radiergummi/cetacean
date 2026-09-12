package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestEveryStreamRouteIsAChannel probes every registered GET route with
// Accept: text/event-stream and requires anything that streams to appear in
// the document.
//
// Task 2's walk runs the other way: it proves every channel is a stream. Only
// this direction catches a stream route added after the document was written,
// which is what makes the document worth trusting later.
//
// /metrics is walked but never asserted: it answers 400 for a missing query
// parameter before setting a content type, so it cannot stream here. Its
// channel declaration is covered by TestAsyncAPIChannelsAnswerAsStreams,
// which probes addresses rather than routes and supplies the query.
func TestEveryStreamRouteIsAChannel(t *testing.T) {
	router, _, _ := streamTestRouter(t, 0)

	declared := make(map[string]bool)
	for _, address := range asyncAPIAddresses(t) {
		declared[address] = true
	}

	for _, pattern := range routerPatterns(t) {
		method, template, ok := strings.Cut(pattern, " ")
		if !ok || method != http.MethodGet {
			continue
		}

		// The SPA fallback matches everything; probing it proves nothing.
		if template == "/" {
			continue
		}

		// A template pathFixtures cannot resolve is skipped, as it is in
		// openapi_exhaustive_test.go. Adding the prefix there is what brings a
		// new parameterised route into both walks at once.
		path, ok := resolvePath(template)
		if !ok {
			continue
		}

		t.Run(template, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/event-stream")

			ctx, cancel := context.WithTimeout(req.Context(), 150*time.Millisecond)
			defer cancel()

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req.WithContext(ctx))

			if rec.Header().Get("Content-Type") != "text/event-stream" {
				return
			}

			if !declared[template] {
				t.Errorf(
					"%s streams but api/asyncapi.yaml declares no channel with "+
						"address %q — every stream this process serves has to be "+
						"described, or the document is a partial map again",
					template, template,
				)
			}
		})
	}
}

// TestAsyncAPIPerTypeMessagesMatchEventTypes pins what actually varies across
// the sixteen resource channels.
//
// Driving all sixteen behaviourally would run streamList and streamResource
// eight times each for sixteen chances to flake, while the only thing that
// differs between them is a type string. Comparing the declared per-type
// messages against the cache.EventType constants pins that string for all of
// them, and fails when a ninth resource type is added without anyone opening
// a ninth stream.
//
// The constants are read out of the source rather than from a Go slice: a
// slice would have to be kept in step by the same person who forgot the
// document, which is the drift this is meant to catch.
func TestAsyncAPIPerTypeMessagesMatchEventTypes(t *testing.T) {
	want := eventTypeConstants(t)

	components, ok := loadAsyncAPIDoc(t)["components"].(map[string]any)
	if !ok {
		t.Fatal("the document declares no components")
	}

	declared, ok := components["messages"].(map[string]any)
	if !ok {
		t.Fatal("the document declares no components.messages")
	}

	// The six messages that are not a resource type: a batch of them, the
	// resync signal, the log line, and the three metrics frames.
	notATypeName := map[string]bool{
		"batch": true, "sync": true, "logLine": true,
		"initial": true, "point": true, "queryError": true,
	}

	var got []string

	for name := range declared {
		if notATypeName[name] {
			continue
		}

		got = append(got, name)
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Errorf(
			"the document's per-type messages are %v but cache.EventType "+
				"declares %v — a resource type gained or lost an event without "+
				"the stream contract following",
			got, want,
		)
	}
}

// eventTypeConstants reads the cache.EventType constants out of every file in
// internal/cache, so a constant that moves between files is still found.
// EventSync is excluded: it is the resync signal, not a resource type, and the
// document declares it as its own message.
func eventTypeConstants(t *testing.T) []string {
	t.Helper()

	const source = "../cache"

	sources, err := filepath.Glob(filepath.Join(source, "*.go"))
	if err != nil || len(sources) == 0 {
		t.Fatalf("glob %s: %v", source, err)
	}

	var declarations []byte

	for _, name := range sources {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		declarations = append(declarations, raw...)
	}

	// EventNode    EventType = "node"
	pattern := regexp.MustCompile(`(?m)^\s*Event(\w+)\s+EventType\s*=\s*"([^"]+)"`)

	matches := pattern.FindAllStringSubmatch(string(declarations), -1)
	if len(matches) == 0 {
		t.Fatalf(
			"found no EventType constants in %s — the declaration moved and "+
				"this test is now asserting nothing",
			source,
		)
	}

	var types []string

	for _, match := range matches {
		if match[2] == "sync" {
			continue
		}

		types = append(types, match[2])
	}

	return types
}
