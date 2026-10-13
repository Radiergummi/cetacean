package docker

import (
	"context"

	"github.com/docker/docker/api/types/swarm"
)

type pinnedVersionKey struct{}

type pinnedVersion struct {
	kind, id string
	version  swarm.Version
}

// WithPinnedVersion makes a versioned write to the kind ("service", "node",
// "config" or "secret") and ID name version instead of the one it reads, so
// the engine refuses it with "update out of sequence" if the resource has
// moved since a precondition validated that version.
func WithPinnedVersion(
	ctx context.Context,
	kind, id string,
	version swarm.Version,
) context.Context {
	return context.WithValue(ctx, pinnedVersionKey{}, pinnedVersion{kind, id, version})
}

// PinnedVersion reports the version pinned for kind and id, if any.
func PinnedVersion(ctx context.Context, kind, id string) (swarm.Version, bool) {
	pinned, ok := ctx.Value(pinnedVersionKey{}).(pinnedVersion)
	if !ok || pinned.kind != kind || pinned.id != id {
		return swarm.Version{}, false
	}

	return pinned.version, true
}

// writeVersion is the version a write names: the pinned one, or the one read.
func writeVersion(ctx context.Context, kind, id string, read swarm.Version) swarm.Version {
	if pinned, ok := PinnedVersion(ctx, kind, id); ok {
		return pinned
	}

	return read
}
