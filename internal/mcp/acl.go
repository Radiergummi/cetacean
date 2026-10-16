package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

// checkRead enforces the "read" permission on resourceType:resourceName for
// the identity in ctx. Returns nil if ACL is disabled or no identity is on the
// context (the bearer middleware would have rejected the request earlier in
// that case). Mirrors checkWrite in tools.go.
func (s *Server) checkRead(ctx context.Context, resourceType, resourceName string) error {
	if s.acl == nil {
		return nil
	}
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return nil
	}
	if !s.acl.Can(identity, "read", resourceType+":"+resourceName) {
		return fmt.Errorf("read access denied for %s:%s", resourceType, resourceName)
	}
	return nil
}

// readPredicate is checkRead in the predicate form the shared rules in
// internal/cluster take.
func (s *Server) readPredicate(ctx context.Context) func(resource string) bool {
	identity := auth.IdentityFromContext(ctx)

	return func(resource string) bool {
		return identity == nil || s.canRead(identity, resource)
	}
}

// requireAnyGrant refuses a caller holding no grant at all, the gate REST puts
// in front of the cluster-wide aggregates that no per-resource filter covers.
func (s *Server) requireAnyGrant(ctx context.Context) error {
	identity := auth.IdentityFromContext(ctx)
	if s.acl == nil || identity == nil || s.acl.HasAnyGrant(identity) {
		return nil
	}
	return errors.New("no grants found for this identity")
}

func (s *Server) filterNodes(ctx context.Context, items []swarm.Node) []swarm.Node {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(n swarm.Node) string {
			return "node:" + cluster.NodeACLName(n)
		},
	)
}

func (s *Server) filterServices(ctx context.Context, items []swarm.Service) []swarm.Service {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(svc swarm.Service) string {
			return "service:" + svc.Spec.Name
		},
	)
}

// filterRawTasks keeps the tasks the caller may read, without enriching them.
// The ACL key is "task:<id>", which swarm.Task already carries, and the
// builders resolve parents from the slices they are given — so enriching first
// would resolve names only to discard them, from behind the caller's grants.
func (s *Server) filterRawTasks(ctx context.Context, items []swarm.Task) []swarm.Task {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(t swarm.Task) string {
			return "task:" + t.ID
		},
	)
}

func (s *Server) filterStacks(ctx context.Context, items []cache.Stack) []cache.Stack {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(st cache.Stack) string {
			return "stack:" + st.Name
		},
	)
}

func (s *Server) filterConfigs(ctx context.Context, items []swarm.Config) []swarm.Config {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(c swarm.Config) string {
			return "config:" + c.Spec.Name
		},
	)
}

func (s *Server) filterSecrets(ctx context.Context, items []swarm.Secret) []swarm.Secret {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(sec swarm.Secret) string {
			return "secret:" + sec.Spec.Name
		},
	)
}

func (s *Server) filterNetworks(ctx context.Context, items []network.Summary) []network.Summary {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(n network.Summary) string {
			return "network:" + n.Name
		},
	)
}

func (s *Server) filterVolumes(ctx context.Context, items []volume.Volume) []volume.Volume {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(v volume.Volume) string {
			return "volume:" + v.Name
		},
	)
}

// filterServiceRefs drops the services an identity may not read from one of the
// cache's reverse indexes. A digest's "used by" list is built from those, and
// would otherwise name services the caller cannot see merely because they can
// read the config those services mount.
func (s *Server) filterServiceRefs(
	ctx context.Context,
	items []cache.ServiceRef,
) []cache.ServiceRef {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(ref cache.ServiceRef) string {
			return "service:" + ref.Name
		},
	)
}

// filterRecommendations mirrors api/recommendation_handlers.go.
func (s *Server) filterRecommendations(
	ctx context.Context,
	items []recommendations.Recommendation,
) []recommendations.Recommendation {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		recommendationACLResource,
	)
}

func recommendationACLResource(rec recommendations.Recommendation) string {
	switch rec.Scope {
	case recommendations.ScopeService:
		return "service:" + rec.TargetName
	case recommendations.ScopeNode:
		return "node:" + rec.TargetName
	default:
		return "swarm:cluster"
	}
}

// readableEnrichedTasks filters tasks to the ones the caller may read, then
// names their parents from the services and nodes they may read too. The
// enrichment has to be ACL-aware: cluster.EnrichTasks resolves straight out of
// the cache, so a bare `task:*` grant would name every service in the cluster.
func (s *Server) readableEnrichedTasks(
	ctx context.Context,
	tasks []swarm.Task,
) []cluster.EnrichedTask {
	return cluster.EnrichTasksWithin(
		s.filterRawTasks(ctx, tasks),
		s.filterServices(ctx, s.cache.ListServices()),
		s.filterNodes(ctx, s.cache.ListNodes()),
	)
}

// readableEnrichedTask is readableEnrichedTasks for a single, already
// permitted task: resolving its two parents by ID beats listing and filtering
// both types to name them.
func (s *Server) readableEnrichedTask(
	ctx context.Context,
	task swarm.Task,
) cluster.EnrichedTask {
	enriched := cluster.EnrichedTask{Task: task}

	if svc := s.readableService(ctx, task.ServiceID); svc != nil {
		enriched.ServiceName = svc.Spec.Name
	}

	if node := s.readableNode(ctx, task.NodeID); node != nil {
		enriched.NodeHostname = node.Description.Hostname
	}

	return enriched
}

