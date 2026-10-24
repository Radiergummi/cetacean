package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// holdLogReads takes n log-read slots for owner, releasing them at cleanup.
func holdLogReads(t *testing.T, srv *Server, n int, owner func(i int) *http.Request) {
	t.Helper()

	for i := range n {
		release, _, ok := srv.logReads.Acquire(owner(i), maxLogReads)
		if !ok {
			t.Fatalf("slot %d refused while filling", i)
		}
		t.Cleanup(release)
	}
}

func TestLogReadsAreCappedAcrossCallers(t *testing.T) {
	srv := newLogTestServer(t, cache.New(nil), &fakeLogStreamer{})

	holdLogReads(t, srv, maxLogReads, func(i int) *http.Request {
		return &http.Request{RemoteAddr: fmt.Sprintf("10.0.0.%d:1234", i)}
	})

	td, _ := srv.findTool("get_logs")
	_, err := td.handler(context.Background(), newCallToolRequest("get_logs", map[string]any{
		"service": "svc1",
	}))
	if err == nil || !strings.Contains(err.Error(), "too many log reads") {
		t.Fatalf("get_logs err = %v, want the log-read cap", err)
	}

	if _, err := srv.readServiceLogs(context.Background(), "svc1"); err == nil {
		t.Fatal("logs resource read succeeded past the log-read cap")
	}
}

func TestLogReadsCapOneCallerAtAQuarter(t *testing.T) {
	srv := newLogTestServer(t, cache.New(nil), &fakeLogStreamer{})

	alice := &auth.Identity{Provider: "oidc", Subject: "alice"}
	ctx := auth.ContextWithIdentity(context.Background(), alice)

	holdLogReads(t, srv, maxLogReads/4, func(int) *http.Request {
		return (&http.Request{}).WithContext(ctx)
	})

	td, _ := srv.findTool("get_logs")
	_, err := td.handler(ctx, newCallToolRequest("get_logs", map[string]any{"cluster": true}))
	if err == nil || !strings.Contains(err.Error(), "from this client") {
		t.Fatalf("get_logs err = %v, want this caller's share refused", err)
	}

	bob := auth.ContextWithIdentity(
		context.Background(),
		&auth.Identity{Provider: "oidc", Subject: "bob"},
	)
	if _, err := td.handler(bob, newCallToolRequest("get_logs", map[string]any{
		"service": "svc1",
	})); err != nil {
		t.Fatalf("another caller was refused: %v", err)
	}
}

// A read gives its slot back however it ends, or the cap fills with reads that
// already finished and refuses every caller from then on.
func TestLogReadsReleaseTheirSlot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		streamer *fakeLogStreamer
	}{
		{"success", &fakeLogStreamer{}},
		{"failure", &fakeLogStreamer{err: errors.New("daemon unreachable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newLogTestServer(t, cache.New(nil), tc.streamer)
			td, _ := srv.findTool("get_logs")

			for i := range maxLogReads + 1 {
				_, err := td.handler(context.Background(), newCallToolRequest(
					"get_logs",
					map[string]any{"service": "svc1"},
				))
				if err != nil && strings.Contains(err.Error(), "too many log reads") {
					t.Fatalf("get_logs %d refused by the cap: a slot leaked", i)
				}

				_, err = srv.readServiceLogs(context.Background(), "svc1")
				if err != nil && strings.Contains(err.Error(), "too many log reads") {
					t.Fatalf("logs resource read %d refused by the cap: a slot leaked", i)
				}
			}
		})
	}
}
