package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/config"
)

// The engine's account of its own failure may carry daemon internals, so it
// is logged rather than handed to the caller, as REST's ENG004 does. An error
// the caller can act on keeps its text.
func TestAnEngineFailureIsReportedWithoutItsText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		shown bool
	}{
		{"internal", fmt.Errorf("%w: open /var/lib/docker/swarm: denied", cerrdefs.ErrInternal), false},
		{"unknown", fmt.Errorf("%w: open /var/lib/docker/swarm: denied", cerrdefs.ErrUnknown), false},
		{"invalid", fmt.Errorf("%w: open /var/lib/docker/swarm: denied", cerrdefs.ErrInvalidArgument), true},
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
				t.Fatal("the engine's failure was not reported")
			}

			if shown := strings.Contains(err.Error(), "/var/lib/docker"); shown != tc.shown {
				t.Errorf("error %q shows the engine's text = %t, want %t", err, shown, tc.shown)
			}
		})
	}
}
