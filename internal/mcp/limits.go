package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/api/sse"
)

const (
	// maxRequestBytes caps a JSON-RPC body at the REST writes' limit.
	maxRequestBytes = 1 << 20

	// maxListenStreams bounds open subscriptions/listen streams. Each holds a
	// connection with no write timeout, so they accumulate without one.
	maxListenStreams = 64

	// maxLogReads bounds get_logs calls and logs resource reads in flight, by
	// the REST log reads' numbers. The two transports hold separate pools.
	maxLogReads = 32
)

// remoteAddrKey carries the connection's address into handler contexts, as
// the per-caller share of a cap keys an unauthenticated caller on it.
type remoteAddrKey struct{}

// claimLogRead takes a log-read slot for the caller, refusing past the cap or
// past the caller's quarter share of it.
func (s *Server) claimLogRead(ctx context.Context) (release func(), err error) {
	addr, _ := ctx.Value(remoteAddrKey{}).(string)
	r := (&http.Request{RemoteAddr: addr}).WithContext(ctx)

	release, callerFull, ok := s.logReads.Acquire(r, maxLogReads)
	if !ok {
		if callerFull {
			return nil, errors.New("too many log reads in flight from this client; retry shortly")
		}

		return nil, errors.New("too many log reads in flight; retry shortly")
	}

	return release, nil
}

// limitBody refuses a body over maxRequestBytes before mcp-go reads it whole.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
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
		next.ServeHTTP(w, r)
	})
}

// limitListens refuses a listen stream past maxListenStreams as the SSE caps
// do. It sits inside the origin and bearer checks, which must answer first,
// and reads a body limitBody has already bounded.
func (s *Server) limitListens(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.listens == nil || r.Method != http.MethodPost {
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
			Method mcplib.MCPMethod `json:"method"`
		}
		if json.Unmarshal(body, &envelope) != nil ||
			envelope.Method != mcplib.MethodSubscriptionsListen {
			next.ServeHTTP(w, r)
			return
		}

		select {
		case s.listens <- struct{}{}:
			defer func() { <-s.listens }()
		default:
			w.Header().Set("Retry-After", sse.RetryAfter())
			http.Error(w, "too many open subscription streams", http.StatusTooManyRequests)

			return
		}

		r, release := s.streams.Bind(r)
		defer release()

		next.ServeHTTP(w, r)
	})
}
