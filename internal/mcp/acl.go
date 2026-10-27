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
// that case). Mirrors checkWrite.
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
	return s.readerFor(auth.IdentityFromContext(ctx))
}

// readerFor is readPredicate for an identity already in hand; a nil identity
// reads all, as in checkRead.
func (s *Server) readerFor(identity *auth.Identity) func(resource string) bool {
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
		cluster.ServiceRefACLResource,
	)
}

// filterRecommendations mirrors api/recommendation_handlers.go.
func (s *Server) filterRecommendations(
	ctx context.Context,
	items []recommendations.Recommendation,
) []recommendations.Recommendation {
	return cluster.ReadableRecommendations(s.acl, auth.IdentityFromContext(ctx), items)
}

// readableEnrichedTasks filters tasks to the ones the caller may read, then
// names their parents from the services and nodes they may read too, so a
// bare `task:*` grant names no service. Filtering both lists once beats a
// per-task check when the page holds many tasks.
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

// checkResolvedWrite enforces write on the record arg resolved to, by exact
// ID or name. Docker also takes an ID prefix, which the ACL would have checked
// as written, so an argument the cache does not resolve is refused as missing,
// as is one the caller cannot read, which a denial would otherwise name.
func (s *Server) checkResolvedWrite(
	ctx context.Context,
	kind, arg string,
	record cluster.Record,
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
		if s.acl.Can(identity, "read", kind+":"+arg) {
			return err
		}
		found = false
	}

	resource := record.WriteResource()
	if !found || !s.acl.Can(identity, "read", resource) {
		return fmt.Errorf("%s %q not found", kind, arg)
	}
	if !s.acl.Can(identity, "write", resource) {
		return fmt.Errorf("write access denied for %s", resource)
	}

	return nil
}

// resolveWrite is checkResolvedWrite returning the ID arg resolved to, so the
// write names the object its history event will; an argument the cache could
// not resolve passes through for Docker to judge.
func (s *Server) resolveWrite(kind string) func(ctx context.Context, arg string) (string, error) {
	return func(ctx context.Context, arg string) (string, error) {
		record, found, err := cluster.Lookup(s.cache, kind, arg)
		if err := s.checkResolvedWrite(ctx, kind, arg, record, found, err); err != nil {
			return "", err
		}
		if !found {
			return arg, nil
		}

		return record.ID, nil
	}
}

// checkServiceWrite is resolveWrite for a service write that keeps the
// caller's identifier.
func (s *Server) checkServiceWrite(ctx context.Context, id string) error {
	_, err := s.resolveWrite("service")(ctx, id)

	return err
}

// resolveTaskWrite resolves id to a task the caller may name, then checks the
// write under the key cluster.TaskRecord gives it.
func (s *Server) resolveTaskWrite(ctx context.Context, id string) (string, error) {
	task, err := s.resolveTaskArg(ctx, id)
	if err != nil {
		return "", err
	}

	record := cluster.TaskRecord(s.cache, task)

	return task.ID, s.checkResolvedWrite(ctx, "task", id, record, true, nil)
}

// resolveTaskArg resolves a task ID or "<service>.<slot>" through the cache.
// Docker would read an unresolved argument as an ID prefix the ACL never
// checked, so one is refused with or without a policy.
func (s *Server) resolveTaskArg(ctx context.Context, arg string) (swarm.Task, error) {
	task, ok, err := s.resolveTaskAs(auth.IdentityFromContext(ctx), arg)
	if err != nil && s.checkRead(ctx, "task", arg) == nil {
		return swarm.Task{}, err
	}

	if err != nil || !ok {
		return swarm.Task{}, fmt.Errorf("task %q not found", arg)
	}

	return task, nil
}

// resolveTaskAs resolves a task ID or "<service>.<slot>" for identity, by the
// rule every transport resolving a task argument shares.
func (s *Server) resolveTaskAs(identity *auth.Identity, arg string) (swarm.Task, bool, error) {
	return cluster.ResolveTaskAs(s.cache, arg, s.readerFor(identity))
}

// resolveTaskRead is resolveTaskArg for a read of the task it names.
func (s *Server) resolveTaskRead(ctx context.Context, arg string) (swarm.Task, error) {
	task, err := s.resolveTaskArg(ctx, arg)
	if err != nil {
		return swarm.Task{}, err
	}

	if err := s.checkTaskRead(ctx, task.ID); err != nil {
		return swarm.Task{}, fmt.Errorf("task %q not found", arg)
	}

	return task, nil
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
// It does not walk to the parent the way resolveTaskWrite does, because
// acl.grantMatchesResource already resolves a task through its service and
// stack — and every other task read passes this same key.
func (s *Server) checkTaskRead(ctx context.Context, id string) error {
	if s.checkRead(ctx, "task", id) != nil {
		return fmt.Errorf("task %q not found", id)
	}
	return nil
}
