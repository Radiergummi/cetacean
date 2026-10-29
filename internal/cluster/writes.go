package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
)

// Writer is the Docker surface the write use cases call.
type Writer interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	UpdateServiceImage(ctx context.Context, id, image string) (swarm.Service, error)
	RollbackService(ctx context.Context, id string) (swarm.Service, error)
	RestartService(ctx context.Context, id string) (swarm.Service, error)

	// UpdateServiceSpec hands change the spec Docker holds and writes back
	// what it left, so a merge never starts from the cache.
	UpdateServiceSpec(ctx context.Context, id string, change SpecChange) (swarm.Service, error)

	UpdateNodeAvailability(
		ctx context.Context,
		id string,
		availability swarm.NodeAvailability,
	) (swarm.Node, error)
	UpdateNodeRole(ctx context.Context, id string, role swarm.NodeRole) (swarm.Node, error)
	UpdateNodeLabels(ctx context.Context, id string, change MapChange) (swarm.Node, error)

	RemoveService(ctx context.Context, id string) error
	RemoveTask(ctx context.Context, id string) error
	RemoveConfig(ctx context.Context, id string) error
	RemoveSecret(ctx context.Context, id string) error
	RemoveNetwork(ctx context.Context, id string) error
	RemoveVolume(ctx context.Context, name string, force bool) error

	CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error)
	CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
	InspectConfig(ctx context.Context, id string) (swarm.Config, error)
	InspectSecret(ctx context.Context, id string) (swarm.Secret, error)
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

// The domain rules a lifecycle write is refused by. The writer applies the
// first two again against the live spec, so either path names the same rule.
var (
	ErrScaleGlobal    = docker.ErrScaleGlobal
	ErrNoPreviousSpec = docker.ErrNoPreviousSpec
	ErrImageRequired  = errors.New("image is required")
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

	slog.InfoContext(ctx, "scaling service", "service", svc.ID, "replicas", replicas)
	updated, err := w.docker.ScaleService(ctx, svc.ID, replicas)

	return updated, classify(err, "service", key)
}

// UpdateServiceImage points a service at another image.
func (w *Writes) UpdateServiceImage(
	ctx context.Context,
	c Caller,
	key, image string,
) (swarm.Service, error) {
	image = ServiceImage(image)
	if image == "" {
		return swarm.Service{}, invalid("service", ErrImageRequired)
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
		return swarm.Service{}, classify(err, "service", key)
	}

	slog.InfoContext(ctx, "updating service image", "service", svc.ID, "image", image)
	updated, err := w.docker.UpdateServiceImage(ctx, svc.ID, image)

	return updated, classify(err, "service", key)
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
		return swarm.Service{}, classify(err, "service", key)
	}

	slog.InfoContext(ctx, "rolling back service", "service", svc.ID)
	updated, err := w.docker.RollbackService(ctx, svc.ID)

	return updated, classify(err, "service", key)
}

// RestartService replaces every task of a service.
func (w *Writes) RestartService(ctx context.Context, c Caller, key string) (swarm.Service, error) {
	svc, err := w.service(c, OpRestartService, key)
	if err != nil {
		return swarm.Service{}, err
	}

	slog.InfoContext(ctx, "restarting service", "service", svc.ID)
	updated, err := w.docker.RestartService(ctx, svc.ID)

	return updated, classify(err, "service", key)
}

// UpdateService applies change to the service's live spec, refusing what
// checkServiceChange refuses. op is the section's operation, for its tier.
func (w *Writes) UpdateService(
	ctx context.Context,
	c Caller,
	op Operation,
	key string,
	change SpecChange,
) (swarm.Service, error) {
	svc, err := w.service(c, op, key)
	if err != nil {
		return swarm.Service{}, err
	}

	slog.InfoContext(ctx, "updating service", "service", svc.ID, "operation", op)
	updated, err := w.docker.UpdateServiceSpec(ctx, svc.ID, w.CheckedChange(c, change))

	return updated, classify(err, "service", key)
}

// CheckedChange applies change to the live spec, then refuses the result
// unless checkServiceChange passes it against a copy taken before.
func (w *Writes) CheckedChange(c Caller, change SpecChange) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		live, err := cloneSpec(spec)
		if err != nil {
			return err
		}
		if err := change(spec); err != nil {
			return err
		}

		return w.checkServiceChange(c, *live, spec)
	}
}

