package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
)

// listenStream drives subscriptions/listen against a real HTTP server the way a
// 2026-07-28 client does, and reports the notification methods and resource
// URIs that arrive on the held-open response stream.
//
// Everything here goes through the transport on purpose. Calling the hooks
// directly with a synthetic session is what let a completely broken
// implementation look correct: the real transport mints no session at all for a
// modern client, and a stub that supplies one hides exactly the bug that
// matters.
type listenStream struct {
	notifications chan mcplib.JSONRPCNotification
	cancel        context.CancelFunc
}

func startListenStream(t *testing.T, handler http.Handler, uris ...string) *listenStream {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	subscriptions, err := json.Marshal(uris)
	if err != nil {
		t.Fatalf("marshal uris: %v", err)
	}

	params := fmt.Sprintf(`{"notifications":{"resourceSubscriptions":%s}}`, subscriptions)
	body := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"subscriptions/listen","params":%s}`,
		withProtocolMeta(t, params),
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		server.URL+"/mcp",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set(mcplib.HeaderProtocolVersion, mcplib.LATEST_PROTOCOL_VERSION)
	request.Header.Set(mcplib.HeaderMethod, "subscriptions/listen")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("subscriptions/listen: %v", err)
	}

	t.Cleanup(func() { _ = response.Body.Close() })

	if response.StatusCode != http.StatusOK {
		t.Fatalf("subscriptions/listen status = %d", response.StatusCode)
	}

	stream := &listenStream{
		notifications: make(chan mcplib.JSONRPCNotification, 16),
		cancel:        cancel,
	}

	go func() {
		defer close(stream.notifications)

		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			payload, found := strings.CutPrefix(scanner.Text(), "data: ")
			if !found {
				continue
			}

			var notification mcplib.JSONRPCNotification
			if err := json.Unmarshal([]byte(payload), &notification); err != nil {
				continue
			}

			select {
			case stream.notifications <- notification:
			default:
			}
		}
	}()

	return stream
}

// await returns the first notification with the given method, or fails.
func (l *listenStream) await(t *testing.T, method string) mcplib.JSONRPCNotification {
	t.Helper()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case notification, open := <-l.notifications:
			if !open {
				t.Fatalf("stream closed before %q arrived", method)
			}

			if notification.Method == method {
				return notification
			}

		case <-deadline:
			t.Fatalf("timed out waiting for %q on the subscription stream", method)
		}
	}
}

// TestSubscriptionsListenDeliversResourceUpdates is the end-to-end guarantee
// Phase 2 exists for: a modern client subscribes over subscriptions/listen and
// actually receives notifications/resources/updated when the cluster changes.
func TestSubscriptionsListenDeliversResourceUpdates(t *testing.T) {
	clusterCache := cache.New(nil)
	clusterCache.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	srv := newResourceTestServer(t, clusterCache)
	stream := startListenStream(t, srv.Handler(), "cetacean://services/svc1")

	// The acknowledgement proves the subscription was established before we
	// mutate the cache, so a missing update is a delivery failure and not a
	// race with subscription setup.
	stream.await(t, "notifications/subscriptions/acknowledged")

	clusterCache.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web-renamed"}},
	})

	notification := stream.await(t, "notifications/resources/updated")
	if got := notification.Params.AdditionalFields["uri"]; got != "cetacean://services/svc1" {
		t.Fatalf("updated notification uri = %v, want cetacean://services/svc1", got)
	}
}

// TestSubscriptionsListenHonoursOptIn pins the opt-in rule: 2026-07-28 makes
// every notification type opt-in, so a client that subscribed only to a
// resource URI must not receive resources/list_changed.
func TestSubscriptionsListenHonoursOptIn(t *testing.T) {
	clusterCache := cache.New(nil)
	srv := newResourceTestServer(t, clusterCache)
	stream := startListenStream(t, srv.Handler(), "cetacean://services/svc1")

	stream.await(t, "notifications/subscriptions/acknowledged")

	// A create is a list change. The client did not ask for list_changed, so
	// only the per-URI update may arrive.
	clusterCache.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}},
	})

	notification := stream.await(t, "notifications/resources/updated")
	if notification.Method != "notifications/resources/updated" {
		t.Fatalf("unexpected first notification: %s", notification.Method)
	}

	select {
	case extra, open := <-stream.notifications:
		if open && extra.Method == "notifications/resources/list_changed" {
			t.Fatal("delivered resources/list_changed to a client that did not opt in")
		}

	case <-time.After(300 * time.Millisecond):
		// Nothing further arrived, which is what we want.
	}
}
