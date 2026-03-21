package api

import (
	"fmt"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

// SpecChange represents a single difference between two ServiceSpecs.
type SpecChange struct {
	Field string `json:"field"`
	Old   string `json:"old,omitempty"`
	New   string `json:"new,omitempty"`
}

// DiffServiceSpecs compares prev and curr and returns a list of
// human-readable changes. Returns nil if the specs are identical
// (or if prev is nil).
func DiffServiceSpecs(prev, curr *swarm.ServiceSpec) []SpecChange {
	if prev == nil {
		return nil
	}

	var changes []SpecChange
	add := func(field, old, new string) {
		if old != new {
			changes = append(changes, SpecChange{Field: field, Old: old, New: new})
		}
	}

	// Image
	var prevImage, currImage string
	if prev.TaskTemplate.ContainerSpec != nil {
		prevImage = prev.TaskTemplate.ContainerSpec.Image
	}
	if curr.TaskTemplate.ContainerSpec != nil {
		currImage = curr.TaskTemplate.ContainerSpec.Image
	}
	add("Image", stripDigest(prevImage), stripDigest(currImage))

	// Replicas
	add("Replicas", formatReplicas(prev.Mode), formatReplicas(curr.Mode))

	// Container-level fields
	prevContainer := prev.TaskTemplate.ContainerSpec
	currContainer := curr.TaskTemplate.ContainerSpec
	if prevContainer != nil && currContainer != nil {
		add(
			"Command",
			strings.Join(prevContainer.Command, " "),
			strings.Join(currContainer.Command, " "),
		)
		add("Args", strings.Join(prevContainer.Args, " "), strings.Join(currContainer.Args, " "))
		add("User", prevContainer.User, currContainer.User)
		add("Dir", prevContainer.Dir, currContainer.Dir)
		add("Hostname", prevContainer.Hostname, currContainer.Hostname)

		changes = append(
			changes,
			diffLabels("Env", envToMap(prevContainer.Env), envToMap(currContainer.Env))...)
		changes = append(
			changes,
			diffLabels("Label", prevContainer.Labels, currContainer.Labels)...)
		changes = append(changes, diffMounts(prevContainer.Mounts, currContainer.Mounts)...)

		changes = append(changes, diffSets("Config",
			configNames(prevContainer.Configs), configNames(currContainer.Configs))...)
		changes = append(changes, diffSets("Secret",
			secretNames(prevContainer.Secrets), secretNames(currContainer.Secrets))...)
	}

	// Service-level labels
	changes = append(changes, diffLabels("Service label", prev.Labels, curr.Labels)...)

	// Networks
	changes = append(changes, diffSets("Network",
		networkTargets(prev.TaskTemplate.Networks), networkTargets(curr.TaskTemplate.Networks))...)

	// Ports
	changes = append(changes, diffSets("Port",
		portKeys(prev.EndpointSpec), portKeys(curr.EndpointSpec))...)

	// Resources
	changes = append(
		changes,
		diffResources(prev.TaskTemplate.Resources, curr.TaskTemplate.Resources)...)

	// Placement constraints
	var prevConstraints, currConstraints []string
	if prev.TaskTemplate.Placement != nil {
		prevConstraints = prev.TaskTemplate.Placement.Constraints
	}
	if curr.TaskTemplate.Placement != nil {
		currConstraints = curr.TaskTemplate.Placement.Constraints
	}
	changes = append(changes, diffSets("Placement constraint",
		toSet(prevConstraints), toSet(currConstraints))...)

	// Stable ordering for deterministic ETags.
	slices.SortFunc(changes, func(a, b SpecChange) int {
		return strings.Compare(a.Field+a.Old+a.New, b.Field+b.Old+b.New)
	})
	return changes
}

// stripDigest removes the @sha256:... digest suffix from an image reference,
// keeping only the tag-based name which is more readable.
func stripDigest(image string) string {
	if i := strings.Index(image, "@sha256:"); i > 0 {
		return image[:i]
	}
	return image
}

func formatReplicas(mode swarm.ServiceMode) string {
	if mode.Global != nil {
		return "global"
	}
	if mode.Replicated != nil && mode.Replicated.Replicas != nil {
		return fmt.Sprintf("%d", *mode.Replicated.Replicas)
	}
	return ""
}

// diffSets emits "added" and "removed" changes for two string sets.
func diffSets(prefix string, prev, curr map[string]bool) []SpecChange {
	var changes []SpecChange
	for s := range prev {
		if !curr[s] {
			changes = append(changes, SpecChange{Field: prefix + " removed", Old: s})
		}
	}
	for s := range curr {
		if !prev[s] {
			changes = append(changes, SpecChange{Field: prefix + " added", New: s})
		}
	}
	return changes
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func configNames(refs []*swarm.ConfigReference) map[string]bool {
	m := make(map[string]bool, len(refs))
	for _, c := range refs {
		m[c.ConfigName] = true
	}
	return m
}

func secretNames(refs []*swarm.SecretReference) map[string]bool {
	m := make(map[string]bool, len(refs))
	for _, s := range refs {
		m[s.SecretName] = true
	}
	return m
}

func networkTargets(nets []swarm.NetworkAttachmentConfig) map[string]bool {
	m := make(map[string]bool, len(nets))
	for _, n := range nets {
		m[n.Target] = true
	}
	return m
}

func portKeys(spec *swarm.EndpointSpec) map[string]bool {
	if spec == nil {
		return nil
	}
	m := make(map[string]bool, len(spec.Ports))
	for _, p := range spec.Ports {
		m[fmt.Sprintf("%d→%d/%s", p.PublishedPort, p.TargetPort, p.Protocol)] = true
	}
	return m
}

func envToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func diffLabels(prefix string, prev, curr map[string]string) []SpecChange {
	var changes []SpecChange
	for k, v := range prev {
		if cv, ok := curr[k]; !ok {
			changes = append(changes, SpecChange{Field: prefix + " removed", Old: k + "=" + v})
		} else if cv != v {
			changes = append(changes, SpecChange{Field: prefix + " " + k, Old: v, New: cv})
		}
	}
	for k, v := range curr {
		if _, ok := prev[k]; !ok {
			changes = append(changes, SpecChange{Field: prefix + " added", New: k + "=" + v})
		}
	}
	return changes
}

func diffMounts(prev, curr []mount.Mount) []SpecChange {
	prevByTarget := make(map[string]mount.Mount, len(prev))
	for _, m := range prev {
		prevByTarget[m.Target] = m
	}
	currByTarget := make(map[string]mount.Mount, len(curr))
	for _, m := range curr {
		currByTarget[m.Target] = m
	}

	var changes []SpecChange
	for target, pm := range prevByTarget {
		if cm, ok := currByTarget[target]; !ok {
			changes = append(changes, SpecChange{Field: "Mount removed", Old: target})
		} else if pm.Source != cm.Source || string(pm.Type) != string(cm.Type) {
			changes = append(changes, SpecChange{
				Field: "Mount " + target,
				Old:   fmt.Sprintf("%s:%s", pm.Type, pm.Source),
				New:   fmt.Sprintf("%s:%s", cm.Type, cm.Source),
			})
		}
	}
	for target := range currByTarget {
		if _, ok := prevByTarget[target]; !ok {
			cm := currByTarget[target]
			changes = append(
				changes,
				SpecChange{
					Field: "Mount added",
					New:   fmt.Sprintf("%s → %s (%s)", cm.Source, target, cm.Type),
				},
			)
		}
	}
	return changes
}

func diffResources(prev, curr *swarm.ResourceRequirements) []SpecChange {
	type pair struct {
		field      string
		prev, curr int64
		format     func(int64) string
	}

	limPrev, limCurr := resourceLimits(prev), resourceLimits(curr)
	resPrev, resCurr := resourceReservations(prev), resourceReservations(curr)

	pairs := []pair{
		{"CPU limit", limPrev.NanoCPUs, limCurr.NanoCPUs, formatCPU},
		{"Memory limit", limPrev.MemoryBytes, limCurr.MemoryBytes, formatMem},
		{"CPU reservation", resPrev.NanoCPUs, resCurr.NanoCPUs, formatCPU},
		{"Memory reservation", resPrev.MemoryBytes, resCurr.MemoryBytes, formatMem},
	}

	var changes []SpecChange
	for _, p := range pairs {
		if p.prev != p.curr {
			changes = append(
				changes,
				SpecChange{Field: p.field, Old: p.format(p.prev), New: p.format(p.curr)},
			)
		}
	}
	return changes
}

func resourceLimits(r *swarm.ResourceRequirements) swarm.Limit {
	if r != nil && r.Limits != nil {
		return *r.Limits
	}
	return swarm.Limit{}
}

func resourceReservations(r *swarm.ResourceRequirements) swarm.Resources {
	if r != nil && r.Reservations != nil {
		return *r.Reservations
	}
	return swarm.Resources{}
}

func formatCPU(nano int64) string {
	if nano == 0 {
		return "(none)"
	}
	return fmt.Sprintf("%.2f cores", float64(nano)/1e9)
}

func formatMem(bytes int64) string {
	if bytes == 0 {
		return "(none)"
	}
	if bytes >= 1<<30 {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
	}
	return fmt.Sprintf("%.0f MiB", float64(bytes)/(1<<20))
}
