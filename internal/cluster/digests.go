package cluster

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// NodeDigest builds the detail view of one cluster node.
//
// A node being drained still reports Status.State "ready" — Swarm keeps
// reporting the daemon's own health while gracefully evacuating it — but the
// scheduler will not place anything there, so Availability is the field that
// actually answers "can this take work" and overrides State whenever it says
// the node is not active. Without this, a caller polling for "is it safe to
// drain this node" would see "ready" and conclude the drain had not started.
func NodeDigest(node swarm.Node, tasks []swarm.Task, services []swarm.Service) Digest {
	state := string(node.Status.State)
	if node.Spec.Availability != swarm.NodeAvailabilityActive {
		state = string(node.Spec.Availability)
	}

	var reason string
	if state != string(swarm.NodeStateReady) {
		reason = node.Status.Message
	}

	serviceNames := make(map[string]string, len(services))
	for _, svc := range services {
		serviceNames[svc.ID] = svc.Spec.Name
	}

	seen := make(map[string]bool)
	related := make([]Related, 0)

	for _, task := range tasks {
		if task.NodeID != node.ID || !taskIsLive(task) {
			continue
		}

		if seen[task.ServiceID] {
			continue
		}
		seen[task.ServiceID] = true

		name := serviceNames[task.ServiceID]
		if name == "" {
			// The services slice may have been ACL-filtered; the ID is always
			// known from the task itself, so only the name needs a fallback.
			name = task.ServiceID
		}

		related = append(related, Related{
			ID:       task.ServiceID,
			Name:     name,
			Type:     "service",
			Relation: "runs",
		})
	}

	sortRelated(related)

	details := map[string]any{
		"role":          string(node.Spec.Role),
		"availability":  string(node.Spec.Availability),
		"address":       node.Status.Addr,
		"engineVersion": node.Description.Engine.EngineVersion,
		"platform":      node.Description.Platform.OS + "/" + node.Description.Platform.Architecture,
		"cpuCores":      float64(node.Description.Resources.NanoCPUs) / 1e9,
		"memoryBytes":   node.Description.Resources.MemoryBytes,
	}

	if node.ManagerStatus != nil {
		details["reachability"] = string(node.ManagerStatus.Reachability)
		details["leader"] = node.ManagerStatus.Leader
	}

	if len(node.Spec.Labels) > 0 {
		details["labels"] = node.Spec.Labels
	}

	return Digest{
		ID:             node.ID,
		Name:           node.Description.Hostname,
		Type:           "node",
		State:          state,
		Reason:         reason,
		Since:          node.UpdatedAt.UTC().Format(time.RFC3339),
		Details:        details,
		Related:        related,
		RecentFailures: make([]TaskFailure, 0),
	}
}

// TaskDigest builds the detail view of one task. service and node are
// pointers because a caller may be able to read the task but not the
// resource it references — a filtered-out service or an unreachable node —
// and the digest must still be usable rather than erroring or leaving the
// name blank.
func TaskDigest(task swarm.Task, service *swarm.Service, node *swarm.Node) Digest {
	name := task.ID
	if service != nil {
		// Docker's own naming convention: a replicated task is
		// "<service>.<slot>", a global one "<service>.<node>", since a global
		// service has no slot to distinguish its replicas by.
		if service.Spec.Mode.Global != nil {
			name = fmt.Sprintf("%s.%s", service.Spec.Name, task.NodeID)
		} else {
			name = fmt.Sprintf("%s.%d", service.Spec.Name, task.Slot)
		}
	}

	var reason string
	if task.Status.State != swarm.TaskStateRunning {
		reason = taskReason(task)
	}

	details := map[string]any{
		"desiredState": string(task.DesiredState),
		"message":      task.Status.Message,
	}

	if task.Slot != 0 {
		details["slot"] = task.Slot
	}

	if spec := task.Spec.ContainerSpec; spec != nil {
		details["image"] = StripImageDigest(spec.Image)
	}

	if cs := task.Status.ContainerStatus; cs != nil {
		details["containerID"] = cs.ContainerID

		// ExitCode is a placeholder like -1 until the task actually stops;
		// reporting it earlier would read as a real exit that never happened.
		if cache.IsTerminalState(task.Status.State) {
			details["exitCode"] = cs.ExitCode
		}
	}

	related := make([]Related, 0, 2)

	switch {
	case service != nil:
		related = append(related, Related{
			ID: service.ID, Name: service.Spec.Name, Type: "service", Relation: "instance-of",
		})
	case task.ServiceID != "":
		related = append(related, Related{
			ID: task.ServiceID, Name: task.ServiceID, Type: "service", Relation: "instance-of",
		})
	}

	switch {
	case node != nil:
		related = append(related, Related{
			ID: node.ID, Name: node.Description.Hostname, Type: "node", Relation: "runs-on",
		})
	case task.NodeID != "":
		related = append(related, Related{
			ID: task.NodeID, Name: task.NodeID, Type: "node", Relation: "runs-on",
		})
	}

	sortRelated(related)

	return Digest{
		ID:             task.ID,
		Name:           name,
		Type:           "task",
		State:          string(task.Status.State),
		Reason:         reason,
		Since:          task.Status.Timestamp.UTC().Format(time.RFC3339),
		Details:        details,
		Related:        related,
		RecentFailures: make([]TaskFailure, 0),
	}
}

