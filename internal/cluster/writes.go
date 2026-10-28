package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// Writer is the Docker surface the write use cases call.
type Writer interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	UpdateServiceImage(ctx context.Context, id, image string) (swarm.Service, error)
	RollbackService(ctx context.Context, id string) (swarm.Service, error)
	RestartService(ctx context.Context, id string) (swarm.Service, error)
}

// Caller is who asks for a write, as a transport established it.
type Caller struct {
	CanRead, CanWrite func(resource string) bool
	Level             config.OperationsLevel
}

// Writes performs the writes both transports offer, end to end: resolving the
// target, the ACL and the tier, the domain rules, the Docker call, and the
// classification of what it returned. A transport pins a version on ctx.
type Writes struct {
	cache  *cache.Cache
	docker Writer
}

// NewWrites returns the use cases over c, writing through docker.
func NewWrites(c *cache.Cache, docker Writer) *Writes {
	return &Writes{cache: c, docker: docker}
}

var (
	// ErrScaleGlobal refuses scaling a service that runs one task per node.
	ErrScaleGlobal = errors.New("cannot scale a global-mode service")
	// ErrNoPreviousSpec refuses rolling back a service never updated.
	ErrNoPreviousSpec = errors.New("service has no previous spec to roll back to")
)

// ScaleService sets a replicated service's desired replica count.
func (w *Writes) ScaleService(
	ctx context.Context,
	c Caller,
	key string,
	replicas uint64,
) (swarm.Service, error) {
	svc, err := w.service(c, OpScaleService, key)
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.Spec.Mode.Replicated == nil {
		return swarm.Service{}, invalid("service", ErrScaleGlobal)
	}

	return refuse(w.docker.ScaleService(ctx, svc.ID, replicas))(key)
}

// UpdateServiceImage points a service at another image.
func (w *Writes) UpdateServiceImage(
	ctx context.Context,
	c Caller,
	key, image string,
) (swarm.Service, error) {
	image = ServiceImage(image)
	if image == "" {
		return swarm.Service{}, invalid("service", errors.New("image is required"))
	}

	svc, err := w.service(c, OpUpdateServiceImage, key)
	if err != nil {
		return swarm.Service{}, err
	}

	if err := CheckHostAccessHeld(
		svc,
		"changing the image of",
		w.cache.GetVolume,
		c.Level,
	); err != nil {
		return refuse(swarm.Service{}, err)(key)
	}

	return refuse(w.docker.UpdateServiceImage(ctx, svc.ID, image))(key)
}

// RollbackService returns a service to its previous spec.
func (w *Writes) RollbackService(ctx context.Context, c Caller, key string) (swarm.Service, error) {
	svc, err := w.service(c, OpRollbackService, key)
	if err != nil {
		return swarm.Service{}, err
	}
	if svc.PreviousSpec == nil {
		return swarm.Service{}, invalid("service", ErrNoPreviousSpec)
	}

	if err := CheckHostAccessHeld(svc, RollingBack, w.cache.GetVolume, c.Level); err != nil {
		return refuse(swarm.Service{}, err)(key)
	}

	return refuse(w.docker.RollbackService(ctx, svc.ID))(key)
}

// RestartService replaces every task of a service.
func (w *Writes) RestartService(ctx context.Context, c Caller, key string) (swarm.Service, error) {
	svc, err := w.service(c, OpRestartService, key)
	if err != nil {
		return swarm.Service{}, err
	}

	return refuse(w.docker.RestartService(ctx, svc.ID))(key)
}

// service resolves the service a write names. One the caller cannot read is
// not found, so a refusal never confirms it exists.
func (w *Writes) service(c Caller, op Operation, key string) (swarm.Service, error) {
	svc, found, err := w.cache.ResolveService(key)
	if err != nil {
		if !c.CanRead("service:" + key) {
			found = false
		} else {
			return swarm.Service{}, &WriteError{
				Kind: Invalid, Resource: "service", Detail: err.Error(), Err: err,
			}
		}
	}

	resource := "service:" + svc.Spec.Name
	if !found || !c.CanRead(resource) {
		return swarm.Service{}, &WriteError{
			Kind: NotFound, Resource: "service", Detail: fmt.Sprintf("service %q not found", key),
		}
	}
	if !c.CanWrite(resource) {
		return swarm.Service{}, &WriteError{
			Kind: Denied, Resource: "service", Detail: "write access denied for " + resource,
		}
	}
	if c.Level < op.Tier() {
		return swarm.Service{}, &WriteError{
			Kind:     BelowTier,
			Resource: "service",
			Detail: fmt.Sprintf(
				"this operation requires operations level %d, but the caller is at level %d",
				op.Tier(), c.Level,
			),
		}
	}

	return svc, nil
}

// refuse classifies what a write returned for the key the caller named. An
// error that is none of the kinds is Cetacean's own and passes as it is.
func refuse(svc swarm.Service, err error) func(key string) (swarm.Service, error) {
	return func(key string) (swarm.Service, error) {
		if err == nil {
			return svc, nil
		}
		if refusal, ok := ClassifyWrite(err, "service", key); ok {
			return swarm.Service{}, refusal
		}

		return swarm.Service{}, err
	}
}

func invalid(resource string, err error) *WriteError {
	return &WriteError{Kind: Invalid, Resource: resource, Detail: err.Error(), Err: err}
}
