package cluster

import (
	"reflect"
	"slices"
	"strings"

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

// CheckMounts refuses a mount that reaches the host below the impactful tier:
// a bind or named pipe hands the container a host path, the Docker socket
// included, and a volume driver's options can bind one too. One the service
// already carries, unchanged, is exempt so tier 2 can edit the rest.
func CheckMounts(svc swarm.Service, requested []mount.Mount, level config.OperationsLevel) error {
	if level >= config.OpsImpactful {
		return nil
	}

	var current []mount.Mount
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		current = svc.Spec.TaskTemplate.ContainerSpec.Mounts
	}

	for _, m := range requested {
		what := hostAccess(m)
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

// hostAccess names how a mount reaches the host, or returns "" if it does not.
// It reads the type as Docker does: case-insensitively, empty meaning a bind.
func hostAccess(m mount.Mount) string {
	switch mount.Type(strings.ToLower(string(m.Type))) {
	case "", mount.TypeBind:
		return "a bind mount of " + m.Source
	case mount.TypeNamedPipe:
		return "a named pipe mount of " + m.Source
	case mount.TypeVolume:
		if m.VolumeOptions == nil || m.VolumeOptions.DriverConfig == nil {
			return ""
		}

		if m.Source == "" {
			return "an anonymous volume mount with driver options"
		}

		return "a volume mount of " + m.Source + " with driver options"
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