// StackDigest builds the detail view of one stack. A stack has no Swarm
// status of its own, so its State and Reason are derived from its member
// services using the exact rule RowsForServices already applies to each one
// — otherwise a stack digest and its own services list could report
// different health for the same cluster. stack.Services already holds the
// full swarm.Service records (cache.StackDetail, not the bare-name
// cache.Stack), so there is nothing to resolve a second time.
func StackDigest(stack cache.StackDetail, tasks []swarm.Task) Digest {
	running := make(map[string]int, len(stack.Services))
	for _, task := range tasks {
		if task.Status.State == swarm.TaskStateRunning {
			running[task.ServiceID]++
		}
	}

	type member struct {
		svc   swarm.Service
		state string
	}

	members := make([]member, 0, len(stack.Services))

	var desiredTotal, runningTotal int

	for _, svc := range stack.Services {
		members = append(members, member{svc: svc, state: DeriveServiceState(svc, running[svc.ID])})
		desiredTotal += ReplicaCount(svc)
		runningTotal += running[svc.ID]
	}

	slices.SortFunc(members, func(a, b member) int {
		return strings.Compare(a.svc.Spec.Name, b.svc.Spec.Name)
	})

	state := "running"

worstState:
	for _, candidate := range []string{"failed", "updating", "pending"} {
		for _, m := range members {
			if m.state == candidate {
				state = candidate

				break worstState
			}
		}
	}

	var reason string
	if state != "running" {
		for _, m := range members {
			if m.state == state {
				reason = fmt.Sprintf("service %s is %s", m.svc.Spec.Name, state)

				break
			}
		}
	}

	related := make([]Related, 0, len(stack.Services))
	for _, m := range members {
		related = append(related, Related{
			ID: m.svc.ID, Name: m.svc.Spec.Name, Type: "service", Relation: "contains",
		})
	}

	sortRelated(related)

	return Digest{
		ID:     stack.Name,
		Name:   stack.Name,
		Type:   "stack",
		State:  state,
		Reason: reason,
		Details: map[string]any{
			"services":        len(stack.Services),
			"desiredReplicas": desiredTotal,
			"runningReplicas": runningTotal,
		},
		Related:        related,
		RecentFailures: make([]TaskFailure, 0),
	}
}

// ConfigDigest builds the detail view of one config. Config data is base64
// and already safe to size: Spec.Data's length is what a caller needs to
// judge whether it holds a full file or a single flag, without ever seeing
// the payload itself.
func ConfigDigest(cfg swarm.Config, users []cache.ServiceRef) Digest {
	details := stackScopedDetails(cfg.Spec.Labels)
	details["createdAt"] = cfg.CreatedAt.UTC().Format(time.RFC3339)
	details["updatedAt"] = cfg.UpdatedAt.UTC().Format(time.RFC3339)
	details["sizeBytes"] = len(cfg.Spec.Data)

	return Digest{
		ID:             cfg.ID,
		Name:           cfg.Spec.Name,
		Type:           "config",
		Since:          cfg.UpdatedAt.UTC().Format(time.RFC3339),
		Details:        details,
		Related:        usersRelated(users, "used-by"),
		RecentFailures: make([]TaskFailure, 0),
	}
}

