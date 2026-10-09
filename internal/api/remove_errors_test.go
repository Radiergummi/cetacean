package api

import (
	"errors"
	"fmt"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
)

// Shaped like the engine's answers: the class the client derives from the
// status, and the message swarmkit or the volume service wrote.
func TestIsRemovalConflict(t *testing.T) {
	cases := []struct {
		resource string
		err      error
		want     bool
	}{
		{
			"network",
			fmt.Errorf(
				"%w: error while removing network: network web has active endpoints",
				cerrdefs.ErrPermissionDenied,
			),
			true,
		},
		{
			"node",
			fmt.Errorf(
				"%w: rpc error: code = FailedPrecondition desc = node abc is not down and can't be removed",
				cerrdefs.ErrInvalidArgument,
			),
			true,
		},
		{
			"node",
			fmt.Errorf(
				"%w: node abc is a cluster manager and is a member of the raft cluster. It must be demoted to worker before removal",
				cerrdefs.ErrInvalidArgument,
			),
			true,
		},
		{
			"volume",
			fmt.Errorf("%w: remove data: volume is in use - [19625d2c]", cerrdefs.ErrConflict),
			true,
		},
		{"network", errors.New("connection reset"), false},
		{"volume", fmt.Errorf("%w: invalid volume name", cerrdefs.ErrInvalidArgument), false},
	}
	for _, tc := range cases {
		if got := isRemovalConflict(tc.err, tc.resource); got != tc.want {
			t.Errorf("%s %q: got %v, want %v", tc.resource, tc.err, got, tc.want)
		}
	}
}
