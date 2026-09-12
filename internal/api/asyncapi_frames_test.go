package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
	promapi "github.com/radiergummi/cetacean/internal/prometheus"
)

// sseFrame is one parsed `event:`/`id:`/`data:` group. Comment lines (the
// keepalive) carry none of the three and are dropped.
type sseFrame struct {
	Event string
	ID    string
	Data  string
}

func readSSEFrames(t *testing.T, body string) []sseFrame {
	t.Helper()

	var frames []sseFrame

	for block := range strings.SplitSeq(body, "\n\n") {
		var frame sseFrame
		var seen bool

		for line := range strings.SplitSeq(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				frame.Event = strings.TrimPrefix(line, "event: ")
				seen = true
			case strings.HasPrefix(line, "id: "):
				frame.ID = strings.TrimPrefix(line, "id: ")
				seen = true
			case strings.HasPrefix(line, "data: "):
				frame.Data = strings.TrimPrefix(line, "data: ")
				seen = true
			}
		}

		if seen {
			frames = append(frames, frame)
		}
	}

	return frames
}

// streamUntilIdle opens path as a stream, runs drive once the handler is
// serving, and returns the frames written before the context expires.
func streamUntilIdle(
	t *testing.T,
	router http.Handler,
	path string,
	lastEventID string,
	drive func(),
) []sseFrame {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", "text/event-stream")

	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	ctx, cancel := context.WithTimeout(req.Context(), 400*time.Millisecond)
	defer cancel()

	rec := httptest.NewRecorder()

	if drive != nil {
		go func() {
			time.Sleep(50 * time.Millisecond)
			drive()
		}()
	}

	router.ServeHTTP(rec, req.WithContext(ctx))

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	return readSSEFrames(t, rec.Body.String())
}

// resourceStreamRouter is a router whose broadcaster has a real history ring,
// so Last-Event-ID reaches the replay path rather than being ignored.
func resourceStreamRouter(t *testing.T) (http.Handler, *cache.Cache, *sse.Broadcaster) {
	t.Helper()

	c := cache.New(nil)
	populateSpecFixtures(c)

	broadcaster := sse.NewBroadcaster(50*time.Millisecond, noopErrorWriter, c.History())
	t.Cleanup(broadcaster.Close)

	router := newTestRouterWithCache(t, c, withBroadcaster(broadcaster))

	return router, c, broadcaster
}

// TestListStreamEmitsTheDeclaredNames drives /services, the exemplar for
// streamList: one mutation is named for its type, two inside a batch interval
// become `batch` with an array payload.
func TestListStreamEmitsTheDeclaredNames(t *testing.T) {
	t.Run("a single event is named for its type", func(t *testing.T) {
		router, _, broadcaster := resourceStreamRouter(t)

		frames := streamUntilIdle(t, router, "/services", "", func() {
			broadcaster.Broadcast(cache.Event{
				Type:      cache.EventService,
				Action:    "update",
				ID:        "svc1",
				Name:      "web",
				HistoryID: 1,
				Resource:  swarm.Service{ID: "svc1"},
			})
		})

		if len(frames) == 0 {
			t.Fatal("no frames")
		}

		if frames[0].Event != "service" {
			t.Errorf("event = %q, want service", frames[0].Event)
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(frames[0].Data), &payload); err != nil {
			t.Fatalf("payload is not an object: %v", err)
		}

		if payload["resource"] == nil {
			t.Error("a live event must carry resource")
		}
	})

	t.Run("two events inside the interval become a batch array", func(t *testing.T) {
		router, _, broadcaster := resourceStreamRouter(t)

		frames := streamUntilIdle(t, router, "/services", "", func() {
			for _, id := range []string{"svc1", "svc2"} {
				broadcaster.Broadcast(cache.Event{
					Type:      cache.EventService,
					Action:    "update",
					ID:        id,
					Name:      "web",
					HistoryID: 1,
					Resource:  swarm.Service{ID: id},
				})
			}
		})

		var batch *sseFrame

		for i := range frames {
			if frames[i].Event == "batch" {
				batch = &frames[i]
			}
		}

		if batch == nil {
			t.Fatalf("no batch frame; got %+v", frames)
		}

		var payload []map[string]any
		if err := json.Unmarshal([]byte(batch.Data), &payload); err != nil {
			t.Fatalf(
				"a batch payload must be an array of envelopes, not one envelope: %v",
				err,
			)
		}

		if len(payload) != 2 {
			t.Errorf("batch carries %d envelopes, want 2", len(payload))
		}
	})
}