// SecretDigest builds the detail view of one secret. Unlike a config, a
// secret's Data must never be read here — not even for its length, which
// would still be information about a value this endpoint exists to keep out
// of reach.
func SecretDigest(sec swarm.Secret, users []cache.ServiceRef) Digest {
	details := stackScopedDetails(sec.Spec.Labels)
	details["createdAt"] = sec.CreatedAt.UTC().Format(time.RFC3339)
	details["updatedAt"] = sec.UpdatedAt.UTC().Format(time.RFC3339)

	return Digest{
		ID:             sec.ID,
		Name:           sec.Spec.Name,
		Type:           "secret",
		Since:          sec.UpdatedAt.UTC().Format(time.RFC3339),
		Details:        details,
		Related:        usersRelated(users, "used-by"),
		RecentFailures: make([]TaskFailure, 0),
	}
}

// NetworkDigest builds the detail view of one network.
func NetworkDigest(net network.Summary, users []cache.ServiceRef) Digest {
	details := stackScopedDetails(net.Labels)
	details["driver"] = net.Driver
	details["scope"] = net.Scope
	details["internal"] = net.Internal
	details["attachable"] = net.Attachable
	details["ingress"] = net.Ingress
	details["ipv6"] = net.EnableIPv6

	if len(net.IPAM.Config) > 0 {
		subnets := make([]string, 0, len(net.IPAM.Config))

		for _, ipamConfig := range net.IPAM.Config {
			if ipamConfig.Subnet != "" {
				subnets = append(subnets, ipamConfig.Subnet)
			}
		}

		if len(subnets) > 0 {
			details["subnets"] = subnets
		}
	}

	return Digest{
		ID:             net.ID,
		Name:           net.Name,
		Type:           "network",
		Since:          net.Created.UTC().Format(time.RFC3339),
		Details:        details,
		Related:        usersRelated(users, "attached-by"),
		RecentFailures: make([]TaskFailure, 0),
	}
}

// VolumeDigest builds the detail view of one volume. It takes a value rather
// than the *volume.Volume RowsForVolumes takes: a digest describes one
// resource the caller has already resolved, so there is no nil to guard.
func VolumeDigest(vol volume.Volume, users []cache.ServiceRef) Digest {
	details := stackScopedDetails(vol.Labels)
	details["driver"] = vol.Driver
	details["mountpoint"] = vol.Mountpoint
	details["scope"] = vol.Scope

	if len(vol.Options) > 0 {
		details["options"] = vol.Options
	}

	digest := Digest{
		ID:             vol.Name,
		Name:           vol.Name,
		Type:           "volume",
		Details:        details,
		Related:        usersRelated(users, "mounted-by"),
		RecentFailures: make([]TaskFailure, 0),
	}

	// Docker's volume CreatedAt is a free-form string, not guaranteed to be
	// RFC3339 — only pass it through once it has proven to parse as one.
	if since, err := time.Parse(time.RFC3339, vol.CreatedAt); err == nil {
		digest.Since = since.UTC().Format(time.RFC3339)
	}

	return digest
}

// stackScopedDetails starts a Details map with the stack namespace and raw
// labels every stack-scoped resource type shares, both omitted when absent so
// an unlabelled resource does not carry empty keys.
func stackScopedDetails(labels map[string]string) map[string]any {
	details := make(map[string]any)

	if stack := labels["com.docker.stack.namespace"]; stack != "" {
		details["stack"] = stack
	}

	if len(labels) > 0 {
		details["labels"] = labels
	}

	return details
}

// usersRelated builds the "which services reference this resource" half of a
// Digest. Configs, secrets, networks and volumes all resolve this the same
// way, from the cache's own reverse index (cache.ServiceRef), which already
// carries a real ID and name — there is nothing here to fall back to an ID
// for, unlike a Related built from a raw attachment.
func usersRelated(users []cache.ServiceRef, relation string) []Related {
	related := make([]Related, 0, len(users))

	for _, ref := range users {
		related = append(related, Related{
			ID:       ref.ID,
			Name:     ref.Name,
			Type:     "service",
			Relation: relation,
		})
	}

	sortRelated(related)

	return related
}