// checkWrite enforces the "write" permission on resourceType:resourceName for
// the identity in ctx, returning nil when ACL is disabled or no identity is on
// the context — the bearer middleware would have rejected the request already.
// Every tool handler routes through it, so denials have one shape.
func (s *Server) checkWrite(ctx context.Context, resourceType, resourceName string) error {
	if s.acl == nil {
		return nil
	}
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return nil
	}
	if !s.acl.Can(identity, "write", resourceType+":"+resourceName) {
		return fmt.Errorf("write access denied for %s:%s", resourceType, resourceName)
	}
	return nil
}

// writePredicate is checkWrite in the predicate form the shared rules in
// internal/cluster take.
func (s *Server) writePredicate(ctx context.Context) func(resource string) bool {
	identity := auth.IdentityFromContext(ctx)

	return func(resource string) bool {
		return identity == nil || s.acl.Can(identity, "write", resource)
	}
}

// checkResolvedWrite enforces write on the resource arg resolved to, by exact
// ID or name. Docker also takes an ID prefix, which the ACL would have checked
// as written, so an argument the cache does not resolve is refused as missing,
// as is one the caller cannot read, which a denial would otherwise name.
func (s *Server) checkResolvedWrite(
	ctx context.Context,
	resourceType, arg, name string,
	found bool,
	err error,
) error {
	if s.acl == nil {
		return nil
	}
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return nil
	}

	if err != nil {
		if s.acl.Can(identity, "read", resourceType+":"+arg) {
			return err
		}
		found = false
	}
	if !found || !s.acl.Can(identity, "read", resourceType+":"+name) {
		return fmt.Errorf("%s %q not found", resourceType, arg)
	}

	return s.checkWrite(ctx, resourceType, name)
}

// checkServiceWrite keys the ACL as `service:<name>`, matching REST behaviour.
func (s *Server) checkServiceWrite(ctx context.Context, id string) error {
	svc, ok, err := s.cache.ResolveService(id)
	return s.checkResolvedWrite(ctx, "service", id, svc.Spec.Name, ok, err)
}

// checkNodeWrite keys the ACL as `node:<hostname>`, matching REST behaviour.
func (s *Server) checkNodeWrite(ctx context.Context, id string) error {
	node, ok, err := s.cache.ResolveNode(id)
	return s.checkResolvedWrite(ctx, "node", id, cluster.NodeACLName(node), ok, err)
}

// checkConfigWrite keys the ACL as `config:<name>`, matching REST behaviour.
func (s *Server) checkConfigWrite(ctx context.Context, id string) error {
	cfg, ok, err := s.cache.ResolveConfig(id)
	return s.checkResolvedWrite(ctx, "config", id, cfg.Spec.Name, ok, err)
}

// checkSecretWrite keys the ACL as `secret:<name>`, matching REST behaviour.
func (s *Server) checkSecretWrite(ctx context.Context, id string) error {
	sec, ok, err := s.cache.ResolveSecret(id)
	return s.checkResolvedWrite(ctx, "secret", id, sec.Spec.Name, ok, err)
}

// checkNetworkWrite keys the ACL as `network:<name>`, matching REST behaviour.
func (s *Server) checkNetworkWrite(ctx context.Context, id string) error {
	net, ok, err := s.cache.ResolveNetwork(id)
	return s.checkResolvedWrite(ctx, "network", id, net.Name, ok, err)
}

// checkTaskWrite routes the ACL check to the task's parent service so an MCP
// `remove_task` evaluates the same key REST DELETE /tasks/{id} does
// (`service:<name>`), falling back to `task:<id>` as REST does when the cache
// cannot resolve the parent service.
func (s *Server) checkTaskWrite(ctx context.Context, id string) error {
	task, ok, err := cluster.ResolveTask(s.cache, id)

	svc, found := s.cache.GetService(task.ServiceID)
	if !ok || !found || svc.Spec.Name == "" {
		return s.checkResolvedWrite(ctx, "task", id, task.ID, ok, err)
	}

	if s.checkRead(ctx, "service", svc.Spec.Name) != nil {
		return fmt.Errorf("task %q not found", id)
	}

	return s.checkWrite(ctx, "service", svc.Spec.Name)
}

// checkServiceRead resolves Spec.Name from the cache so the ACL key is
// `service:<name>`, matching lookupResource; get_logs does not go through it.
// It resolves rather than looking the ID up because get_logs accepts either,
// and a plain GetService turns a name into "service not found" under a policy.
func (s *Server) checkServiceRead(ctx context.Context, id string) error {
	if s.acl == nil {
		return nil
	}
	svc, ok, err := s.cache.ResolveService(id)
	if err != nil {
		return err
	}
	if !ok || s.checkRead(ctx, "service", svc.Spec.Name) != nil {
		return fmt.Errorf("service %q not found", id)
	}
	return nil
}

// checkTaskRead enforces the read permission on a task, keyed as `task:<id>`.
// It does not walk to the parent the way checkTaskWrite does, because
// acl.grantMatchesResource already resolves a task through its service and
// stack — and every other task read passes this same key.
func (s *Server) checkTaskRead(ctx context.Context, id string) error {
	if s.checkRead(ctx, "task", id) != nil {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}
