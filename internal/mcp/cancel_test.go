package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

// The stateless transport keys a request's cancel by its JSON-RPC id alone, so
// without the guard any caller could cancel another's request by naming its id.
func TestCancelReachesOnlyTheCallersOwnRequest(t *testing.T) {
	alice := &auth.Identity{Provider: "oidc", Subject: "alice"}
	bob := &auth.Identity{Provider: "oidc", Subject: "bob"}

	var (
		mu        sync.Mutex
		forwarded []string
	)
	started, release := make(chan struct{}), make(chan struct{})

	srv := &Server{}
	handler := srv.scopeCancels(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"tools/call"`) {
			close(started)
			<-release
			return
		}
		mu.Lock()
		forwarded = append(forwarded, auth.IdentityFromContext(r.Context()).Subject)
		mu.Unlock()
	}))

	post := func(id *auth.Identity, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		r = r.WithContext(auth.ContextWithIdentity(r.Context(), id))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		post(bob, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`)
	}()
	<-started

	cancel := func(requestID string) string {
		return `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":` + requestID + `}}`
	}

	if w := post(alice, cancel(`7`)); w.Code != http.StatusAccepted {
		t.Errorf("alice's cancel of bob's request: status = %d, want 202", w.Code)
	}
	if w := post(alice, cancel(`"7"`)); w.Code != http.StatusAccepted {
		t.Errorf("the same id as a string: status = %d, want 202", w.Code)
	}
	post(bob, cancel(`7`))
	post(alice, cancel(`8`))

	close(release)
	<-done

	mu.Lock()
	defer mu.Unlock()
	if strings.Join(forwarded, ",") != "bob,alice" {
		t.Errorf(
			"forwarded cancels from %v, want bob's own and alice's for an id nobody else holds",
			forwarded,
		)
	}
}
