package cluster

import (
	"errors"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// SpecChange edits a service spec in place. It is handed the spec Docker holds,
// so a change that merges into a section merges into the live one.
type SpecChange = func(spec *swarm.ServiceSpec) error

// MapChange computes a string map from its live value.
type MapChange = func(current map[string]string) (map[string]string, error)

// ErrNoContainerSpec refuses an edit to a container a service does not have:
// the sections edit an existing container, they do not add one.
var ErrNoContainerSpec = errors.New("service has no container spec")

// PatchEnv applies change to the environment, as a map of name to value.
func PatchEnv(change MapChange) SpecChange {
	return EditContainer(func(cs *swarm.ContainerSpec) error {
		updated, err := change(envMap(cs.Env, false))
		if err != nil {
			return err
		}

		env := make([]string, 0, len(updated))
		for name, value := range updated {
			env = append(env, name+"="+value)
		}
		sort.Strings(env)
		cs.Env = env

		return nil
	})
}

// PatchLabels applies change to the service's labels.
func PatchLabels(change MapChange) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		updated, err := change(spec.Labels)
		if err != nil {
			return err
		}
		spec.Labels = updated

		return nil
	}
}

// SetResources replaces the resource limits and reservations.
func SetResources(resources *swarm.ResourceRequirements) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Resources = resources
		return nil
	}
}

// SetPlacement replaces the placement constraints and preferences.
func SetPlacement(placement *swarm.Placement) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Placement = placement
		return nil
	}
}

// SetPorts replaces the published ports.
func SetPorts(ports []swarm.PortConfig) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}
		spec.EndpointSpec.Ports = ports

		return nil
	}
}

// SetEndpointMode replaces how the service's tasks are resolved.
func SetEndpointMode(mode swarm.ResolutionMode) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		if spec.EndpointSpec == nil {
			spec.EndpointSpec = &swarm.EndpointSpec{}
		}
		spec.EndpointSpec.Mode = mode

		return nil
	}
}

// SetUpdatePolicy replaces how an update rolls out.
func SetUpdatePolicy(policy *swarm.UpdateConfig) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		spec.UpdateConfig = policy
		return nil
	}
}

// SetRollbackPolicy replaces how a rollback rolls out.
func SetRollbackPolicy(policy *swarm.UpdateConfig) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		spec.RollbackConfig = policy
		return nil
	}
}

// SetLogDriver replaces the log driver and its options.
func SetLogDriver(driver *swarm.Driver) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.LogDriver = driver
		return nil
	}
}

// SetHealthcheck replaces the container's healthcheck.
func SetHealthcheck(hc *container.HealthConfig) SpecChange {
	return EditContainer(func(cs *swarm.ContainerSpec) error {
		cs.Healthcheck = hc
		return nil
	})
}

// SetNetworks replaces the networks the service's tasks join.
func SetNetworks(networks []swarm.NetworkAttachmentConfig) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		spec.TaskTemplate.Networks = networks
		return nil
	}
}

// The reference lists may be set on a service before it has any other
// container settings, so these create the container spec rather than refuse.

// SetSecrets replaces the secrets the container receives.
func SetSecrets(secrets []*swarm.SecretReference) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		ensureContainerSpec(spec).Secrets = secrets
		return nil
	}
}

// SetConfigs replaces the configs the container receives.
func SetConfigs(configs []*swarm.ConfigReference) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		ensureContainerSpec(spec).Configs = configs
		return nil
	}
}

// SetMounts replaces the container's mounts.
func SetMounts(mounts []mount.Mount) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		ensureContainerSpec(spec).Mounts = mounts
		return nil
	}
}

// EditContainer applies edit to the service's container spec.
func EditContainer(edit func(cs *swarm.ContainerSpec) error) SpecChange {
	return func(spec *swarm.ServiceSpec) error {
		if spec.TaskTemplate.ContainerSpec == nil {
			return invalid("service", ErrNoContainerSpec)
		}

		return edit(spec.TaskTemplate.ContainerSpec)
	}
}

func ensureContainerSpec(spec *swarm.ServiceSpec) *swarm.ContainerSpec {
	if spec.TaskTemplate.ContainerSpec == nil {
		spec.TaskTemplate.ContainerSpec = &swarm.ContainerSpec{}
	}

	return spec.TaskTemplate.ContainerSpec
}

// envMap reads KEY=VALUE entries. A bare KEY is dropped when editing, as the
// writer always has, and kept as empty when comparing, so dropping it counts.
func envMap(env []string, bare bool) map[string]string {
	out := make(map[string]string, len(env))
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok || bare {
			out[name] = value
		}
	}

	return out
}
