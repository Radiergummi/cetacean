package cluster

import (
	"reflect"
	"slices"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

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

// CheckMounts refuses a bind mount below the impactful tier: it hands the
// container the host's filesystem, the Docker socket included. A bind the
// service already carries, unchanged, is exempt so tier 2 can edit the rest.
func CheckMounts(svc swarm.Service, requested []mount.Mount, level config.OperationsLevel) error {
	if level >= config.OpsImpactful {
		return nil
	}

	var current []mount.Mount
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		current = svc.Spec.TaskTemplate.ContainerSpec.Mounts
	}

	for _, m := range requested {
		if m.Type != mount.TypeBind {
			continue
		}

		if !slices.ContainsFunc(
			current,
			func(c mount.Mount) bool { return reflect.DeepEqual(c, m) },
		) {
			return &HostAccessError{What: "a bind mount of " + m.Source}
		}
	}

	return nil
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
