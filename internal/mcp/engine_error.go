package mcp

import (
	"errors"
	"log/slog"
	"net"
	"net/url"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/radiergummi/cetacean/internal/docker"
)

// errEngineFailure is what a caller is told of an engine failure whose text
// may carry daemon internals; REST's ENG004 withholds it too.
var errEngineFailure = errors.New(
	"the Docker engine failed to carry out the write; Cetacean's log has its reason",
)

// engineFailure lets through, of the errors that came from the engine or from
// failing to reach it, only those the caller can act on — the classes REST
// shows. The rest it logs and withholds. Cetacean's own errors carry no class.
func engineFailure(err error) error {
	if !fromEngine(err) || actionable(err) {
		return err
	}

	slog.Error("the engine refused an MCP write", "error", err)

	return errEngineFailure
}

func actionable(err error) bool {
	return cerrdefs.IsNotFound(err) || cerrdefs.IsInvalidArgument(err) ||
		cerrdefs.IsUnavailable(err) || cerrdefs.IsConflict(err) ||
		cerrdefs.IsFailedPrecondition(err) || docker.IsOutOfSequence(err)
}

// fromEngine reports an error the engine classified, which every API answer
// is, or a transport failure on the way to it. Resolve answers ErrUnknown for
// an unclassified error too, so a deliberate Unknown is asked for separately.
func fromEngine(err error) bool {
	_, transport := errors.AsType[*url.Error](err)
	if _, dial := errors.AsType[net.Error](err); dial {
		transport = true
	}

	return transport || cerrdefs.IsUnknown(err) ||
		!errors.Is(cerrdefs.Resolve(err), cerrdefs.ErrUnknown)
}
