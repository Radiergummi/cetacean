package cluster

import (
	"errors"
	"fmt"
	"net"
	"net/url"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/client"

	"github.com/radiergummi/cetacean/internal/docker"
)

// WriteErrorKind is the class of a refused write each transport maps to its own answer.
type WriteErrorKind int

const (
	NotFound WriteErrorKind = iota + 1
	Denied
	BelowTier
	HostAccess
	Invalid
	Conflict
	Stale
	Unavailable
	Engine
)

// WriteError is why a write did not happen, in terms both transports can map.
type WriteError struct {
	Kind     WriteErrorKind
	Resource string
	Detail   string // safe to show the caller; empty for Engine
	Err      error  // the cause, for the log
}

func (e *WriteError) Error() string {
	if e.Detail == "" {
		return "the " + e.Resource + " write failed"
	}

	return e.Detail
}

func (e *WriteError) Unwrap() error {
	return e.Err
}

// ClassifyWrite turns the error a write returned into a WriteError, and is the
// one place that decides which engine text a caller may see. It reports false
// for an error that is Cetacean's own and none of the kinds: its text is ours.
func ClassifyWrite(err error, resource, key string) (*WriteError, bool) {
	refusal := func(kind WriteErrorKind, detail string) (*WriteError, bool) {
		return &WriteError{Kind: kind, Resource: resource, Detail: detail, Err: err}, true
	}

	if classified, ok := errors.AsType[*WriteError](err); ok {
		return classified, true
	}
	if _, ok := errors.AsType[*HostAccessError](err); ok {
		return refusal(HostAccess, err.Error())
	}
	if _, ok := errors.AsType[*AttachDeniedError](err); ok {
		return refusal(Denied, err.Error())
	}
	if _, ok := errors.AsType[*StackLabelDeniedError](err); ok {
		return refusal(Denied, err.Error())
	}
	if docker.IsOutOfSequence(err) {
		return refusal(Stale, fmt.Sprintf(
			"%s %q changed since it was read; nothing was written", resource, key,
		))
	}
	if !fromEngine(err) {
		return nil, false
	}

	switch {
	case cerrdefs.IsNotFound(err):
		return refusal(NotFound, fmt.Sprintf("%s %q not found", resource, key))
	case cerrdefs.IsInvalidArgument(err):
		return refusal(Invalid, err.Error())
	case cerrdefs.IsUnavailable(err):
		return refusal(Unavailable, err.Error())
	case cerrdefs.IsConflict(err):
		return refusal(Conflict, err.Error())
	case cerrdefs.IsFailedPrecondition(err):
		return refusal(Conflict, fmt.Sprintf(
			"%s %q cannot be changed in its current state", resource, key,
		))
	}

	return refusal(Engine, "")
}

// ClassifyRemoval is ClassifyWrite for a removal, whose conflict detail is
// our own: the engine's text names what is in the way, which the caller may
// not be allowed to see.
func ClassifyRemoval(err error, resource, key string) (*WriteError, bool) {
	if IsRemovalConflict(err, resource) {
		return &WriteError{
			Kind:     Conflict,
			Resource: resource,
			Detail:   RemovalConflictDetail(resource, key),
			Err:      err,
		}, true
	}

	return ClassifyWrite(err, resource, key)
}

// fromEngine reports an error the engine classified, which every API answer
// is, or a failure to reach it: the client's own, which names the socket and
// carries no class, or a raw transport error. Resolve answers ErrUnknown for
// an unclassified error too, so a deliberate Unknown is asked for separately.
func fromEngine(err error) bool {
	_, transport := errors.AsType[*url.Error](err)
	if _, dial := errors.AsType[net.Error](err); dial || client.IsErrConnectionFailed(err) {
		transport = true
	}

	return transport || cerrdefs.IsUnknown(err) ||
		!errors.Is(cerrdefs.Resolve(err), cerrdefs.ErrUnknown)
}
