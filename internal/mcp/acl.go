package mcp

import (
	"context"
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

// searchACLPrefix maps cluster.Search's plural type keys to the ACL resource
// prefix. Kept aligned with api/search_handlers.go so the two transports apply
// the same ACL rules to the same hits.
var searchACLPrefix = map[string]string{
	"services": "service:",
	"stacks":   "stack:",
	"nodes":    "node:",
	"tasks":    "task:",
	"configs":  "config:",
	"secrets":  "secret:",
	"networks": "network:",
	"volumes":  "volume:",
}

// searchResultACLResource returns the ACL resource string for a search hit.
// Tasks key on the task ID; every other type keys on the resource name.
func searchResultACLResource(resourceType string, sr cluster.SearchResult) string {
	prefix := searchACLPrefix[resourceType]
	if resourceType == "tasks" {
		return prefix + sr.ID
	}
	return prefix + sr.Name
}

// filterSearchResults runs cluster.Search through ACL. The returned
// SearchResults reflect post-filter Hits and per-type Counts (adjusted by the
// observed denial rate, matching REST behaviour).
func (s *Server) filterSearchResults(
	ctx context.Context,
	raw cluster.SearchResults,
) cluster.SearchResults {
	if s.acl == nil {
		return raw
	}
	identity := auth.IdentityFromContext(ctx)
	out := cluster.SearchResults{
		Hits:   make(map[string][]cluster.SearchResult, len(raw.Hits)),
		Counts: make(map[string]int, len(raw.Counts)),
	}
	for resourceType, count := range raw.Counts {
		hits := raw.Hits[resourceType]
		filtered := acl.Filter(s.acl, identity, "read", hits, func(sr cluster.SearchResult) string {
			return searchResultACLResource(resourceType, sr)
		})
		// Adjust the pre-cap count by the number of visible-page denials. We
		// assume the visible-page ACL rate generalizes to the pre-cap set; this
		// is the same approximation api/search_handlers.go uses.
		removed := len(hits) - len(filtered)
		visible := count - removed
		if visible <= 0 {
			continue
		}
		out.Hits[resourceType] = filtered
		out.Counts[resourceType] = visible
		out.Total += visible
	}
	return out
}

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

// nodeACLName returns the ACL-friendly node name (hostname, falling back to ID).
// Matches the convention used by REST's nodeHostnameOrID.
func nodeACLName(n swarm.Node) string {
	if h := n.Description.Hostname; h != "" {
		return h
	}
	return n.ID
}

func (s *Server) filterNodes(ctx context.Context, items []swarm.Node) []swarm.Node {
	return acl.Filter(
		s.acl,
		auth.IdentityFromContext(ctx),
		"read",
		items,
		func(n swarm.Node) string {
			return "node:" + nodeACLName(n)
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
//
// The ACL key is "task:<id>", a field swarm.Task already carries, so nothing
// has to be enriched to make the decision — and the digest and row builders
// resolve parent names from the slices they are given rather than from
// enriched fields. Enriching first would resolve a service name and a node
// hostname per task only to discard them, on a path (resources/read) that a
// subscription re-drives after every cache event, and would resolve them from
// behind the caller's grants besides.
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

// filterServiceRefs drops the services an identity may not read from one of
// the cache's reverse indexes (ServicesUsingConfig and friends). A digest's
// "used by" list is built from those, and would otherwise name services the
// caller cannot see just because they can read the config those services
// mount.
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

// filterHistory mirrors api/history_handlers.go: drop entries whose resource
// the identity can't read.
func (s *Server) filterHistory(
	ctx context.Context,
	entries []cache.HistoryEntry,
) []cache.HistoryEntry {
	if s.acl == nil {
		return entries
	}
	identity := auth.IdentityFromContext(ctx)
	filtered := entries[:0]
	for _, e := range entries {
		if s.acl.Can(identity, "read", string(e.Type)+":"+e.Name) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// readableEnrichedTasks filters tasks to the ones the caller may read, then
// names their parents from the services and nodes the caller may read too.
//
// The enrichment has to be ACL-aware because ServiceName and NodeHostname are
// the enriched task's whole point, and cluster.EnrichTasks resolves them
// straight out of the cache: a caller holding a bare `task:*` grant was told
// the name of every service and node in the cluster, which is exactly what
// TaskDigest goes out of its way not to do. Filtering the tasks first also
// keeps the work proportional to what is actually returned.
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
// the identity in ctx. Returns nil if ACL is disabled or no identity is on the
// context (the bearer middleware would have rejected the request earlier in
// that case). All MCP tool handlers route through this helper so denial errors
// have a uniform shape.
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

// checkServiceWrite resolves the service name from the cache so the ACL key
// is `service:<name>` rather than `service:<id>`, matching REST behaviour.
func (s *Server) checkServiceWrite(ctx context.Context, id string) error {
	name := id
	if svc, ok := s.cache.GetService(id); ok && svc.Spec.Name != "" {
		name = svc.Spec.Name
	}
	return s.checkWrite(ctx, "service", name)
}

// checkNodeWrite resolves the node hostname from the cache so the ACL key is
// `node:<hostname>` rather than `node:<id>`, matching REST behaviour.
func (s *Server) checkNodeWrite(ctx context.Context, id string) error {
	name := id
	if node, ok := s.cache.GetNode(id); ok && node.Description.Hostname != "" {
		name = node.Description.Hostname
	}
	return s.checkWrite(ctx, "node", name)
}

// checkConfigWrite resolves the config Spec.Name from the cache so the ACL key
// is `config:<name>` rather than `config:<id>`, matching REST behaviour.
func (s *Server) checkConfigWrite(ctx context.Context, id string) error {
	name := id
	if cfg, ok := s.cache.GetConfig(id); ok && cfg.Spec.Name != "" {
		name = cfg.Spec.Name
	}
	return s.checkWrite(ctx, "config", name)
}

// checkSecretWrite resolves the secret Spec.Name from the cache so the ACL key
// is `secret:<name>` rather than `secret:<id>`, matching REST behaviour.
func (s *Server) checkSecretWrite(ctx context.Context, id string) error {
	name := id
	if sec, ok := s.cache.GetSecret(id); ok && sec.Spec.Name != "" {
		name = sec.Spec.Name
	}
	return s.checkWrite(ctx, "secret", name)
}

// checkNetworkWrite resolves the network Name from the cache so the ACL key is
// `network:<name>` rather than `network:<id>`, matching REST behaviour.
func (s *Server) checkNetworkWrite(ctx context.Context, id string) error {
	name := id
	if net, ok := s.cache.GetNetwork(id); ok && net.Name != "" {
		name = net.Name
	}
	return s.checkWrite(ctx, "network", name)
}

// checkTaskWrite routes the ACL check to the task's parent service so an MCP
// `remove_task` evaluates the same key REST DELETE /tasks/{id} does
// (`service:<name>`). Falls back to `task:<id>` when the cache cannot resolve
// the parent service — that path is also what REST takes as a last resort.
func (s *Server) checkTaskWrite(ctx context.Context, id string) error {
	task, ok := s.cache.GetTask(id)
	if !ok {
		return s.checkWrite(ctx, "task", id)
	}
	svc, ok := s.cache.GetService(task.ServiceID)
	if !ok || svc.Spec.Name == "" {
		return s.checkWrite(ctx, "task", id)
	}
	return s.checkWrite(ctx, "service", svc.Spec.Name)
}

// checkServiceRead resolves the service Spec.Name from the cache so the ACL
// key is `service:<name>` rather than `service:<id>`, matching the resource
// read path in lookupResource. Used by toolGetLogs which doesn't go through
// lookupResource.
//
// It resolves rather than looking the ID up, because get_logs advertises its
// `service` argument as "Service ID or name" and Docker honours both — so a
// plain GetService turned a name, the identifier find and the completions
// hand back, into "service not found" on every cluster that has an ACL policy
// and into a working call on every cluster that has not.
func (s *Server) checkServiceRead(ctx context.Context, id string) error {
	if s.acl == nil {
		return nil
	}
	svc, ok, err := s.cache.ResolveService(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("service %q not found", id)
	}
	return s.checkRead(ctx, "service", svc.Spec.Name)
}

// checkTaskRead enforces the read permission on a task, keyed as `task:<id>`.
//
// It does not walk to the parent service the way checkTaskWrite does, because
// the evaluator already does: acl.grantMatchesResource resolves a task through
// its parent service and that service's stack, so the task's own key is
// strictly the broader check. It is also the key every other task read passes
// — REST's HandleTaskLogs and the cetacean://tasks/{id} resource both do — and
// walking to the parent here would leave a `task:*` grant able to read task
// logs on every path except this tool.
func (s *Server) checkTaskRead(ctx context.Context, id string) error {
	return s.checkRead(ctx, "task", id)
}
