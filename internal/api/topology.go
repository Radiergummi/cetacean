package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

type NetworkTopology struct {
	Nodes    []TopoServiceNode `json:"nodes"`
	Edges    []TopoEdge        `json:"edges"`
	Networks []TopoNetwork     `json:"networks"`
}

type TopoServiceNode struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Stack          string              `json:"stack,omitempty"`
	Replicas       int                 `json:"replicas"`
	Image          string              `json:"image"`
	Ports          []string            `json:"ports,omitempty"`
	Mode           string              `json:"mode"`
	UpdateStatus   string              `json:"updateStatus,omitempty"`
	NetworkAliases map[string][]string `json:"networkAliases,omitempty"`
}

type TopoEdge struct {
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Networks []string `json:"networks"`
}

type TopoNetwork struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Scope  string `json:"scope"`
	Stack  string `json:"stack,omitempty"`
}

type PlacementTopology struct {
	Nodes []TopoClusterNode `json:"nodes"`
}

type TopoClusterNode struct {
	ID           string     `json:"id"`
	Hostname     string     `json:"hostname"`
	Role         string     `json:"role"`
	State        string     `json:"state"`
	Availability string     `json:"availability"`
	Tasks        []TopoTask `json:"tasks"`
}

type TopoTask struct {
	ID          string `json:"id"`
	ServiceID   string `json:"serviceId"`
	ServiceName string `json:"serviceName"`
	State       string `json:"state"`
	Slot        int    `json:"slot"`
	Image       string `json:"image"`
}

func (h *Handlers) HandleNetworkTopology(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	identity := auth.IdentityFromContext(r.Context())
	services := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListServices(),
		func(s swarm.Service) string { return "service:" + s.Spec.Name },
	)
	networks := h.cache.ListNetworks()

	// Build overlay network ID set for fast filtering.
	overlaySet := make(map[string]struct{}, len(networks))
	for _, n := range networks {
		if n.Driver == "overlay" {
			overlaySet[n.ID] = struct{}{}
		}
	}

	// Build service nodes and netServices (network → service list) in one pass.
	netServices := make(map[string][]string)
	nodes := make([]TopoServiceNode, 0, len(services))
	for _, svc := range services {
		replicas := replicaCount(svc)
		stack := svc.Spec.Labels["com.docker.stack.namespace"]

		var image string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			image = stripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}
		var mode string
		if svc.Spec.Mode.Replicated != nil {
			mode = "replicated"
		} else if svc.Spec.Mode.Global != nil {
			mode = "global"
		}
		var ports []string
		if svc.Spec.EndpointSpec != nil {
			ports = formatPorts(svc.Spec.EndpointSpec.Ports)
		}
		var updateStatus string
		if svc.UpdateStatus != nil {
			updateStatus = string(svc.UpdateStatus.State)
		}

		var networkAliases map[string][]string
		for _, na := range svc.Spec.TaskTemplate.Networks {
			if _, ok := overlaySet[na.Target]; ok && len(na.Aliases) > 0 {
				if networkAliases == nil {
					networkAliases = make(map[string][]string)
				}
				networkAliases[na.Target] = na.Aliases
			}
		}

		nodes = append(nodes, TopoServiceNode{
			ID:             svc.ID,
			Name:           svc.Spec.Name,
			Stack:          stack,
			Replicas:       replicas,
			Image:          image,
			Ports:          ports,
			Mode:           mode,
			UpdateStatus:   updateStatus,
			NetworkAliases: networkAliases,
		})

		// Build netServices directly — no intermediate svcNetworks map.
		for _, vip := range svc.Endpoint.VirtualIPs {
			if _, ok := overlaySet[vip.NetworkID]; ok {
				netServices[vip.NetworkID] = append(netServices[vip.NetworkID], svc.ID)
			}
		}
	}

	// Build edges: for each network, emit an edge for every pair of services.
	// Deduplicate with a set keyed by the ordered pair.
	type edgeKey struct{ a, b string }
	edgeMap := make(map[edgeKey][]string)
	for netID, svcs := range netServices {
		for i := range svcs {
			for j := i + 1; j < len(svcs); j++ {
				a, b := svcs[i], svcs[j]
				if a > b {
					a, b = b, a
				}
				edgeMap[edgeKey{a, b}] = append(edgeMap[edgeKey{a, b}], netID)
			}
		}
	}
	edges := make([]TopoEdge, 0, len(edgeMap))
	for k, nets := range edgeMap {
		edges = append(edges, TopoEdge{Source: k.a, Target: k.b, Networks: nets})
	}

	// Build TopoNetwork only for networks that have services attached.
	topoNetworks := make([]TopoNetwork, 0, len(netServices))
	for _, n := range networks {
		if _, used := netServices[n.ID]; used {
			topoNetworks = append(topoNetworks, TopoNetwork{
				ID:     n.ID,
				Name:   n.Name,
				Driver: n.Driver,
				Scope:  n.Scope,
				Stack:  n.Labels["com.docker.stack.namespace"],
			})
		}
	}

	writeCachedJSON(w, r, NetworkTopology{
		Nodes:    nodes,
		Edges:    edges,
		Networks: topoNetworks,
	})
}

