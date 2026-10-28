package cluster

import (
	"slices"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Row is one entry in a list of cluster resources. The raw Docker object is
// the wrong answer to a list question: it is mostly Platforms and PreviousSpec,
// and whether the thing is healthy is not in it at all, since state is derived
// from tasks. ID and Name are both always present.
type Row struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Type is the resource type, singular: "service", "node", "task", ...
	Type string `json:"type"`

	// Stack is the owning stack namespace, where the resource belongs to one.
	Stack string `json:"stack,omitempty"`

	// State is the derived condition — a service's DeriveServiceState, a node's
	// status — not a raw Docker enum.
	State string `json:"state,omitempty"`

	// Detail is the single most identifying secondary fact: the image for a
	// service, the role for a node, the driver for a network.
	Detail string `json:"detail,omitempty"`

	// Desired and Running are populated only for types where a replica count
	// means something, so a caller can tell "2 of 3" from "no such concept".
	Desired int `json:"desired,omitempty"`
	Running int `json:"running,omitempty"`

	// The version fields place a config or secret among its other versions;
	// UsedBy counts its services, and is nil for every other type.
	VersionOf       string `json:"versionOf,omitempty"`
	VersionInferred bool   `json:"versionInferred,omitempty"`
	NewestVersion   bool   `json:"newestVersion,omitempty"`
	InUse           bool   `json:"inUse,omitempty"`
	OlderVersions   int    `json:"olderVersions,omitempty"`
	UsedBy          *int   `json:"usedBy,omitempty"`
}

// RowsForServices builds the list view of services. State and running count
// are derived from tasks, not from the spec, so the counts come in — already
// aggregated by cache.RunningTaskCounts under one read lock, rather than
// cloning the whole task table here to reduce it to one integer per service.
func RowsForServices(
	services []swarm.Service,
	running, completed, eligible map[string]int,
	flapping map[string]bool,
) []Row {
	rows := make([]Row, 0, len(services))

	for _, svc := range services {
		var image string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			image = StripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}

		rows = append(rows, Row{
			ID:    svc.ID,
			Name:  svc.Spec.Name,
			Type:  "service",
			Stack: svc.Spec.Labels[StackNamespaceLabel],
			State: DeriveServiceState(
				svc,
				running[svc.ID],
				completed[svc.ID],
				EligibleIn(eligible, svc.ID),
				flapping[svc.ID],
			),
			Detail:  image,
			Desired: ReplicaCount(svc),
			Running: running[svc.ID],
		})
	}

	SortRows(rows)

	return rows
}

// SortRows puts a list into a stable order. Callers build rows by ranging over
// cache slices whose order is not guaranteed, and the result is marshalled into
// an MCP result a client may cache by ETag.
func SortRows(rows []Row) {
	slices.SortFunc(rows, func(a, b Row) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})
}

// RowsForNodes builds the list view of cluster nodes. Detail is the node's
// role, which is what distinguishes two otherwise identical ready nodes.
func RowsForNodes(nodes []swarm.Node) []Row {
	rows := make([]Row, 0, len(nodes))

	for _, n := range nodes {
		rows = append(rows, Row{
			ID:     n.ID,
			Name:   n.Description.Hostname,
			Type:   "node",
			State:  DeriveNodeState(n),
			Detail: string(n.Spec.Role),
		})
	}

	SortRows(rows)

	return rows
}

// RowsForTasks builds the list view of tasks. Services name each task's
// parent, which its own record holds only as an ID; the node half comes off
// the enriched task, which resolved it already. The services slice and the
// enrichment must both be ACL-filtered, or a row names what describe withholds.
func RowsForTasks(tasks []EnrichedTask, services []swarm.Service) []Row {
	serviceByID := make(map[string]*swarm.Service, len(services))
	for i := range services {
		serviceByID[services[i].ID] = &services[i]
	}

	rows := make([]Row, 0, len(tasks))

	for _, task := range tasks {
		// The hostname when it is readable, the node ID otherwise — the same
		// fallback TaskDigest makes, and no disclosure either way, since the
		// task record the caller can already read carries the ID itself.
		where := task.NodeHostname
		if where == "" {
			where = task.NodeID
		}

		rows = append(rows, Row{
			ID:     task.ID,
			Name:   TaskName(task.Task, serviceByID[task.ServiceID]),
			Type:   "task",
			State:  string(task.Status.State),
			Detail: where,
		})
	}

	SortRows(rows)

	return rows
}

// RowsForConfigs builds the list view of configs. Config data is base64 and
// never belongs in a list, so Detail stays empty.
func RowsForConfigs(configs []swarm.Config) []Row {
	rows := make([]Row, 0, len(configs))

	for _, cfg := range configs {
		rows = append(rows, Row{
			ID:    cfg.ID,
			Name:  cfg.Spec.Name,
			Type:  "config",
			Stack: cfg.Spec.Labels[StackNamespaceLabel],
		})
	}

	SortRows(rows)

	return rows
}

// RowsForSecrets builds the list view of secrets. A secret's data is zeroed
// before it reaches here and nothing about it is ever placed in Detail.
func RowsForSecrets(secrets []swarm.Secret) []Row {
	rows := make([]Row, 0, len(secrets))

	for _, sec := range secrets {
		rows = append(rows, Row{
			ID:    sec.ID,
			Name:  sec.Spec.Name,
			Type:  "secret",
			Stack: sec.Spec.Labels[StackNamespaceLabel],
		})
	}

	SortRows(rows)

	return rows
}

// RowsForNetworks builds the list view of networks.
func RowsForNetworks(networks []network.Summary) []Row {
	rows := make([]Row, 0, len(networks))

	for _, net := range networks {
		rows = append(rows, Row{
			ID:     net.ID,
			Name:   net.Name,
			Type:   "network",
			Stack:  net.Labels[StackNamespaceLabel],
			Detail: net.Driver,
		})
	}

	SortRows(rows)

	return rows
}

// RowsForVolumes builds the list view of volumes. Volumes are keyed by Name
// rather than ID everywhere in Cetacean, so both fields carry the name.
func RowsForVolumes(volumes []volume.Volume) []Row {
	rows := make([]Row, 0, len(volumes))

	for _, vol := range volumes {
		rows = append(rows, Row{
			ID:     vol.Name,
			Name:   vol.Name,
			Type:   "volume",
			Stack:  vol.Labels[StackNamespaceLabel],
			Detail: vol.Driver,
		})
	}

	SortRows(rows)

	return rows
}

// RowsForStacks builds the list view of stacks. A stack is derived from labels
// rather than being a Docker primitive, so the count of services it holds is
// what makes it worth listing, and that is its Desired.
func RowsForStacks(stacks []cache.Stack) []Row {
	rows := make([]Row, 0, len(stacks))

	for _, st := range stacks {
		rows = append(rows, Row{
			ID:      st.Name,
			Name:    st.Name,
			Type:    "stack",
			Desired: len(st.Services),
		})
	}

	SortRows(rows)

	return rows
}
