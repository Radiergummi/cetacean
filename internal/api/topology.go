package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api/dot"
	"github.com/radiergummi/cetacean/internal/api/graphml"
	"github.com/radiergummi/cetacean/internal/api/jgf"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

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

	contextURL := absPath(r.Context(), jsonLDContext)
	networkGraph := buildNetworkJGF(services, networks, contextURL)

	// Build service name/image lookup and readable set from ACL-filtered services.
	svcNames := make(map[string]string, len(services))
	svcImages := make(map[string]string, len(services))
	readableServiceIDs := make(map[string]bool, len(services))
	for _, svc := range services {
		svcNames[svc.ID] = svc.Spec.Name
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			svcImages[svc.ID] = cluster.StripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
		}
		readableServiceIDs[svc.ID] = true
	}

	placementGraph := buildPlacementJGF(
		clusterNodes,
		h.cache,
		svcNames,
		svcImages,
		readableServiceIDs,
		contextURL,
	)

	doc := jgf.Document{
		Graphs: []jgf.Graph{networkGraph, placementGraph},
	}

	body, err := json.Marshal(doc)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to serialize response")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.jgf+json")
	writeRawWithETag(w, r, body)
}

// buildACLFilteredNetworkGraph builds the network JGF graph for the requesting
// identity, applying ACL filtering on services.
func (h *Handlers) buildACLFilteredNetworkGraph(r *http.Request) jgf.Graph {
	identity := auth.IdentityFromContext(r.Context())
	services := acl.Filter(
		h.acl, identity, "read",
		h.cache.ListServices(),
		func(s swarm.Service) string { return "service:" + s.Spec.Name },
	)
	networks := h.cache.ListNetworks()
	contextURL := absPath(r.Context(), jsonLDContext)
	return buildNetworkJGF(services, networks, contextURL)
}

// HandleTopologyGraphML serves the network topology as a GraphML document.
func (h *Handlers) HandleTopologyGraphML(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}
	g := h.buildACLFilteredNetworkGraph(r)
	data, err := graphml.Render(g)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to render GraphML")
		return
	}
	w.Header().Set("Content-Type", "application/graphml+xml")
	writeRawWithETag(w, r, data)
}

// HandleTopologyDOT serves the network topology as a DOT (Graphviz) document.
func (h *Handlers) HandleTopologyDOT(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}
	g := h.buildACLFilteredNetworkGraph(r)
	data, err := dot.Render(g)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to render DOT")
		return
	}
	w.Header().Set("Content-Type", "text/vnd.graphviz")
	writeRawWithETag(w, r, data)
}

// buildNetworkJGF produces a JGF hypergraph of the network topology.
func buildNetworkJGF(
	services []swarm.Service,
	networks []network.Summary,
	contextURL string,
) jgf.Graph {
	overlays := cluster.OverlayNetworks(networks)

	nodes := make(map[string]jgf.Node, len(services))
	netServices := make(map[string][]string)

	// Per-service alias lookup: networkID → serviceURN → aliases
	svcAliases := make(map[string]map[string][]string)

	// Stack membership: stackName → []serviceURN
	stacks := make(map[string][]string)

	for _, svc := range services {
		urn := jgf.URN("service", svc.ID)

		meta := jgf.Metadata{
			"@context": contextURL,
			"kind":     "service",
			"replicas": cluster.ReplicaCount(svc),
		}

		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			meta["image"] = cluster.StripImageDigest(svc.Spec.TaskTemplate.ContainerSpec.Image)
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

		// One join for both membership and aliases — see
		// cluster.ServiceAttachments for why neither source alone is enough.
		for networkID, aliases := range cluster.ServiceAttachments(svc, overlays) {
			netServices[networkID] = append(netServices[networkID], svc.ID)

			if len(aliases) == 0 {
				continue
			}

			if svcAliases[networkID] == nil {
				svcAliases[networkID] = make(map[string][]string)
			}

			svcAliases[networkID][urn] = aliases
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
				// Sort by raw ID for stable edge direction. Equivalent to
				// URN order because the shared prefix cancels out.
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
			info := overlays[netID]
			entry := map[string]any{
				"id":     jgf.URN("network", netID),
				"name":   info.Name,
				"driver": info.Driver,
				"scope":  info.Scope,
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
				"@context": contextURL,
				"networks": netEntries,
			},
		})
	}

	// Sort edges for deterministic output (map iteration is non-deterministic).
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Target < edges[j].Target
	})

	// Build stack hyperedges.
	hyperedges := make([]jgf.Hyperedge, 0, len(stacks))
	for name, members := range stacks {
		sort.Strings(members)
		hyperedges = append(hyperedges, jgf.Hyperedge{
			Nodes: members,
			Metadata: jgf.Metadata{
				"@context": contextURL,
				"kind":     "stack",
				"name":     name,
			},
		})
	}

	// Sort hyperedges for deterministic output.
	sort.Slice(hyperedges, func(i, j int) bool {
		nameI, _ := hyperedges[i].Metadata["name"].(string)
		nameJ, _ := hyperedges[j].Metadata["name"].(string)
		return nameI < nameJ
	})

	return jgf.Graph{
		ID:         "network",
		Type:       "network-topology",
		Label:      "Network Topology",
		Directed:   false,
		Metadata:   jgf.Metadata{"@context": contextURL},
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
	contextURL string,
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
				"@context":     contextURL,
				"kind":         "node",
				"role":         string(n.Spec.Role),
				"state":        string(n.Status.State),
				"availability": string(n.Spec.Availability),
			},
		}

		tasks := c.ListTasksByNode(n.ID)
		for _, t := range tasks {
			// A replaced task still sits on its node — see cluster.TaskIsLive.
			if !readableServiceIDs[t.ServiceID] || !cluster.TaskIsLive(t) {
				continue
			}

			var taskImage string
			if t.Spec.ContainerSpec != nil {
				taskImage = cluster.StripImageDigest(t.Spec.ContainerSpec.Image)
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
				"@context": contextURL,
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

		// Sort tasks by ID for deterministic output.
		sort.Slice(sp.tasks, func(i, j int) bool {
			idI, _ := sp.tasks[i]["id"].(string)
			idJ, _ := sp.tasks[j]["id"].(string)
			return idI < idJ
		})

		hyperedges = append(hyperedges, jgf.Hyperedge{
			Nodes: heNodes,
			Metadata: jgf.Metadata{
				"@context": contextURL,
				"kind":     "placement",
				"tasks":    sp.tasks,
			},
		})
	}

	// Sort hyperedges by first node (service URN) for deterministic output.
	sort.Slice(hyperedges, func(i, j int) bool {
		return hyperedges[i].Nodes[0] < hyperedges[j].Nodes[0]
	})

	return jgf.Graph{
		ID:         "placement",
		Type:       "placement-topology",
		Label:      "Placement Topology",
		Directed:   false,
		Metadata:   jgf.Metadata{"@context": contextURL},
		Nodes:      nodes,
		Hyperedges: hyperedges,
	}
}
