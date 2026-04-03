package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/jgf"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
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

	w.Header().Set("Deprecation", "true")
	w.Header().Add("Link", `</topology>; rel="successor-version"`)

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

	w.Header().Set("Deprecation", "true")
	w.Header().Add("Link", `</topology>; rel="successor-version"`)

	identity := auth.IdentityFromContext(r.Context())
	clusterNodes := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListNodes(),
		nodeResource,
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

// HandleTopology serves a unified JGF document containing both network and
// placement topology graphs.
func (h *Handlers) HandleTopology(w http.ResponseWriter, r *http.Request) {
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
	clusterNodes := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListNodes(),
		nodeResource,
	)

	networkGraph := buildNetworkJGF(services, networks)

	// Build service name and image lookup.
	allServices := h.cache.ListServices()
	svcNames := make(map[string]string, len(allServices))
	svcImages := make(map[string]string, len(allServices))
	for _, svc := range allServices {
		svcNames[svc.ID] = svc.Spec.Name
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			svcImages[svc.ID] = stripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}
	}

	readableServiceIDs := make(map[string]bool, len(services))
	for _, svc := range services {
		readableServiceIDs[svc.ID] = true
	}

	placementGraph := buildPlacementJGF(clusterNodes, h.cache, svcNames, svcImages, readableServiceIDs)

	doc := jgf.Document{
		Graphs: []jgf.Graph{networkGraph, placementGraph},
	}

	body, err := json.Marshal(doc)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to serialize response")
		return
	}

	etag := computeETag(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/vnd.jgf+json")
	w.Header().Set("Cache-Control", "no-cache")

	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Write(body)         //nolint:errcheck
	w.Write([]byte{'\n'}) //nolint:errcheck
}

// buildNetworkJGF produces a JGF hypergraph of the network topology.
func buildNetworkJGF(services []swarm.Service, networks []network.Summary) jgf.Graph {
	// Build overlay network lookup for fast filtering.
	type overlayInfo struct {
		name   string
		driver string
		scope  string
	}

	overlaySet := make(map[string]overlayInfo, len(networks))
	for _, n := range networks {
		if n.Driver == "overlay" {
			overlaySet[n.ID] = overlayInfo{name: n.Name, driver: n.Driver, scope: n.Scope}
		}
	}

	nodes := make(map[string]jgf.Node, len(services))
	netServices := make(map[string][]string)

	// Per-service alias lookup: networkID → serviceURN → aliases
	svcAliases := make(map[string]map[string][]string)

	// Stack membership: stackName → []serviceURN
	stacks := make(map[string][]string)

	for _, svc := range services {
		urn := jgf.URN("service", svc.ID)

		meta := jgf.Metadata{
			"@context": jsonLDContext,
			"kind":     "service",
			"replicas": replicaCount(svc),
		}

		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			meta["image"] = stripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}

		if svc.Spec.Mode.Replicated != nil {
			meta["mode"] = "replicated"
		} else if svc.Spec.Mode.Global != nil {
			meta["mode"] = "global"
		}

		if svc.Spec.EndpointSpec != nil {
			if ports := formatPorts(svc.Spec.EndpointSpec.Ports); len(ports) > 0 {
				meta["ports"] = ports
			}
		}

		if svc.UpdateStatus != nil {
			meta["updateStatus"] = string(svc.UpdateStatus.State)
		}

		nodes[urn] = jgf.Node{
			Label:    svc.Spec.Name,
			Metadata: meta,
		}

		// Collect aliases per network.
		for _, na := range svc.Spec.TaskTemplate.Networks {
			if _, ok := overlaySet[na.Target]; ok && len(na.Aliases) > 0 {
				if svcAliases[na.Target] == nil {
					svcAliases[na.Target] = make(map[string][]string)
				}
				svcAliases[na.Target][urn] = na.Aliases
			}
		}

		// Build netServices from VIPs.
		for _, vip := range svc.Endpoint.VirtualIPs {
			if _, ok := overlaySet[vip.NetworkID]; ok {
				netServices[vip.NetworkID] = append(netServices[vip.NetworkID], svc.ID)
			}
		}

		// Track stack membership.
		if stack := svc.Spec.Labels["com.docker.stack.namespace"]; stack != "" {
			stacks[stack] = append(stacks[stack], urn)
		}
	}

	// Build edges: for each pair of services sharing a network, collect
	// all shared networks and their metadata.
	type edgeKey struct{ a, b string }
	edgeNetworks := make(map[edgeKey][]string) // edgeKey → []networkID

	for netID, svcs := range netServices {
		for i := range svcs {
			for j := i + 1; j < len(svcs); j++ {
				a, b := svcs[i], svcs[j]
				if a > b {
					a, b = b, a
				}
				k := edgeKey{a, b}
				edgeNetworks[k] = append(edgeNetworks[k], netID)
			}
		}
	}

	edges := make([]jgf.Edge, 0, len(edgeNetworks))
	for k, netIDs := range edgeNetworks {
		sourceURN := jgf.URN("service", k.a)
		targetURN := jgf.URN("service", k.b)

		netEntries := make([]any, 0, len(netIDs))
		for _, netID := range netIDs {
			info := overlaySet[netID]
			entry := map[string]any{
				"id":     jgf.URN("network", netID),
				"name":   info.name,
				"driver": info.driver,
				"scope":  info.scope,
			}

			// Collect aliases for this network that belong to either endpoint.
			if aliasMap := svcAliases[netID]; len(aliasMap) > 0 {
				aliases := make(map[string]any)
				for svcURN, aliasList := range aliasMap {
					if svcURN == sourceURN || svcURN == targetURN {
						aliases[svcURN] = aliasList
					}
				}

				if len(aliases) > 0 {
					entry["aliases"] = aliases
				}
			}

			netEntries = append(netEntries, entry)
		}

		edges = append(edges, jgf.Edge{
			Source: sourceURN,
			Target: targetURN,
			Metadata: jgf.Metadata{
				"@context": jsonLDContext,
				"networks": netEntries,
			},
		})
	}

	// Build stack hyperedges.
	hyperedges := make([]jgf.Hyperedge, 0, len(stacks))
	for name, members := range stacks {
		sort.Strings(members)
		hyperedges = append(hyperedges, jgf.Hyperedge{
			Nodes: members,
			Metadata: jgf.Metadata{
				"@context": jsonLDContext,
				"kind":     "stack",
				"name":     name,
			},
		})
	}

	return jgf.Graph{
		ID:         "network-topology",
		Type:       "network",
		Label:      "Network Topology",
		Directed:   false,
		Metadata:   jgf.Metadata{"@context": jsonLDContext},
		Nodes:      nodes,
		Edges:      edges,
		Hyperedges: hyperedges,
	}
}

