package cache

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/docker/docker/api/types/swarm"
)

// PublishedPort is one port a service publishes on the cluster's nodes. An
// ingress port is bound on every node, so only a host-mode port names Nodes.
type PublishedPort struct {
	PublishedPort uint32   `json:"publishedPort"`
	TargetPort    uint32   `json:"targetPort"`
	Protocol      string   `json:"protocol"`
	PublishMode   string   `json:"publishMode"`
	ServiceID     string   `json:"serviceId"`
	ServiceName   string   `json:"serviceName"`
	Stack         string   `json:"stack,omitempty"`
	Nodes         []string `json:"nodes,omitempty"`
}

// ListPublishedPorts returns every published port, by port then protocol. A
// host-mode port is read off the running tasks rather than the endpoint: only
// they say which nodes bind it, and which port each got when Swarm chose one.
func (c *Cache) ListPublishedPorts() []PublishedPort {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []PublishedPort

	// A host-mode port's row, by service, protocol and port, gathers the
	// nodes of every task binding it.
	hostRows := map[string]int{}

	for _, t := range c.tasks {
		if t.Status.State != swarm.TaskStateRunning {
			continue
		}

		svc, ok := c.services[t.ServiceID]
		if !ok {
			continue
		}

		for _, p := range t.Status.PortStatus.Ports {
			if p.PublishMode != swarm.PortConfigPublishModeHost {
				continue
			}

			key := fmt.Sprintf("%s/%s/%d/%d", svc.ID, p.Protocol, p.PublishedPort, p.TargetPort)

			index, ok := hostRows[key]
			if !ok {
				index = len(out)
				hostRows[key] = index
				out = append(out, publishedPort(svc, p))
			}

			if !slices.Contains(out[index].Nodes, t.NodeID) {
				out[index].Nodes = append(out[index].Nodes, t.NodeID)
			}
		}
	}

	for i := range out {
		slices.Sort(out[i].Nodes)
	}

	for _, svc := range c.services {
		for _, p := range svc.Endpoint.Ports {
			if p.PublishMode != swarm.PortConfigPublishModeHost && p.PublishedPort != 0 {
				out = append(out, publishedPort(svc, p))
			}
		}
	}

	slices.SortFunc(out, func(a, b PublishedPort) int {
		return cmp.Or(
			cmp.Compare(a.PublishedPort, b.PublishedPort),
			cmp.Compare(a.Protocol, b.Protocol),
			cmp.Compare(a.ServiceName, b.ServiceName),
		)
	})

	return out
}

func publishedPort(svc swarm.Service, p swarm.PortConfig) PublishedPort {
	mode := p.PublishMode
	if mode == "" {
		mode = swarm.PortConfigPublishModeIngress
	}

	return PublishedPort{
		PublishedPort: p.PublishedPort,
		TargetPort:    p.TargetPort,
		Protocol:      string(p.Protocol),
		PublishMode:   string(mode),
		ServiceID:     svc.ID,
		ServiceName:   svc.Spec.Name,
		Stack:         svc.Spec.Labels[StackLabel],
	}
}
