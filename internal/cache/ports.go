package cache

import (
	"cmp"
	"slices"

	"github.com/docker/docker/api/types/swarm"
)

// PublishedPort is one port a service publishes, as Docker reports it, with the
// service publishing it. An ingress port is bound on every node, so only a
// host-mode port names Nodes.
type PublishedPort struct {
	swarm.PortConfig
	ServiceID   string   `json:"ServiceID"`
	ServiceName string   `json:"ServiceName"`
	Stack       string   `json:"Stack,omitempty"`
	Nodes       []string `json:"Nodes,omitempty"`
}

type hostPortKey struct {
	service           string
	protocol          swarm.PortConfigProtocol
	published, target uint32
}

// ListPublishedPorts returns every published port, by port, protocol, target
// and service. A host-mode port is read off the tasks holding it rather than
// the endpoint: only they say which nodes bind it, and which port each got
// when Swarm chose one. A starting task already holds its port.
func (c *Cache) ListPublishedPorts() []PublishedPort {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out []PublishedPort

	hostRows := map[hostPortKey]int{}

	for _, t := range c.tasks {
		if t.Status.State != swarm.TaskStateRunning && t.Status.State != swarm.TaskStateStarting {
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

			key := hostPortKey{svc.ID, p.Protocol, p.PublishedPort, p.TargetPort}

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
			cmp.Compare(a.TargetPort, b.TargetPort),
			cmp.Compare(a.ServiceName, b.ServiceName),
		)
	})

	return out
}

func publishedPort(svc swarm.Service, p swarm.PortConfig) PublishedPort {
	if p.PublishMode == "" {
		p.PublishMode = swarm.PortConfigPublishModeIngress
	}

	return PublishedPort{
		PortConfig:  p,
		ServiceID:   svc.ID,
		ServiceName: svc.Spec.Name,
		Stack:       svc.Spec.Labels[StackLabel],
	}
}
