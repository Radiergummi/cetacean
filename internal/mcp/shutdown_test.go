package mcp

import (
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/cache"
)

// A listen stream otherwise holds http.Server.Shutdown until its client leaves.
func TestListenStreamEndsAtShutdown(t *testing.T) {
	lifetime := sse.NewLifetime()
	srv := newResourceTestServer(t, cache.New(nil), func(o *Options) { o.Streams = lifetime })
	stream := startListenStream(t, srv.Handler(), "cetacean://services/svc1")

	stream.await(t, "notifications/subscriptions/acknowledged")
	lifetime.End()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-stream.notifications:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("the listen stream outlived shutdown")
		}
	}
}