// checkServiceChange refuses a change from live to next the caller may not
// make: an attachment it cannot read, a stack it cannot write, host access
// below level 3, or any change to what runs in a service that holds some.
func (w *Writes) checkServiceChange(
	c Caller,
	live swarm.ServiceSpec,
	next *swarm.ServiceSpec,
) error {
	current := swarm.Service{Spec: live}
	is := containerOf(next)

	for _, ref := range is.Secrets {
		name := ref.SecretName
		if sec, ok := w.cache.GetSecret(ref.SecretID); ok {
			name = sec.Spec.Name
		}
		if err := CheckAttachable(
			current,
			c.CanRead,
			AttachSecret,
			ref.SecretID,
			name,
		); err != nil {
			return err
		}
	}
	for _, ref := range is.Configs {
		name := ref.ConfigName
		if cfg, ok := w.cache.GetConfig(ref.ConfigID); ok {
			name = cfg.Spec.Name
		}
		if err := CheckAttachable(
			current,
			c.CanRead,
			AttachConfig,
			ref.ConfigID,
			name,
		); err != nil {
			return err
		}
	}
	for _, ref := range next.TaskTemplate.Networks {
		name := ref.Target
		if network, ok := w.cache.GetNetwork(ref.Target); ok {
			name = network.Name
		}
		if err := CheckAttachable(current, c.CanRead, AttachNetwork, ref.Target, name); err != nil {
			return err
		}
	}
	if err := CheckVolumesAttachable(current, c.CanRead, is.Mounts); err != nil {
		return err
	}
	if err := CheckStackLabel(live.Labels, next.Labels, c.CanWrite); err != nil {
		return err
	}
	if err := CheckMounts(current, is.Mounts, w.cache.GetVolume, c.Level); err != nil {
		return err
	}
	if err := CheckCapabilities(current, is.CapabilityAdd, c.Level); err != nil {
		return err
	}

	return CheckHostAccessHeld(current, specChange(&live, next), w.cache.GetVolume, c.Level)
}

// specChange names the first change from before to after that a *Change
// function finds, or returns "" when there is none.
func specChange(before, after *swarm.ServiceSpec) string {
	was, is := containerOf(before), containerOf(after)

	for _, change := range []string{
		CommandChange(was, is),
		HealthcheckChange(was.Healthcheck, is.Healthcheck),
		SecretsChange(was.Secrets, is.Secrets),
		ConfigsChange(was.Configs, is.Configs),
		MountsChange(was.Mounts, is.Mounts),
		EnvChange(envMap(was.Env, true), envMap(is.Env, true)),
		LabelsChange(before.Labels, after.Labels),
		PortsChange(endpointPorts(before), endpointPorts(after)),
		NetworksChange(before.TaskTemplate.Networks, after.TaskTemplate.Networks),
		PlacementChange(before.TaskTemplate.Placement, after.TaskTemplate.Placement),
	} {
		if change != "" {
			return change
		}
	}

	return ""
}

func containerOf(spec *swarm.ServiceSpec) *swarm.ContainerSpec {
	if spec.TaskTemplate.ContainerSpec == nil {
		return &swarm.ContainerSpec{}
	}

	return spec.TaskTemplate.ContainerSpec
}

func endpointPorts(spec *swarm.ServiceSpec) []swarm.PortConfig {
	if spec.EndpointSpec == nil {
		return nil
	}

	return spec.EndpointSpec.Ports
}

// cloneSpec copies a spec as Docker's API carries it, so nothing in the copy
// shares a pointer, slice or map with the spec a change then edits.
func cloneSpec(spec *swarm.ServiceSpec) (*swarm.ServiceSpec, error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}

	var clone swarm.ServiceSpec
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, err
	}

	return &clone, nil
}

// service resolves the service a write names.
func (w *Writes) service(c Caller, op Operation, key string) (swarm.Service, error) {
	record, err := w.record(c, op, "service", key)
	if err != nil {
		return swarm.Service{}, err
	}

	// Removed since the lookup: refused as one never there would be.
	svc, ok := w.cache.GetService(record.ID)
	if !ok {
		return swarm.Service{}, &WriteError{
			Kind: NotFound, Resource: "service", Detail: fmt.Sprintf("service %q not found", key),
		}
	}

	return svc, nil
}

// record resolves the record of kind a write names, then judges the caller
// against it. One the caller cannot read is not found, so a refusal never
// confirms it exists.
func (w *Writes) record(c Caller, op Operation, kind, key string) (Record, error) {
	record, found, err := LookupAs(w.cache, kind, key, c.CanRead)
	if err != nil {
		if !c.CanRead(kind + ":" + key) {
			found = false
		} else {
			return Record{}, &WriteError{
				Kind:     Invalid,
				Resource: kind,
				Detail:   err.Error(),
				Err:      err,
			}
		}
	}

	resource := record.WriteResource()
	if !found || !c.CanRead(resource) {
		return Record{}, &WriteError{
			Kind: NotFound, Resource: kind, Detail: fmt.Sprintf("%s %q not found", kind, key),
		}
	}
	if !c.CanWrite(resource) {
		return Record{}, denied(kind, resource)
	}

	return record, belowTier(c, op, kind)
}

func denied(kind, resource string) *WriteError {
	return &WriteError{Kind: Denied, Resource: kind, Detail: "write access denied for " + resource}
}

// belowTier refuses a caller below op's tier, or returns nil.
func belowTier(c Caller, op Operation, kind string) error {
	if c.Level >= op.Tier() {
		return nil
	}

	return &WriteError{
		Kind:     BelowTier,
		Resource: kind,
		Detail: fmt.Sprintf(
			"this operation requires operations level %d, but the caller is at level %d",
			op.Tier(), c.Level,
		),
	}
}

// classify turns what a write returned into the refusal for the key the caller
// named. An error that is none of the kinds is Cetacean's own and passes.
func classify(err error, resource, key string) error {
	if err == nil {
		return nil
	}
	if refusal, ok := ClassifyWrite(err, resource, key); ok {
		return refusal
	}

	return err
}

func invalid(resource string, err error) *WriteError {
	return &WriteError{Kind: Invalid, Resource: resource, Detail: err.Error(), Err: err}
}
