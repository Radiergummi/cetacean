package api

import (
	"net/http"

	"github.com/docker/docker/api/types/swarm"
)

type NetworkTopology struct {
	Nodes    []TopoServiceNode `json:"nodes"`
	Edges    []TopoEdge        `json:"edges"`
	Networks []TopoNetwork     `json:"networks"`
}

type TopoServiceNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Stack    string `json:"stack,omitempty"`
	Replicas int    `json:"replicas"`
}

type TopoEdge struct {
	Source   string   `json:"source"`
	Target  string   `json:"target"`
	Networks []string `json:"networks"`
}

type TopoNetwork struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
}

type PlacementTopology struct {
	Nodes []TopoClusterNode `json:"nodes"`
}

type TopoClusterNode struct {
	ID       string     `json:"id"`
	Hostname string     `json:"hostname"`
	Role     string     `json:"role"`
	State    string     `json:"state"`
	Tasks    []TopoTask `json:"tasks"`
}

type TopoTask struct {
	ID          string `json:"id"`
	ServiceID   string `json:"serviceId"`
	ServiceName string `json:"serviceName"`
	State       string `json:"state"`
	Slot        int    `json:"slot"`
}

func (h *Handlers) HandleNetworkTopology(w http.ResponseWriter, r *http.Request) {
	services := h.cache.ListServices()
	networks := h.cache.ListNetworks()

	// Build overlay network lookup.
	overlayNets := make(map[string]TopoNetwork)
	for _, n := range networks {
		if n.Driver == "overlay" {
			overlayNets[n.ID] = TopoNetwork{ID: n.ID, Name: n.Name, Driver: n.Driver}
		}
	}

	// Build service nodes and track which overlay networks each service is on.
	svcNetworks := make(map[string]map[string]struct{}) // serviceID -> set of overlay networkIDs
	nodes := make([]TopoServiceNode, 0, len(services))
	for _, svc := range services {
		replicas := replicaCount(svc)
		stack := svc.Spec.Labels["com.docker.stack.namespace"]
		nodes = append(nodes, TopoServiceNode{
			ID:       svc.ID,
			Name:     svc.Spec.Name,
			Stack:    stack,
			Replicas: replicas,
		})

		nets := make(map[string]struct{})
		for _, vip := range svc.Endpoint.VirtualIPs {
			if _, ok := overlayNets[vip.NetworkID]; ok {
				nets[vip.NetworkID] = struct{}{}
			}
		}
		svcNetworks[svc.ID] = nets
	}

	// Build edges for each pair of services sharing overlay networks.
	var edges []TopoEdge
	for i := 0; i < len(services); i++ {
		for j := i + 1; j < len(services); j++ {
			a, b := services[i].ID, services[j].ID
			var shared []string
			for netID := range svcNetworks[a] {
				if _, ok := svcNetworks[b][netID]; ok {
					shared = append(shared, netID)
				}
			}
			if len(shared) > 0 {
				edges = append(edges, TopoEdge{Source: a, Target: b, Networks: shared})
			}
		}
	}

	// Collect only overlay networks that are actually referenced.
	usedNets := make(map[string]struct{})
	for _, nets := range svcNetworks {
		for id := range nets {
			usedNets[id] = struct{}{}
		}
	}
	topoNetworks := make([]TopoNetwork, 0, len(usedNets))
	for id := range usedNets {
		topoNetworks = append(topoNetworks, overlayNets[id])
	}

	writeJSON(w, NetworkTopology{
		Nodes:    nodes,
		Edges:    edges,
		Networks: topoNetworks,
	})
}

func (h *Handlers) HandlePlacementTopology(w http.ResponseWriter, r *http.Request) {
	clusterNodes := h.cache.ListNodes()
	services := h.cache.ListServices()

	// Build service name lookup.
	svcNames := make(map[string]string, len(services))
	for _, svc := range services {
		svcNames[svc.ID] = svc.Spec.Name
	}

	topoNodes := make([]TopoClusterNode, 0, len(clusterNodes))
	for _, n := range clusterNodes {
		tasks := h.cache.ListTasksByNode(n.ID)
		topoTasks := make([]TopoTask, 0, len(tasks))
		for _, t := range tasks {
			topoTasks = append(topoTasks, TopoTask{
				ID:          t.ID,
				ServiceID:   t.ServiceID,
				ServiceName: svcNames[t.ServiceID],
				State:       string(t.Status.State),
				Slot:        t.Slot,
			})
		}
		topoNodes = append(topoNodes, TopoClusterNode{
			ID:       n.ID,
			Hostname: n.Description.Hostname,
			Role:     string(n.Spec.Role),
			State:    string(n.Status.State),
			Tasks:    topoTasks,
		})
	}

	writeJSON(w, PlacementTopology{Nodes: topoNodes})
}

func replicaCount(svc swarm.Service) int {
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		return int(*svc.Spec.Mode.Replicated.Replicas)
	}
	return 0
}
