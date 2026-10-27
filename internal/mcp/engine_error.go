package mcp

import (
	"errors"
	"log/slog"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/radiergummi/cetacean/internal/docker"
)

// errEngineFailure is what a caller is told of a failure the engine reports as
// its own, whose text may carry daemon internals; REST's ENG004 withholds it too.
var errEngineFailure = errors.New(
	"the Docker engine failed to carry out the write; Cetacean's log has its reason",
)

// engineFailure logs and withholds such a failure. An error the caller can act
// on, or one Cetacean raised itself, passes through unchanged.
func engineFailure(err error) error {
	ownFailure := cerrdefs.IsInternal(err) || cerrdefs.IsUnknown(err) ||
		cerrdefs.IsDataLoss(err) || cerrdefs.IsNotImplemented(err) ||
		cerrdefs.IsPermissionDenied(err) || cerrdefs.IsUnauthorized(err)
	if !ownFailure || docker.IsOutOfSequence(err) {
		return err
	}

	slog.Error("the engine refused an MCP write", "error", err)

	return errEngineFailure
}
