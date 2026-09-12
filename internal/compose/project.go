package compose

import (
	"strconv"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// stackNamespaceLabel marks the stack a resource was deployed under; compose's
// deploy.labels/labels already say that, so it is redundant on the document.
const stackNamespaceLabel = "com.docker.stack.namespace"

// identity is the shortening a single-service export applies: none. Nothing is
// owned, so no prefix is the stack's to strip.
func identity(s string) string { return s }

// set writes value at key, creating the map on first use.
func set[T any](m *map[string]T, key string, value T) {
	if *m == nil {
		*m = map[string]T{}
	}
	(*m)[key] = value
}

func externalRef(m *map[string]ExternalRef, name string) {
	if name == "" {
		return
	}
	set(m, name, ExternalRef{External: true})
}

func externalNetwork(m *map[string]Network, name string) {
	if name == "" {
		return
	}
	set(m, name, Network{External: true})
}

func externalVolume(m *map[string]Volume, name string) {
	if name == "" {
		return
	}
	set(m, name, Volume{External: true})
}

// FromService renders one service as a one-service document. Everything it
// references is external, because a service creates none of it. A service
// with no ContainerSpec (a plugin or network-attachment task) is not a
// compose service at all, so nothing about it is emitted.
func FromService(svc swarm.Service) (File, []string) {
	spec, warnings := serviceSpec(svc, identity)

	c := svc.Spec.TaskTemplate.ContainerSpec
	if c == nil {
		return File{}, warnings
	}

	f := File{Services: map[string]Service{svc.Spec.Name: spec}}

	for _, n := range svc.Spec.TaskTemplate.Networks {
		externalNetwork(&f.Networks, n.Target)
	}
	for _, m := range c.Mounts {
		if m.Type == mount.TypeVolume {
			externalVolume(&f.Volumes, m.Source)
		}
	}
	for _, s := range c.Secrets {
		externalRef(&f.Secrets, s.SecretName)
	}
	for _, cfg := range c.Configs {
		externalRef(&f.Configs, cfg.ConfigName)
	}

	return f, warnings
}

// stripNamespace removes the stack namespace label without mutating the
// source map; it returns nil rather than an empty map when that was the only
// key, since an empty map renders as "labels: {}".
func stripNamespace(labels map[string]string) map[string]string {
	if _, ok := labels[stackNamespaceLabel]; !ok {
		return labels
	}

	out := make(map[string]string, len(labels)-1)
	for k, v := range labels {
		if k != stackNamespaceLabel {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}

	return out
}

// fileRef builds a FileRef from a secret reference's file target; a bare
// reference with no File still needs a Source to mount.
func fileRef(source string, target *swarm.SecretReferenceFileTarget) FileRef {
	if target == nil {
		return FileRef{Source: source}
	}

	return FileRef{
		Source: source,
		Target: target.Name,
		UID:    target.UID,
		GID:    target.GID,
		Mode:   uint32(target.Mode),
	}
}

// configRef mirrors fileRef for the config reference's near-identical type.
func configRef(source string, target *swarm.ConfigReferenceFileTarget) FileRef {
	if target == nil {
		return FileRef{Source: source}
	}

	return FileRef{
		Source: source,
		Target: target.Name,
		UID:    target.UID,
		GID:    target.GID,
		Mode:   uint32(target.Mode),
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// serviceSpec projects one service. shorten strips the stack prefix from names
// the stack owns; a single-service export passes identity.
func serviceSpec(svc swarm.Service, shorten func(string) string) (Service, []string) {
	var warnings []string

	spec := svc.Spec
	container := spec.TaskTemplate.ContainerSpec
	if container == nil {
		return Service{}, []string{spec.Name + ": not a container service, omitted"}
	}

	out := Service{
		Image:    container.Image,
		Hostname: container.Hostname,
		// Swarm's Command is the entrypoint and its Args are the command;
		// compose names them the other way round. The swap is deliberate.
		Command:     container.Args,
		Entrypoint:  container.Command,
		WorkingDir:  container.Dir,
		User:        container.User,
		GroupAdd:    container.Groups,
		Environment: environment(container.Env),
		Labels:      stripNamespace(container.Labels),
		ExtraHosts:  extraHosts(container.Hosts),
		Healthcheck: healthcheck(container.Healthcheck),
		StopSignal:  container.StopSignal,
		Init:        container.Init,
		TTY:         container.TTY,
		StdinOpen:   container.OpenStdin,
		ReadOnly:    container.ReadOnly,
		CapAdd:      container.CapabilityAdd,
		CapDrop:     container.CapabilityDrop,
		Sysctls:     container.Sysctls,
		Isolation:   string(container.Isolation),
		OomScoreAdj: container.OomScoreAdj,
	}

	if container.StopGracePeriod != nil {
		out.StopGrace = duration(*container.StopGracePeriod)
	}
	if d := container.DNSConfig; d != nil {
		out.DNS, out.DNSSearch = d.Nameservers, d.Search
	}

	out.Volumes = mounts(container.Mounts, shorten)

	credSpec, securityOpt, warn := privileges(container.Privileges)
	out.CredSpec, out.SecurityOpt = credSpec, securityOpt
	if warn != "" {
		warnings = append(warnings, spec.Name+": "+warn)
	}

	for _, n := range spec.TaskTemplate.Networks {
		out.Networks = append(out.Networks, shorten(n.Target))
	}
	for _, s := range container.Secrets {
		out.Secrets = append(out.Secrets, fileRef(shorten(s.SecretName), s.File))
	}
	for _, c := range container.Configs {
		out.Configs = append(out.Configs, configRef(shorten(c.ConfigName), c.File))
	}

	if e := spec.EndpointSpec; e != nil {
		out.Ports = ports(e.Ports)
	}
	if l := spec.TaskTemplate.LogDriver; l != nil {
		out.Logging = &Logging{Driver: l.Name, Options: l.Options}
	}

	if p := spec.TaskTemplate.Placement; p != nil && len(p.Platforms) > 0 {
		out.Platform = p.Platforms[0].OS + "/" + p.Platforms[0].Architecture
		if len(p.Platforms) > 1 {
			warnings = append(
				warnings,
				spec.Name+": only the first of "+itoa(
					len(p.Platforms),
				)+" platforms kept; compose takes one",
			)
		}
	}

	out.Deploy = deploy(svc, shorten)

	return out, warnings
}

// resourceLimit renders a limit's CPU and memory figures through the
// existing helpers; Pids is omitted for the reservation shape, which has none.
func resourceLimit(nanoCPUs, memoryBytes, pids int64) *ResourceLimit {
	if nanoCPUs == 0 && memoryBytes == 0 && pids == 0 {
		return nil
	}

	return &ResourceLimit{CPUs: cpus(nanoCPUs), Memory: memory(memoryBytes), PIDs: pids}
}

func resources(r *swarm.ResourceRequirements) *Resources {
	out := &Resources{}
	if l := r.Limits; l != nil {
		out.Limits = resourceLimit(l.NanoCPUs, l.MemoryBytes, l.Pids)
	}
	if v := r.Reservations; v != nil {
		out.Reservations = resourceLimit(v.NanoCPUs, v.MemoryBytes, 0)
	}
	if out.Limits == nil && out.Reservations == nil {
		return nil
	}

	return out
}

func placement(p *swarm.Placement) *Placement {
	out := &Placement{Constraints: p.Constraints, MaxReplicas: p.MaxReplicas}
	for _, pref := range p.Preferences {
		if pref.Spread != nil {
			out.Preferences = append(
				out.Preferences,
				map[string]string{"spread": pref.Spread.SpreadDescriptor},
			)
		}
	}

	return out
}

func updateConfig(u *swarm.UpdateConfig) *UpdateConfig {
	return &UpdateConfig{
		Parallelism:     &u.Parallelism,
		Delay:           duration(u.Delay),
		FailureAction:   u.FailureAction,
		Monitor:         duration(u.Monitor),
		MaxFailureRatio: u.MaxFailureRatio,
		Order:           u.Order,
	}
}

func restartPolicy(rp *swarm.RestartPolicy) *RestartPolicy {
	out := &RestartPolicy{Condition: string(rp.Condition), MaxAttempts: rp.MaxAttempts}
	if rp.Delay != nil {
		out.Delay = duration(*rp.Delay)
	}
	if rp.Window != nil {
		out.Window = duration(*rp.Window)
	}

	return out
}

// deploy carries what Swarm owns and a plain container runtime does not; a
// single-service export passes identity for shorten just as serviceSpec does.
func deploy(svc swarm.Service, shorten func(string) string) *Deploy {
	spec := svc.Spec

	d := &Deploy{Labels: stripNamespace(spec.Labels)}

	switch {
	case spec.Mode.Replicated != nil:
		d.Mode, d.Replicas = "replicated", spec.Mode.Replicated.Replicas
	case spec.Mode.Global != nil:
		d.Mode = "global"
	case spec.Mode.ReplicatedJob != nil:
		d.Mode = "replicated-job"
	case spec.Mode.GlobalJob != nil:
		d.Mode = "global-job"
	}

	if e := spec.EndpointSpec; e != nil {
		d.EndpointMode = string(e.Mode)
	}

	if r := spec.TaskTemplate.Resources; r != nil {
		d.Resources = resources(r)
	}
	if p := spec.TaskTemplate.Placement; p != nil {
		d.Placement = placement(p)
	}
	if u := spec.UpdateConfig; u != nil {
		d.UpdateConfig = updateConfig(u)
	}
	if rb := spec.RollbackConfig; rb != nil {
		d.RollbackConfig = updateConfig(rb)
	}
	if rp := spec.TaskTemplate.RestartPolicy; rp != nil {
		d.RestartPolicy = restartPolicy(rp)
	}

	return d
}