// TestAgedOutCursorEmitsSync: a cursor the ring no longer holds gets the
// `sync` message the document declares, with action full_sync and no
// resource.
func TestAgedOutCursorEmitsSync(t *testing.T) {
	router, _, _ := resourceStreamRouter(t)

	frames := streamUntilIdle(t, router, "/services", "999999", nil)

	if len(frames) == 0 {
		t.Fatal("no frames — an aged-out cursor must be answered, not ignored")
	}

	if frames[0].Event != "sync" {
		t.Fatalf("event = %q, want sync", frames[0].Event)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(frames[0].Data), &payload); err != nil {
		t.Fatalf("payload is not an object: %v", err)
	}

	if payload["action"] != "full_sync" {
		t.Errorf("action = %v, want full_sync", payload["action"])
	}

	if _, present := payload["resource"]; present {
		t.Error("a sync payload must not carry resource")
	}
}

// TestReplayedFrameOmitsResource: replay reads the change history, which
// stores identity and not payload, so a replayed envelope has no resource.
// A client that assumes it is present breaks only after a reconnection.
func TestReplayedFrameOmitsResource(t *testing.T) {
	router, c, _ := resourceStreamRouter(t)

	c.History().Append(cache.HistoryEntry{
		Type:       cache.EventService,
		Action:     "update",
		ResourceID: "svc1",
		Name:       "web",
		Timestamp:  time.Now(),
	})

	frames := streamUntilIdle(t, router, "/services", "0", nil)

	if len(frames) == 0 {
		t.Fatal("no frames — a replayable cursor must replay")
	}

	if frames[0].Event == "sync" {
		t.Fatal("the cursor was replayable but the stream answered sync")
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(frames[0].Data), &payload); err != nil {
		// A batch frame: unwrap and take the first envelope.
		var batched []map[string]any
		if err := json.Unmarshal([]byte(frames[0].Data), &batched); err != nil {
			t.Fatalf("payload is neither an object nor an array: %v", err)
		}

		payload = batched[0]
	}

	if _, present := payload["resource"]; present {
		t.Error("a replayed envelope must not carry resource")
	}
}

// TestDetailStreamNeverReplays: a detail channel is ineligible for replay, so
// any cursor gets sync — not only an aged-out one.
func TestDetailStreamNeverReplays(t *testing.T) {
	router, c, _ := resourceStreamRouter(t)

	c.History().Append(cache.HistoryEntry{
		Type:       cache.EventService,
		Action:     "update",
		ResourceID: "svc1",
		Name:       "web",
		Timestamp:  time.Now(),
	})

	frames := streamUntilIdle(t, router, "/services/svc1", "0", nil)

	if len(frames) == 0 {
		t.Fatal("no frames")
	}

	if frames[0].Event != "sync" {
		t.Errorf(
			"event = %q, want sync — a detail stream offers no replay even for a "+
				"cursor the ring still holds",
			frames[0].Event,
		)
	}
}