// buildPlacementJGF produces a JGF hypergraph of the placement topology.
func buildPlacementJGF(
	clusterNodes []swarm.Node,
	c *cache.Cache,
	svcNames, svcImages map[string]string,
	readableServiceIDs map[string]bool,
) jgf.Graph {
	nodes := make(map[string]jgf.Node)
	var hyperedges []jgf.Hyperedge

	// Track which services have tasks on which nodes.
	// serviceID → {nodeURNs set, tasks list}
	type svcPlacement struct {
		nodeURNs map[string]struct{}
		tasks    []map[string]any
	}

	placements := make(map[string]*svcPlacement)

	// Add cluster nodes.
	for _, n := range clusterNodes {
		nodeURN := jgf.URN("node", n.ID)
		nodes[nodeURN] = jgf.Node{
			Label: n.Description.Hostname,
			Metadata: jgf.Metadata{
				"@context":     jsonLDContext,
				"kind":         "node",
				"role":         string(n.Spec.Role),
				"state":        string(n.Status.State),
				"availability": string(n.Spec.Availability),
			},
		}

		tasks := c.ListTasksByNode(n.ID)
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

			sp := placements[t.ServiceID]
			if sp == nil {
				sp = &svcPlacement{nodeURNs: make(map[string]struct{})}
				placements[t.ServiceID] = sp
			}

			sp.nodeURNs[nodeURN] = struct{}{}
			sp.tasks = append(sp.tasks, map[string]any{
				"id":    jgf.URN("task", t.ID),
				"node":  nodeURN,
				"state": string(t.Status.State),
				"slot":  t.Slot,
				"image": taskImage,
			})
		}
	}

	// Add service nodes and build hyperedges.
	for svcID, sp := range placements {
		svcURN := jgf.URN("service", svcID)
		nodes[svcURN] = jgf.Node{
			Label: svcNames[svcID],
			Metadata: jgf.Metadata{
				"@context": jsonLDContext,
				"kind":     "service",
				"image":    svcImages[svcID],
			},
		}

		// Service URN first, then node URNs sorted.
		heNodes := []string{svcURN}
		for nodeURN := range sp.nodeURNs {
			heNodes = append(heNodes, nodeURN)
		}

		sort.Strings(heNodes[1:])

		hyperedges = append(hyperedges, jgf.Hyperedge{
			Nodes: heNodes,
			Metadata: jgf.Metadata{
				"@context": jsonLDContext,
				"tasks":    sp.tasks,
			},
		})
	}

	return jgf.Graph{
		ID:         "placement-topology",
		Type:       "placement",
		Label:      "Placement Topology",
		Directed:   false,
		Metadata:   jgf.Metadata{"@context": jsonLDContext},
		Nodes:      nodes,
		Hyperedges: hyperedges,
	}
}
