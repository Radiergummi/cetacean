package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"github.com/radiergummi/cetacean/internal/config"
)

// The engine's text, or the client's on failing to reach it, may carry daemon
// internals, so it reaches the caller only for the classes REST shows too; the
// rest is logged, as REST's ENG004 does. Cetacean's own errors keep their text.
func TestAnEngineFailureIsReportedWithoutItsText(t *testing.T) {
	// No daemon listens here, so the real client fails to reach it.
	const internals = "/tmp/cetacean-engine-error-test.sock"
	_ = os.Remove(internals)

	dial := &net.OpError{
		Op: "dial", Net: "unix",
		Addr: &net.UnixAddr{Name: internals, Net: "unix"},
		Err:  syscall.ECONNREFUSED,
	}

	for _, tc := range []struct {
		name  string
		err   error
		shown bool
	}{
		{"internal", fmt.Errorf("%w: %s", cerrdefs.ErrInternal, internals), false},
		{"unknown", fmt.Errorf("%w: %s", cerrdefs.ErrUnknown, internals), false},
		{"permission", fmt.Errorf("%w: %s", cerrdefs.ErrPermissionDenied, internals), false},
		{"already exists", fmt.Errorf("%w: %s", cerrdefs.ErrAlreadyExists, internals), false},
		{"unreachable", fmt.Errorf("error during connect: %w", dial), false},
		{"no daemon", unreachableDaemon(t, internals), false},
		{"invalid", fmt.Errorf("%w: %s", cerrdefs.ErrInvalidArgument, internals), true},
		{"not found", fmt.Errorf("%w: %s", cerrdefs.ErrNotFound, internals), true},
		{"unavailable", fmt.Errorf("%w: %s", cerrdefs.ErrUnavailable, internals), true},
		{"conflict", fmt.Errorf("%w: %s", cerrdefs.ErrConflict, internals), true},
		{"out of sequence", errors.New("update out of sequence " + internals), true},
		{"cetacean's own", errors.New("section \"ports\": " + internals), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeWriteClient{
				scaleServiceFn: func(context.Context, string, uint64) (swarm.Service, error) {
					return swarm.Service{}, tc.err
				},
			}

			srv := newToolTestServer(t, attachmentTestCache(t), f, config.OpsImpactful)
			td, _ := srv.findTool("scale_service")

			_, err := td.handler(context.Background(), newCallToolRequest("scale_service",
				map[string]any{"id": "web", "replicas": float64(2)}))
			if err == nil {
				t.Fatal("the failure was not reported")
			}

			if shown := strings.Contains(err.Error(), internals); shown != tc.shown {
				t.Errorf("error %q shows the text = %t, want %t", err, shown, tc.shown)
			}
		})
	}
}

// unreachableDaemon is the error the Docker client itself returns for a socket
// nothing listens on; it carries the socket path and no errdefs class.
func unreachableDaemon(t *testing.T, socket string) error {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.WithHost("unix://" + socket))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	_, err = cli.Ping(context.Background())
	if !client.IsErrConnectionFailed(err) {
		t.Fatalf("Ping = %v, want a connection failure", err)
	}

	return err
}
