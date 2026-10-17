package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// inflight records who sent each request still being served, by JSON-RPC id.
// The stateless transport has no session to scope an id to, so mcp-go cancels
// whichever request holds it; scopeCancels lets a caller name only its own.
type inflight struct {
	mu     sync.Mutex
	owners map[string]map[string]int // request id -> owner -> requests open
}

func (f *inflight) add(id, owner string) func() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.owners == nil {
		f.owners = map[string]map[string]int{}
	}
	if f.owners[id] == nil {
		f.owners[id] = map[string]int{}
	}
	f.owners[id][owner]++

	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		if f.owners[id][owner]--; f.owners[id][owner] == 0 {
			delete(f.owners[id], owner)
		}
		if len(f.owners[id]) == 0 {
			delete(f.owners, id)
		}
	}
}

// heldByOthers reports whether a request with id is open for a caller other
// than owner, whose cancel could then reach it.
func (f *inflight) heldByOthers(id, owner string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for held := range f.owners[id] {
		if held != owner {
			return true
		}
	}

	return false
}

// requestKey folds a JSON-RPC id to the text mcp-go keys it by, where 7 and
// "7" collide.
func requestKey(raw json.RawMessage) string {
	return strings.Trim(string(bytes.TrimSpace(raw)), `"`)
}

// scopeCancels drops a notifications/cancelled naming a request another caller
// has open, answering 202 as for any notification; a receiver may ignore a
// cancel. It sits inside the bearer check, which puts the caller in r.
func (s *Server) scopeCancels(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))

		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				RequestID json.RawMessage `json:"requestId"`
			} `json:"params"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			next.ServeHTTP(w, r)
			return
		}

		owner := taskOwner(r.Context())

		switch {
		case envelope.Method == string(mcplib.MethodNotificationCancelled):
			if s.inflight.heldByOthers(requestKey(envelope.Params.RequestID), owner) {
				w.WriteHeader(http.StatusAccepted)
				return
			}
		case len(envelope.ID) > 0 && string(envelope.ID) != "null":
			defer s.inflight.add(requestKey(envelope.ID), owner)()
		}

		next.ServeHTTP(w, r)
	})
}
