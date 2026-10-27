package cluster

import (
	"reflect"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/config"
)

// HostAccessError reports a write that would reach the host from inside the
// container at an operations level below the one that may.
type HostAccessError struct {
	What string
}

func (e *HostAccessError) Error() string {
	return e.What + " requires operations level 3"
}

// CheckMounts refuses a mount that reaches the host below the impactful tier:
// a bind, a named pipe, or a volume whose driver options could bind one. A named
// volume lookup does not know, held by another node, counts as one. A mount the
// service already carries, unchanged, is exempt so tier 2 can edit the rest.
func CheckMounts(
	svc swarm.Service,
	requested []mount.Mount,
	lookup func(name string) (volume.Volume, bool),
	level config.OperationsLevel,
) error {
	if level >= config.OpsImpactful {
		return nil
	}

	var current []mount.Mount
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		current = svc.Spec.TaskTemplate.ContainerSpec.Mounts
	}

	for _, m := range requested {
		what := hostAccess(m, lookup)
		if what == "" {
			continue
		}

		if !slices.ContainsFunc(current, func(c mount.Mount) bool {
			return reflect.DeepEqual(withoutEmptyBindOptions(c), withoutEmptyBindOptions(m))
		}) {
			return &HostAccessError{What: what}
		}
	}

	return nil
}

// RollingBack is the change a rollback makes, the one change that counts the
// previous spec, since it makes that spec current.
const RollingBack = "rolling back"

// CheckHostAccessHeld refuses, below the impactful tier, a change to what runs in
// a service whose spec reaches the host: the exemptions in CheckMounts and
// CheckCapabilities would otherwise hand that access to new code. An empty
// change, one the *Change functions found nothing in, passes.
func CheckHostAccessHeld(
	svc swarm.Service,
	change string,
	lookup func(name string) (volume.Volume, bool),
	level config.OperationsLevel,
) error {
	if level >= config.OpsImpactful || change == "" {
		return nil
	}

	if what := HeldHostAccess(svc, change == RollingBack, lookup); what != "" {
		return &HostAccessError{What: change + " a service with " + what}
	}

	return nil
}

// HeldHostAccess names how svc reaches the host, or returns "" if it doesn't:
// by its current spec, or by either spec for a rollback.
func HeldHostAccess(
	svc swarm.Service,
	rollback bool,
	lookup func(name string) (volume.Volume, bool),
) string {
	// A volume the manager doesn't hold most likely lives on a worker; unlike
	// CheckMounts, a held mount of one is not presumed to reach the host.
	known := func(name string) (volume.Volume, bool) {
		if stored, ok := lookup(name); ok {
			return stored, true
		}

		return volume.Volume{Name: name, Driver: "local"}, true
	}

	specs := []*swarm.ServiceSpec{&svc.Spec}
	if rollback {
		specs = append(specs, svc.PreviousSpec)
	}

	for _, spec := range specs {
		if what := specHostAccess(spec, known); what != "" {
			return what
		}
	}

	return ""
}

// specHostAccess names the first way a spec's container reaches the host.
func specHostAccess(spec *swarm.ServiceSpec, lookup func(string) (volume.Volume, bool)) string {
	if spec == nil || spec.TaskTemplate.ContainerSpec == nil {
		return ""
	}

	cs := spec.TaskTemplate.ContainerSpec
	for _, m := range cs.Mounts {
		if what := hostAccess(m, lookup); what != "" {
			return what
		}
	}

	if len(cs.CapabilityAdd) > 0 {
		return "the added capability " + cs.CapabilityAdd[0]
	}

	return ""
}

// hostAccess names how a mount reaches the host, or returns "" if it does not.
// It reads the type as Docker does: case-insensitively, empty meaning a bind.
func hostAccess(m mount.Mount, lookup func(string) (volume.Volume, bool)) string {
	switch mount.Type(strings.ToLower(string(m.Type))) {
	case "", mount.TypeBind:
		return "a bind mount of " + m.Source
	case mount.TypeNamedPipe:
		return "a named pipe mount of " + m.Source
	case mount.TypeCluster:
		// Always a CSI driver's, which is what a non-local volume is refused for.
		return "a cluster volume mount of " + m.Source
	case mount.TypeVolume:
		if m.VolumeOptions != nil && m.VolumeOptions.DriverConfig != nil {
			if m.Source == "" {
				return "an anonymous volume mount with driver options"
			}

			return "a volume mount of " + m.Source + " with driver options"
		}

		if m.Source == "" {
			return ""
		}

		stored, ok := lookup(m.Source)
		if !ok {
			return "a mount of unknown volume " + m.Source
		}

		if stored.Driver != "local" || len(stored.Options) > 0 {
			return "a volume mount of " + m.Source + " with driver options"
		}

		return ""
	default:
		return ""
	}
}

// withoutEmptyBindOptions lets a request's empty options block match a stored
// bind, which Docker keeps without one unless propagation or recursion was set.
func withoutEmptyBindOptions(m mount.Mount) mount.Mount {
	if m.BindOptions != nil && *m.BindOptions == (mount.BindOptions{}) {
		m.BindOptions = nil
	}

	return m
}

// CheckCapabilities refuses an added Linux capability below the impactful tier,
// for the same reason as CheckMounts: several of them escape to the host.
func CheckCapabilities(svc swarm.Service, requested []string, level config.OperationsLevel) error {
	if level >= config.OpsImpactful {
		return nil
	}

	var current []string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		current = svc.Spec.TaskTemplate.ContainerSpec.CapabilityAdd
	}

	for _, capability := range requested {
		if !slices.Contains(current, capability) {
			return &HostAccessError{What: "adding the capability " + capability}
		}
	}

	return nil
}
