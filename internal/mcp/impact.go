package mcp

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/mcp/projection"
)

var notRemoved = removalResult{Removed: false}

// serviceRemoval is the confirmation remove_service asks for.
func (s *Server) serviceRemoval(_ context.Context, arg string) (confirmation, error) {
	svc, found, err := s.cache.ResolveService(arg)
	if err != nil {
		return confirmation{}, err
	}

	if !found {
		return confirmation{}, fmt.Errorf("service %q not found", arg)
	}

	impact := projection.ServiceRemovalImpact(svc, s.cache.RunningTaskCount(svc.ID))

	return confirmation{
		target:   "service:" + svc.Spec.Name,
		message:  impact.Message(),
		version:  svc.Version.Index,
		bind:     svc.ID,
		declined: notRemoved,
	}, nil
}

// taskRemoval is the confirmation remove_task asks for; id is already resolved.
func (s *Server) taskRemoval(ctx context.Context, id string) (confirmation, error) {
	task, found := s.cache.GetTask(id)
	if !found {
		return confirmation{}, fmt.Errorf("task %q not found", id)
	}

	label, service := task.ID, ""
	if svc := s.readableService(ctx, task.ServiceID); svc != nil {
		label, service = cluster.TaskName(task, svc), svc.Spec.Name
	}

	return confirmation{
		target:   "task:" + task.ID,
		message:  projection.TaskRemovalImpact(label, service).Message(),
		version:  task.Version.Index,
		bind:     task.ID,
		declined: notRemoved,
	}, nil
}

// referenceRemoval builds the confirmation for removing a resource services
// reference by ID: a config, a secret or a network.
func (s *Server) referenceRemoval(
	kind string,
	users func(id string) []cache.ServiceRef,
) func(ctx context.Context, arg string) (confirmation, error) {
	return func(ctx context.Context, arg string) (confirmation, error) {
		record, found, err := cluster.Lookup(s.cache, kind, arg)
		if err != nil {
			return confirmation{}, s.visibleAmbiguity(ctx, kind, err)
		}

		if !found {
			return confirmation{}, fmt.Errorf("%s %q not found", kind, arg)
		}

		impact := projection.ReferenceRemovalImpact(
			kind, record.Name, users(record.ID), s.readPredicate(ctx))

		return confirmation{
			target:   record.Resource(),
			message:  impact.Message(),
			version:  record.Version,
			bind:     record.ID,
			declined: notRemoved,
		}, nil
	}
}

func (s *Server) configRemoval(ctx context.Context, arg string) (confirmation, error) {
	return s.referenceRemoval("config", s.cache.ServicesUsingConfig)(ctx, arg)
}

func (s *Server) secretRemoval(ctx context.Context, arg string) (confirmation, error) {
	return s.referenceRemoval("secret", s.cache.ServicesUsingSecret)(ctx, arg)
}

func (s *Server) networkRemoval(ctx context.Context, arg string) (confirmation, error) {
	return s.referenceRemoval("network", s.cache.ServicesUsingNetwork)(ctx, arg)
}

// volumeRemoval is the confirmation remove_volume asks for. A volume the
// cache does not hold may live where it cannot see, so it is asked about by
// name alone rather than refused.
func (s *Server) volumeRemoval(ctx context.Context, name string, force bool) confirmation {
	impact := projection.ReferenceRemovalImpact(
		"volume", name, s.cache.ServicesUsingVolume(name), s.readPredicate(ctx))

	if force {
		impact.Effects = append(impact.Effects,
			"Forced: a cluster volume is removed even while it is in use.")
	}

	bind := name
	if vol, found := s.cache.GetVolume(name); found {
		bind += "@" + vol.CreatedAt
	}

	return confirmation{
		target:   "volume:" + name,
		message:  impact.Message(),
		bind:     bind,
		declined: notRemoved,
	}
}

// nodeChange is the confirmation update_node asks for; value is already parsed.
func (s *Server) nodeChange(
	ctx context.Context,
	arg, section string,
	availability swarm.NodeAvailability,
	role swarm.NodeRole,
) (confirmation, error) {
	node, found, err := s.cache.ResolveNode(arg)
	if err != nil {
		return confirmation{}, s.visibleAmbiguity(ctx, "node", err)
	}

	if !found {
		return confirmation{}, fmt.Errorf("node %q not found", arg)
	}

	var impact projection.Impact

	switch {
	case section == sectionRole:
		impact = projection.RoleImpact(node, s.cache.ListNodes(), role)
	case availability == swarm.NodeAvailabilityDrain:
		impact = s.drainConfirmation(ctx, node)
	default:
		impact = projection.AvailabilityImpact(node, availability)
	}

	return confirmation{
		target:   "node:" + cluster.NodeACLName(node),
		message:  impact.Message(),
		version:  node.Version.Index,
		bind:     node.ID,
		declined: nodeUpdate(section, node),
	}, nil
}

// drainConfirmation assesses a drain from what the caller may read, as the
// drain-impact topology view does, and counts the services it hides.
func (s *Server) drainConfirmation(ctx context.Context, node swarm.Node) projection.Impact {
	all := s.cache.ListNodes()
	visible := s.filterNodes(ctx, all)
	services := s.filterServices(ctx, s.cache.ListServices())

	graph := cluster.DrainImpactGraph(
		node,
		visible,
		s.filterRawTasks(ctx, s.cache.ListTasks()),
		services,
	)
	cluster.NoteHiddenNodes(&graph, len(visible), len(all)-len(visible))

	readable := make(map[string]bool, len(services))
	for _, svc := range services {
		readable[svc.ID] = true
	}

	hidden := map[string]bool{}

	for _, task := range s.cache.ListTasksByNode(node.ID) {
		if cluster.TaskIsLive(task) && !readable[task.ServiceID] {
			hidden[task.ServiceID] = true
		}
	}

	return projection.DrainImpact(node, graph, len(hidden))
}
