package cluster

import (
	"slices"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
)

// Row is one entry in a list of cluster resources.
//
// It exists because the raw Docker object is the wrong answer to a list
// question: eight services of raw swarm.Service cost roughly fourteen thousand
// tokens, most of it Platforms entries and a duplicated PreviousSpec, and the
// field a caller actually asked for — whether the thing is healthy — is not in
// there at all, because state is derived from tasks.
//
// The shape is TopologyNode's, which already proved sufficient to diagnose a
// cluster in practice. Both ID and Name are always present so a caller never
// has to resolve one into the other, following the TargetID/TargetName pair the
// recommendations already use.
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
}

// RowsForServices builds the list view of services. Tasks are needed because a
// service's state and running count are derived from them, not from its spec.
func RowsForServices(services []swarm.Service, tasks []swarm.Task) []Row {
	running := make(map[string]int, len(services))

	for _, task := range tasks {
		if task.Status.State == swarm.TaskStateRunning {
			running[task.ServiceID]++
		}
	}

	rows := make([]Row, 0, len(services))

	for _, svc := range services {
		var image string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			image = StripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}

		rows = append(rows, Row{
			ID:      svc.ID,
			Name:    svc.Spec.Name,
			Type:    "service",
			Stack:   svc.Spec.Labels["com.docker.stack.namespace"],
			State:   DeriveServiceState(svc, running[svc.ID]),
			Detail:  image,
			Desired: ReplicaCount(svc),
			Running: running[svc.ID],
		})
	}

	sortRows(rows)

	return rows
}

// sortRows puts a list into a stable order. Callers build rows by ranging over
// cache slices whose order is not guaranteed, and the result is marshalled into
// an MCP result a client may cache by ETag.
func sortRows(rows []Row) {
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
			State:  string(n.Status.State),
			Detail: string(n.Spec.Role),
		})
	}

	sortRows(rows)

	return rows
}

// RowsForTasks builds the list view of tasks. Services and nodes are needed to
// name the task's parents: a task's own record holds only their IDs, and a
// caller reading a task list is asking which service is broken and where.
func RowsForTasks(tasks []swarm.Task, services []swarm.Service, nodes []swarm.Node) []Row {
	serviceNames := make(map[string]string, len(services))
	for _, svc := range services {
		serviceNames[svc.ID] = svc.Spec.Name
	}

	nodeNames := make(map[string]string, len(nodes))
	for _, n := range nodes {
		nodeNames[n.ID] = n.Description.Hostname
	}

	rows := make([]Row, 0, len(tasks))

	for _, task := range tasks {
		name := serviceNames[task.ServiceID]
		if name == "" {
			name = task.ID
		}

		rows = append(rows, Row{
			ID:     task.ID,
			Name:   name,
			Type:   "task",
			Stack:  "",
			State:  string(task.Status.State),
			Detail: nodeNames[task.NodeID],
		})
	}

	sortRows(rows)

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
			Stack: cfg.Spec.Labels["com.docker.stack.namespace"],
		})
	}

	sortRows(rows)

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
			Stack: sec.Spec.Labels["com.docker.stack.namespace"],
		})
	}

	sortRows(rows)

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
			Stack:  net.Labels["com.docker.stack.namespace"],
			Detail: net.Driver,
		})
	}

	sortRows(rows)

	return rows
}

// RowsForVolumes builds the list view of volumes. Volumes are keyed by Name
// rather than ID everywhere in Cetacean, so both fields carry the name.
func RowsForVolumes(volumes []*volume.Volume) []Row {
	rows := make([]Row, 0, len(volumes))

	for _, vol := range volumes {
		if vol == nil {
			continue
		}

		rows = append(rows, Row{
			ID:     vol.Name,
			Name:   vol.Name,
			Type:   "volume",
			Stack:  vol.Labels["com.docker.stack.namespace"],
			Detail: vol.Driver,
		})
	}

	sortRows(rows)

	return rows
}

// RowsForStacks builds the list view of stacks. A stack is derived from labels
// rather than being a Docker primitive, so its Detail is the thing that makes
// it worth listing: how many services it holds.
func RowsForStacks(stacks []cache.Stack) []Row {
	rows := make([]Row, 0, len(stacks))

	for _, st := range stacks {
		rows = append(rows, Row{
			ID:      st.Name,
			Name:    st.Name,
			Type:    "stack",
			Stack:   st.Name,
			Desired: len(st.Services),
		})
	}

	sortRows(rows)

	return rows
}
