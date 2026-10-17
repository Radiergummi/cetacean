package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// inflight holds a cancel for every request being served, by caller and id.
// The stateless transport has no session to scope an id to, so mcp-go keys its
// own by id alone; scopeCancels cancels through this instead.
type inflight struct {
	mu      sync.Mutex
	cancels map[string]map[*context.CancelFunc]struct{}
}

func (f *inflight) add(key string, cancel context.CancelFunc) func() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cancels == nil {
		f.cancels = map[string]map[*context.CancelFunc]struct{}{}
	}
	if f.cancels[key] == nil {
		f.cancels[key] = map[*context.CancelFunc]struct{}{}
	}
	f.cancels[key][&cancel] = struct{}{}

	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()

		delete(f.cancels[key], &cancel)
		if len(f.cancels[key]) == 0 {
			delete(f.cancels, key)
		}
	}
}

func (f *inflight) cancel(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for cancel := range f.cancels[key] {
		(*cancel)()
	}
}

// inflightKey scopes a JSON-RPC id, folded as mcp-go folds it, to its caller.
func inflightKey(owner string, id any) string {
	return owner + "\x00" + fmt.Sprintf("%v", id)
}

// scopeCancels serves notifications/cancelled itself: it ends the sender's own
// requests with that id, answers 202, and never passes the cancel to mcp-go.
// It sits inside the bearer check, which puts the caller in r.
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

		// Decoded as mcp-go decodes it, so both see the same method and id:
		// the envelope through a struct, a notification's params as a map.
		var envelope struct {
			Method string          `json:"method"`
			ID     any             `json:"id"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &envelope) != nil {
			next.ServeHTTP(w, r)
			return
		}

		owner := taskOwner(r.Context())

		if envelope.Method == string(mcplib.MethodNotificationCancelled) {
			var params map[string]any
			if json.Unmarshal(envelope.Params, &params) == nil {
				if id, ok := params["requestId"]; ok {
					s.inflight.cancel(inflightKey(owner, id))
				}
			}

			w.WriteHeader(http.StatusAccepted)

			return
		}

		if envelope.ID != nil {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			defer s.inflight.add(inflightKey(owner, envelope.ID), cancel)()

			r = r.WithContext(ctx)
		}

		next.ServeHTTP(w, r)
	})
}
