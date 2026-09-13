package compose

import (
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
)

// stackNamespaceLabel marks the stack a resource was deployed under; compose's
// deploy.labels/labels already say that, so it is redundant on the document.
const stackNamespaceLabel = "com.docker.stack.namespace"

// notAContainerService suffixes the warning both entry points emit for a
// plugin or network-attachment task.
const notAContainerService = ": not a container service, omitted"

// names applies the two rules every projected identifier goes through: a
// network attachment carries an ID rather than a name, and a name the stack
// owns carries a prefix docker stack deploy adds back.
type names struct {
	byID   map[string]string
	prefix string
}

// forStack builds the rules for a stack export. A single-service export passes
// an empty stack: it owns nothing, so no prefix is its to strip.
func forStack(stack string, networks []network.Summary) names {
	byID := make(map[string]string, len(networks))
	for _, net := range networks {
		byID[net.ID] = net.Name
	}

	n := names{byID: byID}
	if stack != "" {
		n.prefix = stack + "_"
	}

	return n
}

// short strips the stack's own prefix; docker stack deploy adds it back, so a
// name left long redeploys as web_web_api.
func (n names) short(s string) string { return strings.TrimPrefix(s, n.prefix) }

// network resolves an attachment target, which Swarm carries as an ID. An ID
// no known network covers comes back unchanged, so the reference stays visible
// rather than vanishing.
func (n names) network(target string) (string, bool) {
	name, ok := n.byID[target]
	if !ok {
		return target, false
	}

	return name, true
}

// declare writes value at key unless something already claimed it — the stack's
// own resources are declared first and outrank a reference to the same name.
func declare[T any](m map[string]T, key string, value T) {
	if _, ok := m[key]; !ok {
		m[key] = value
	}
}

// declareReferences declares everything a service references and nothing has
// declared yet. A stack routinely attaches to a network it does not own; that
// network carries no namespace label, so it never reaches StackDetail and the
// document would reference what it never declares.
func declareReferences(f *File, svc swarm.Service, n names) {
	c := svc.Spec.TaskTemplate.ContainerSpec
	if c == nil {
		return
	}

	for _, a := range svc.Spec.TaskTemplate.Networks {
		name, _ := n.network(a.Target)
		declare(f.Networks, n.short(name), Network{External: true, Name: name})
	}
	for _, m := range c.Mounts {
		if m.Type == mount.TypeVolume && m.Source != "" {
			declare(f.Volumes, n.short(m.Source), Volume{External: true, Name: m.Source})
		}
	}
	for _, sec := range c.Secrets {
		name := sec.SecretName
		declare(f.Secrets, n.short(name), ExternalRef{External: true, Name: name})
	}
	for _, cfg := range c.Configs {
		name := cfg.ConfigName
		declare(f.Configs, n.short(name), ExternalRef{External: true, Name: name})
	}
}

// FromService renders one service as a one-service document. Everything it
// references is external, because a service creates none of it. A service
// with no ContainerSpec (a plugin or network-attachment task) is not a
// compose service at all, so nothing about it is emitted.
func FromService(svc swarm.Service, networks []network.Summary) (File, []string) {
	c := svc.Spec.TaskTemplate.ContainerSpec
	if c == nil {
		return File{}, []string{svc.Spec.Name + notAContainerService}
	}

	n := forStack("", networks)
	spec, warnings := serviceSpec(svc, n)

	f := File{
		Services: map[string]Service{svc.Spec.Name: spec},
		Networks: make(map[string]Network, len(svc.Spec.TaskTemplate.Networks)),
		Volumes:  make(map[string]Volume, len(c.Mounts)),
		Secrets:  make(map[string]ExternalRef, len(c.Secrets)),
		Configs:  make(map[string]ExternalRef, len(c.Configs)),
	}
	declareReferences(&f, svc, n)

	return f, warnings
}

// owns reports whether a resource carries this stack's namespace label, the
// only thing distinguishing one the stack created from one it adopted.
func owns(labels map[string]string, stack string) bool {
	return labels[stackNamespaceLabel] == stack
}

