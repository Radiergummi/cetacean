package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// The engine's refusal names what is in the way, which the caller may not be
// allowed to read: the tool says the removal was refused, not why.
func TestRemoveTools_RefusalDoesNotRepeatEngineText(t *testing.T) {
	c := cache.New(nil)
	c.SetNetwork(network.Summary{ID: "net1", Name: "web"})
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "app"}},
	})

	wc := &fakeWriteClient{
		removeConfigFn: func(context.Context, string) error {
			return fmt.Errorf(
				"%w: config 'app' is in use by the following service: hidden-service",
				cerrdefs.ErrInvalidArgument,
			)
		},
		removeNetworkFn: func(context.Context, string) error {
			return fmt.Errorf(
				"%w: rpc error: code = FailedPrecondition desc = network net1 is in use by task hidden-task",
				cerrdefs.ErrInvalidArgument,
			)
		},
		removeVolumeFn: func(context.Context, string, bool) error {
			return fmt.Errorf(
				"%w: remove data: volume is in use - [hidden-container]",
				cerrdefs.ErrConflict,
			)
		},
	}
	srv := newToolTestServer(t, c, wc, config.OpsImpactful)

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"remove_config", map[string]any{"id": "cfg1"}},
		{"remove_network", map[string]any{"id": "net1"}},
		{"remove_volume", map[string]any{"name": "data", "force": true}},
	}
	for _, tc := range cases {
		td, _ := srv.findTool(tc.tool)

		_, err := td.handler(context.Background(), newCallToolRequest(tc.tool, tc.args))
		if err == nil {
			t.Fatalf("%s: expected an error", tc.tool)
		}

		if strings.Contains(err.Error(), "hidden") ||
			!strings.Contains(err.Error(), "cannot be removed") {
			t.Errorf("%s: error = %q, want the generic refusal", tc.tool, err)
		}
	}
}
