package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
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
	c := cache.New(nil)
	populateSpecFixtures(c)

	broadcaster := sse.NewBroadcaster(0, noopErrorWriter, c.History())
	t.Cleanup(broadcaster.Close)

	var frames bytes.Buffer
	frames.Write(buildFrame(1, "2026-01-01T00:00:00.000000000Z hello\n"))

	router := newTestRouterWithCache(
		t, c,
		withBroadcaster(broadcaster),
		withDockerClient(&mockLogStreamer{data: frames.Bytes()}),
	)

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

	messages, ok := loadAsyncAPIDoc(t)["components"].(map[string]any)
	if !ok {
		t.Fatal("the document declares no components")
	}

	declared, ok := messages["messages"].(map[string]any)
	if !ok {
		t.Fatal("the document declares no components.messages")
	}

	// The four messages that are not a resource type: a batch of them, the
	// resync signal, and the three metrics frames plus the log line.
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

	sort.Strings(got)
	sort.Strings(want)

	if !slices.Equal(got, want) {
		t.Errorf(
			"the document's per-type messages are %v but cache.EventType "+
				"declares %v — a resource type gained or lost an event without "+
				"the stream contract following",
			got, want,
		)
	}
}

// eventTypeConstants reads the cache.EventType constant block out of
// internal/cache/cache.go. EventSync is excluded: it is the resync signal,
// not a resource type, and the document declares it as its own message.
func eventTypeConstants(t *testing.T) []string {
	t.Helper()

	const source = "../cache/cache.go"

	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}

	// EventNode    EventType = "node"
	pattern := regexp.MustCompile(`(?m)^\s*Event(\w+)\s+EventType\s*=\s*"([^"]+)"`)

	matches := pattern.FindAllStringSubmatch(string(raw), -1)
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