// FromStack projects every member of a stack. A network or volume carrying
// the stack's namespace label is declared with its driver; everything else,
// including every config and secret, is external, because the deploy must
// not try to create what it does not own.
func FromStack(d cache.StackDetail, networks []network.Summary) (File, []string) {
	n := forStack(d.Name, networks)

	f := File{
		Services: make(map[string]Service, len(d.Services)),
		Networks: make(map[string]Network, len(d.Networks)),
		Volumes:  make(map[string]Volume, len(d.Volumes)),
		Configs:  make(map[string]ExternalRef, len(d.Configs)),
		Secrets:  make(map[string]ExternalRef, len(d.Secrets)),
	}
	var warnings []string

	for _, svc := range d.Services {
		if svc.Spec.TaskTemplate.ContainerSpec == nil {
			warnings = append(warnings, svc.Spec.Name+notAContainerService)
			continue
		}

		spec, w := serviceSpec(svc, n)
		f.Services[n.short(svc.Spec.Name)] = spec
		warnings = append(warnings, w...)
	}

	for _, net := range d.Networks {
		key := n.short(net.Name)
		if owns(net.Labels, d.Name) {
			f.Networks[key] = Network{
				Driver:     net.Driver,
				DriverOpts: net.Options,
				Attachable: net.Attachable,
				Internal:   net.Internal,
				EnableIPv6: net.EnableIPv6,
				Labels:     stripNamespace(net.Labels),
			}
			continue
		}

		f.Networks[key] = Network{External: true, Name: net.Name}
	}

	for _, v := range d.Volumes {
		key := n.short(v.Name)
		if owns(v.Labels, d.Name) {
			f.Volumes[key] = Volume{
				Driver:     v.Driver,
				DriverOpts: v.Options,
				Labels:     stripNamespace(v.Labels),
			}
			continue
		}

		f.Volumes[key] = Volume{External: true, Name: v.Name}
	}

	// Always external, both of them. A config's content is available and is
	// still not inlined: compose's content: field would have the redeploy
	// create a new config rather than reuse the one the service is mounting.
	for _, c := range d.Configs {
		f.Configs[n.short(c.Spec.Name)] = ExternalRef{External: true, Name: c.Spec.Name}
	}
	for _, s := range d.Secrets {
		f.Secrets[n.short(s.Spec.Name)] = ExternalRef{External: true, Name: s.Spec.Name}
	}

	for _, svc := range d.Services {
		declareReferences(&f, svc, n)
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

// serviceSpec projects one service; callers guarantee a non-nil ContainerSpec.
func serviceSpec(svc swarm.Service, n names) (Service, []string) {
	var warnings []string

	spec := svc.Spec
	container := spec.TaskTemplate.ContainerSpec

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

	out.Volumes = mounts(container.Mounts, n)

	credSpec, securityOpt, warn := privileges(container.Privileges)
	out.CredSpec, out.SecurityOpt = credSpec, securityOpt
	if warn != "" {
		warnings = append(warnings, spec.Name+": "+warn)
	}

	for _, a := range spec.TaskTemplate.Networks {
		name, ok := n.network(a.Target)
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"%s: network %s is not in the cluster's network list; referenced by ID",
				spec.Name, a.Target,
			))
		}

		out.Networks = append(out.Networks, n.short(name))
	}
	for _, s := range container.Secrets {
		out.Secrets = append(out.Secrets, fileRef(n.short(s.SecretName), s.File))
	}
	for _, c := range container.Configs {
		out.Configs = append(out.Configs, configRef(n.short(c.ConfigName), c.File))
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
			warnings = append(warnings, fmt.Sprintf(
				"%s: only the first of %d platforms kept; compose takes one",
				spec.Name, len(p.Platforms),
			))
		}
	}

	out.Deploy = deploy(svc)

	return out, warnings
}

// resourceLimit takes pids separately: the reservation shape has no such field.
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

func deploy(svc swarm.Service) *Deploy {
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
