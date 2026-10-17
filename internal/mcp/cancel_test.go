package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

var (
	alice = &auth.Identity{Provider: "oidc", Subject: "alice"}
	bob   = &auth.Identity{Provider: "oidc", Subject: "bob"}
)

// cancelHarness runs requests through scopeCancels into a handler that blocks
// until its request is cancelled, and records any message the guard forwards.
type cancelHarness struct {
	handler   http.Handler
	forwarded chan string
}

func newCancelHarness() *cancelHarness {
	h := &cancelHarness{forwarded: make(chan string, 16)}
	h.handler = (&Server{}).scopeCancels(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"tools/call"`) {
				<-r.Context().Done()
				return
			}
			h.forwarded <- string(body)
		}),
	)

	return h
}

func (h *cancelHarness) post(id *auth.Identity, body string) int {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), id))
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)

	return w.Code
}

// open starts a tools/call for id and returns a channel closed when it ends.
func (h *cancelHarness) open(t *testing.T, id *auth.Identity, requestID string) <-chan struct{} {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.post(id, `{"jsonrpc":"2.0","id":`+requestID+`,"method":"tools/call","params":{}}`)
	}()
	time.Sleep(20 * time.Millisecond)

	return done
}

func ended(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-time.After(100 * time.Millisecond):
		return false
	}
}

func cancelOf(requestID string) string {
	return `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":` + requestID + `}}`
}

// The stateless transport keys a request's cancel by its JSON-RPC id alone, so
// Cetacean cancels requests itself, by caller, and mcp-go never sees a cancel.
func TestCancelEndsOnlyTheCallersOwnRequest(t *testing.T) {
	h := newCancelHarness()
	bobs := h.open(t, bob, `7`)
	alices := h.open(t, alice, `7`)

	if code := h.post(alice, cancelOf(`7`)); code != http.StatusAccepted {
		t.Errorf("cancel: status = %d, want 202", code)
	}
	if !ended(alices) {
		t.Error("alice's cancel did not end her own request")
	}
	if ended(bobs) {
		t.Fatal("alice's cancel ended bob's request with the same id")
	}

	h.post(bob, cancelOf(`7`))
	if !ended(bobs) {
		t.Error("bob's cancel did not end his request")
	}

	select {
	case body := <-h.forwarded:
		t.Errorf("a cancel reached mcp-go: %s", body)
	default:
	}
}

// Every spelling mcp-go folds to the same id names the same request here too,
// and a cancel never reaches mcp-go whatever its case or duplicate keys.
func TestCancelReadsTheTargetAsMCPGoDoes(t *testing.T) {
	for _, body := range []string{
		cancelOf(`7.0`),
		cancelOf(`7e0`),
		cancelOf(`"7"`),
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7,"RequestId":999}}`,
		`{"jsonrpc":"2.0","METHOD":"notifications/cancelled","params":{"requestId":7}}`,
	} {
		h := newCancelHarness()
		bobs := h.open(t, bob, `7`)

		h.post(alice, body)
		if ended(bobs) {
			t.Errorf("alice ended bob's request 7 with %s", body)
		}

		select {
		case forwarded := <-h.forwarded:
			t.Errorf("forwarded to mcp-go: %s", forwarded)
		default:
		}

		h.post(bob, body)
		if !ended(bobs) {
			t.Errorf("bob's own cancel, spelled %s, did not end his request", body)
		}
	}
}

func TestOtherMessagesPassThrough(t *testing.T) {
	h := newCancelHarness()
	h.post(alice, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	select {
	case <-h.forwarded:
	default:
		t.Error("a notification other than a cancel did not reach mcp-go")
	}

}

// Through mcp-go itself: a cancel ends the request it names by ending the
// context mcp-go serves it under.
func TestCancelEndsARequestMCPGoIsServing(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))
	handler := srv.Handler()
	stream := startListenStream(t, handler, "cetacean://services/svc1")
	stream.await(t, "notifications/subscriptions/acknowledged")

	cancel := modernRequest(t, 0, "notifications/cancelled", `{"requestId":1}`)
	body, _ := io.ReadAll(cancel.Body)
	cancel.Body = io.NopCloser(strings.NewReader(strings.Replace(string(body), `"id":0,`, "", 1)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, cancel)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("cancel: status = %d, want 202", rec.Code)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-stream.notifications:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("the cancelled listen stream kept running")
		}
	}
}
