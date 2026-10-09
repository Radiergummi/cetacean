package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/api/sse"
)

const (
	// maxRequestBytes caps a JSON-RPC body, matching the REST writes. A
	// secret or config, the largest thing a tool takes, stays under it.
	maxRequestBytes = 1 << 20

	// maxListenStreams bounds open subscriptions/listen streams. Each holds a
	// connection with no write timeout, so they accumulate without one.
	maxListenStreams = 64
)

// limitRequests refuses a body over maxRequestBytes before mcp-go reads it
// whole, and a listen stream past maxListenStreams.
func (s *Server) limitRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
		if err != nil {
			if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}

			http.Error(w, "failed to read request body", http.StatusBadRequest)

			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))

		var envelope struct {
			Method mcplib.MCPMethod `json:"method"`
		}
		if s.listens == nil || json.Unmarshal(body, &envelope) != nil ||
			envelope.Method != mcplib.MethodSubscriptionsListen {
			next.ServeHTTP(w, r)
			return
		}

		select {
		case s.listens <- struct{}{}:
			defer func() { <-s.listens }()
		default:
			w.Header().Set("Retry-After", sse.RetryAfter())
			http.Error(w, "too many open subscription streams", http.StatusServiceUnavailable)

			return
		}

		next.ServeHTTP(w, r)
	})
}
