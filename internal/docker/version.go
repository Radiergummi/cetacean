package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

type pinnedVersionKey struct{}

type pinnedVersion struct {
	kind, id string
	version  swarm.Version
}

// WithPinnedVersion makes a versioned write to the kind ("service", "node",
// "config" or "secret") and ID conditional on the record still being at
// version: once it has moved on, the write is refused with a
// *StaleVersionError and nothing is sent.
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

// StaleVersionError is a pinned write refused because the record is no longer
// at the pinned version.
type StaleVersionError struct {
	Kind, ID        string
	Pinned, Current uint64
}

func (e *StaleVersionError) Error() string {
	return fmt.Sprintf("%s %s: %s: pinned at version %d, now at %d",
		e.Kind, e.ID, sequenceConflict, e.Pinned, e.Current)
}

// sequenceConflict is Swarmkit's optimistic-concurrency refusal. A
// StaleVersionError repeats it, so one test recognises either.
const sequenceConflict = "update out of sequence"

// IsOutOfSequence reports whether err refused a write named at a version the
// record has moved past. Swarmkit raises it with gRPC code Unknown, which
// Docker renders as a bare 500, so only the message identifies it.
func IsOutOfSequence(err error) bool {
	return err != nil && strings.Contains(err.Error(), sequenceConflict)
}

// checkPinned refuses a write whose pinned version is not the one just read.
// The write then names the version read, so the engine refuses it in turn if
// the record moves between the read and the write.
func checkPinned(ctx context.Context, kind, id string, read swarm.Version) error {
	pinned, ok := PinnedVersion(ctx, kind, id)
	if !ok || pinned.Index == read.Index {
		return nil
	}

	return &StaleVersionError{Kind: kind, ID: id, Pinned: pinned.Index, Current: read.Index}
}