// TestLogTailFramesAreUnnamedAndTheCursorOnlyMovesForward: log frames carry
// no event name, and their RFC 3339 id never moves backwards, because Docker
// interleaves a service's tasks and a backwards cursor would discard the
// lines in between on the next resume.
func TestLogTailFramesAreUnnamedAndTheCursorOnlyMovesForward(t *testing.T) {
	c := cache.New(nil)
	populateSpecFixtures(c)

	var docker bytes.Buffer
	docker.Write(buildFrame(1, "2026-01-01T00:00:02.000000000Z second\n"))
	docker.Write(buildFrame(1, "2026-01-01T00:00:01.000000000Z out-of-order\n"))
	docker.Write(buildFrame(1, "2026-01-01T00:00:03.000000000Z third\n"))

	router := newTestRouterWithCache(
		t, c,
		withDockerClient(&mockLogStreamer{data: docker.Bytes()}),
	)

	for _, path := range []string{"/services/svc1/logs", "/tasks/task1/logs"} {
		t.Run(path, func(t *testing.T) {
			frames := streamUntilIdle(t, router, path, "", nil)

			if len(frames) == 0 {
				t.Fatal("no frames")
			}

			var previous string

			for _, frame := range frames {
				if frame.Event != "" {
					t.Errorf("log frame is named %q; log frames are unnamed", frame.Event)
				}

				if frame.ID == "" {
					continue
				}

				if _, err := time.Parse(time.RFC3339Nano, frame.ID); err != nil {
					t.Errorf("id %q is not an RFC 3339 timestamp", frame.ID)
				}

				if previous != "" && frame.ID < previous {
					t.Errorf(
						"id moved backwards: %q after %q — a resume from the later "+
							"cursor would discard the lines in between",
						frame.ID, previous,
					)
				}

				previous = frame.ID
			}
		})
	}
}

// TestMetricsStreamEmitsTheDeclaredNames covers the third dialect: initial,
// point and query_error, and no id at all.
func TestMetricsStreamEmitsTheDeclaredNames(t *testing.T) {
	t.Run("initial then point, with no id", func(t *testing.T) {
		prometheus := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				if strings.HasSuffix(r.URL.Path, "/query_range") {
					w.Write([]byte(
						`{"status":"success","data":{"resultType":"matrix","result":[]}}`,
					))

					return
				}

				w.Write([]byte(
					`{"status":"success","data":{"resultType":"vector","result":[]}}`,
				))
			},
		))
		t.Cleanup(prometheus.Close)

		router := newTestRouterWithCache(
			t, cache.New(nil),
			withPromClient(promapi.NewClient(prometheus.URL)),
			withTickerInterval(10*time.Millisecond),
		)

		frames := streamUntilIdle(t, router, "/metrics?query=up&step=5", "", nil)

		if len(frames) == 0 {
			t.Fatal("no frames")
		}

		if frames[0].Event != "initial" {
			t.Errorf("first event = %q, want initial", frames[0].Event)
		}

		var points int

		for _, frame := range frames {
			if frame.Event == "point" {
				points++
			}

			if frame.ID != "" {
				t.Errorf("metrics frame carries id %q; this stream writes none", frame.ID)
			}
		}

		if points == 0 {
			t.Error("no point event; the stream declares one per tick")
		}
	})

	t.Run("a failing query is named query_error", func(t *testing.T) {
		prometheus := httptest.NewServer(http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
		))
		t.Cleanup(prometheus.Close)

		router := newTestRouterWithCache(
			t, cache.New(nil),
			withPromClient(promapi.NewClient(prometheus.URL)),
			withTickerInterval(10*time.Millisecond),
		)

		frames := streamUntilIdle(t, router, "/metrics?query=up&step=5", "", nil)

		if len(frames) == 0 {
			t.Fatal("no frames")
		}

		if frames[0].Event != "query_error" {
			t.Fatalf("first event = %q, want query_error", frames[0].Event)
		}

		var payload struct {
			Error     string `json:"error"`
			ErrorType string `json:"errorType"`
		}

		if err := json.Unmarshal([]byte(frames[0].Data), &payload); err != nil {
			t.Fatalf("payload is not an object: %v", err)
		}

		if payload.ErrorType != "server_error" {
			t.Errorf("errorType = %q, want server_error", payload.ErrorType)
		}
	})
}
