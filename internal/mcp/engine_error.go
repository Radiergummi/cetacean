package mcp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// errEngineFailure is what a caller is told of an engine failure whose text
// may carry daemon internals; REST's ENG004 withholds it too.
var errEngineFailure = errors.New(
	"the Docker engine failed to carry out the write; Cetacean's log has its reason",
)

// writeFailure tells the caller what cluster.ClassifyWrite lets it see of a
// refused write. Cetacean's own errors pass as they are.
func writeFailure(ctx context.Context, err error, resource, key string) error {
	refusal, ok := cluster.ClassifyWrite(err, resource, key)
	if !ok {
		return err
	}

	return refusalError(ctx, refusal)
}

func refusalError(ctx context.Context, refusal *cluster.WriteError) error {
	if refusal.Kind == cluster.Engine {
		slog.ErrorContext(ctx, "the engine refused an MCP write", "error", refusal.Err)

		return errEngineFailure
	}

	return refusal
}