func (h *Handlers) HandlePlacementTopology(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	identity := auth.IdentityFromContext(r.Context())
	clusterNodes := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListNodes(),
		func(n swarm.Node) string { return "node:" + n.Description.Hostname },
	)
	services := h.cache.ListServices()

	// Build service name and image lookup.
	svcNames := make(map[string]string, len(services))
	svcImages := make(map[string]string, len(services))
	for _, svc := range services {
		svcNames[svc.ID] = svc.Spec.Name
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			svcImages[svc.ID] = stripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}
	}

	// Build a set of readable service IDs for task filtering.
	readableServiceIDs := make(map[string]bool, len(services))
	for _, svc := range acl.Filter(
		h.acl, identity, "read",
		services,
		func(s swarm.Service) string { return "service:" + s.Spec.Name },
	) {
		readableServiceIDs[svc.ID] = true
	}

	topoNodes := make([]TopoClusterNode, 0, len(clusterNodes))
	for _, n := range clusterNodes {
		tasks := h.cache.ListTasksByNode(n.ID)
		topoTasks := make([]TopoTask, 0, len(tasks))
		for _, t := range tasks {
			if !readableServiceIDs[t.ServiceID] {
				continue
			}

			var taskImage string
			if t.Spec.ContainerSpec != nil {
				taskImage = stripImageDigest(t.Spec.ContainerSpec.Image)
			}
			if taskImage == "" {
				taskImage = svcImages[t.ServiceID]
			}
			topoTasks = append(topoTasks, TopoTask{
				ID:          t.ID,
				ServiceID:   t.ServiceID,
				ServiceName: svcNames[t.ServiceID],
				State:       string(t.Status.State),
				Slot:        t.Slot,
				Image:       taskImage,
			})
		}
		topoNodes = append(topoNodes, TopoClusterNode{
			ID:           n.ID,
			Hostname:     n.Description.Hostname,
			Role:         string(n.Spec.Role),
			State:        string(n.Status.State),
			Availability: string(n.Spec.Availability),
			Tasks:        topoTasks,
		})
	}

	writeCachedJSON(w, r, PlacementTopology{Nodes: topoNodes})
}

func replicaCount(svc swarm.Service) int {
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		return int(*svc.Spec.Mode.Replicated.Replicas)
	}
	return 0
}

func stripImageDigest(image string) string {
	if before, _, ok := strings.Cut(image, "@sha256:"); ok {
		return before
	}
	return image
}

func formatPorts(ports []swarm.PortConfig) []string {
	if len(ports) == 0 {
		return nil
	}
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = fmt.Sprintf("%d:%d/%s", p.PublishedPort, p.TargetPort, p.Protocol)
	}
	return out
}
