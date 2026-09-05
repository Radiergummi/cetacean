package mcp

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// describableResourceTypes maps the singular type name describe takes to the
// plural key the cetacean:// resource tree uses.
//
// It is pluralToSingularRowType read the other way, derived rather than
// declared a second time: a compact result names its type the way
// cluster.Digest and cluster.Row do (singular, following TopologyNode), while
// the resource URIs it delegates to are plural, and two literals would let a
// type become findable under one spelling and describable under neither.
var describableResourceTypes = invertResourceTypes(pluralToSingularRowType)

// invertResourceTypes swaps a plural→singular resource-type map end for end.
func invertResourceTypes(pluralToSingular map[string]string) map[string]string {
	out := make(map[string]string, len(pluralToSingular))

	for plural, singular := range pluralToSingular {
		out[singular] = plural
	}

	return out
}

// describableTypeNames lists the accepted singular type names, sorted, for the
// error a caller sees when they name something else. Ranging over the map
// directly would put a different order in the message every call.
func describableTypeNames() []string {
	return slices.Sorted(maps.Keys(describableResourceTypes))
}

// toolDescribe returns one resource as a cluster.Digest: its derived state,
// the reason behind that state, and the resources it references — the answers
// a caller would otherwise need several reads and a raw Docker object to
// assemble.
//
// Identity resolution and the ACL check are delegated to lookupResource, the
// same audited path the cetacean:// resources and find use, rather than
// reading the cache here.
func (s *Server) toolDescribe(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	resourceType := strings.TrimSpace(req.GetString("type", ""))
	if resourceType == "" {
		return "", fmt.Errorf("`type` is required; expected one of %v", describableTypeNames())
	}

	plural, ok := describableResourceTypes[resourceType]
	if !ok {
		return "", fmt.Errorf(
			"unknown resource type %q; expected one of %v",
			resourceType, describableTypeNames(),
		)
	}

	id := strings.TrimSpace(req.GetString("id", ""))
	if id == "" {
		return "", fmt.Errorf("`id` is required")
	}

	// The identifier is spliced into a cetacean:// path, so a separator in it
	// would address something other than the resource the caller named — the
	// service log stream, say, whose shape is not a Digest at all.
	if strings.Contains(id, "/") {
		return "", fmt.Errorf("`id` must name a single resource, not a path: %q", id)
	}

	resolved, err := s.lookupResource(ctx, "cetacean://"+plural+"/"+id)
	if err != nil {
		return "", err
	}

	if req.GetBool("raw", false) {
		// The untouched Docker record is not the shape describe's outputSchema
		// describes (that is cluster.Digest), and mcp-go validates results
		// against it — presenting this as structuredContent would fail the
		// very call that asked for the raw record.
		markTextOnlyResult(ctx)

		return marshalResult(resolved)
	}

	digest, err := s.digestOf(ctx, plural, resolved)
	if err != nil {
		return "", err
	}

	// The digest names the resources this one references; the links turn those
	// names into somewhere the client can go, which is the traversal describe
	// exists to enable.
	attachResourceLinks(ctx, resourceLinksForDigest(digest))

	return marshalResult(digest)
}

// digestOf builds the detail view of one already-resolved resource, and is the
// single place both transports go through: describe calls it, and so does the
// templated cetacean:// resource read, so a tool result and a subscription
// payload cannot describe the same resource differently.
//
// Every slice handed to a builder is filtered to what the caller may read
// first. A digest resolves IDs to names — a service's network attachments, a
// node's workload, the services mounting a config — and an unfiltered slice
// would turn it into a side channel for resources the caller cannot list. Each
// builder falls back to the bare ID when a referenced resource is absent from
// the slice it was given, which is what makes the filtering safe rather than
// lossy.
func (s *Server) digestOf(
	ctx context.Context,
	resourceType string,
	resolved any,
) (cluster.Digest, error) {
	switch resource := resolved.(type) {
	case swarm.Service:
		return cluster.ServiceDigest(
			resource,
			s.filterRawTasks(ctx, s.cache.ListTasksByService(resource.ID)),
			s.readableAttachedNetworks(ctx, resource),
		), nil

	case swarm.Node:
		tasks := s.filterRawTasks(ctx, s.cache.ListTasksByNode(resource.ID))

		return cluster.NodeDigest(resource, tasks, s.readableServicesOf(ctx, tasks)), nil

	case cluster.EnrichedTask:
		return cluster.TaskDigest(
			resource.Task,
			s.readableService(ctx, resource.ServiceID),
			s.readableNode(ctx, resource.NodeID),
		), nil

	case cache.StackDetail:
		// The member records come through as the stack resource resolved them:
		// an ACL stack grant reaches its member types by definition
		// (acl.impliedTypes), and dropping members here would report a stack
		// as healthy because the service that is failing was filtered out.
		var tasks []swarm.Task
		for _, svc := range resource.Services {
			tasks = append(tasks, s.cache.ListTasksByService(svc.ID)...)
		}

		return cluster.StackDigest(resource, s.filterRawTasks(ctx, tasks)), nil

	case swarm.Config:
		return cluster.ConfigDigest(
			resource,
			s.filterServiceRefs(ctx, s.cache.ServicesUsingConfig(resource.ID)),
		), nil

	case swarm.Secret:
		return cluster.SecretDigest(
			resource,
			s.filterServiceRefs(ctx, s.cache.ServicesUsingSecret(resource.ID)),
		), nil

	case network.Summary:
		return cluster.NetworkDigest(
			resource,
			s.filterServiceRefs(ctx, s.cache.ServicesUsingNetwork(resource.ID)),
		), nil

	case volume.Volume:
		return cluster.VolumeDigest(
			resource,
			s.filterServiceRefs(ctx, s.cache.ServicesUsingVolume(resource.Name)),
		), nil

	default:
		// Unreachable while describableResourceTypes and lookupResource agree,
		// but named rather than returning an empty digest if they ever drift.
		return cluster.Digest{}, fmt.Errorf(
			"resource type %q resolved to %T, which has no digest builder",
			resourceType, resolved,
		)
	}
}

// readableAttachedNetworks resolves just the networks a service attaches to,
// dropping any the caller may not read.
//
// ServiceDigest resolves attachment IDs to names for its related entries, so
// it needs the networks named — but only the one to three a service actually
// attaches to. Listing and ACL-filtering every network in the cluster to
// resolve those few is work repaid on every resources/read, which a
// subscription re-drives after each cache event. A network left out because
// the caller may not read it is not an error: the builder falls back to the
// ID, which is what keeps an unreadable name out of the digest.
func (s *Server) readableAttachedNetworks(
	ctx context.Context,
	svc swarm.Service,
) []network.Summary {
	attachments := svc.Spec.TaskTemplate.Networks
	if len(attachments) == 0 {
		return nil
	}

	networks := make([]network.Summary, 0, len(attachments))

	for _, attachment := range attachments {
		net, ok := s.cache.GetNetwork(attachment.Target)
		if !ok {
			continue
		}

		if s.checkRead(ctx, "network", net.Name) != nil {
			continue
		}

		networks = append(networks, net)
	}

	return networks
}

// readableServicesOf resolves the services these tasks belong to, dropping any
// the caller may not read.
//
// NodeDigest needs the names of the services with tasks on this one node —
// a handful — so resolving those by ID beats listing and ACL-filtering every
// service in the cluster on a path a subscription re-drives per cache event.
func (s *Server) readableServicesOf(
	ctx context.Context,
	tasks []swarm.Task,
) []swarm.Service {
	seen := make(map[string]bool, len(tasks))
	services := make([]swarm.Service, 0, len(tasks))

	for _, task := range tasks {
		if task.ServiceID == "" || seen[task.ServiceID] {
			continue
		}

		seen[task.ServiceID] = true

		if svc := s.readableService(ctx, task.ServiceID); svc != nil {
			services = append(services, *svc)
		}
	}

	return services
}

// readableService resolves a task's parent service, or nil when the caller may
// not read it. TaskDigest takes a pointer precisely so this can be nil: the
// digest then names the parent by the ID the task record itself carries,
// rather than disclosing a name from behind the caller's grants or failing a
// read the caller was entitled to.
func (s *Server) readableService(ctx context.Context, serviceID string) *swarm.Service {
	if serviceID == "" {
		return nil
	}

	svc, ok := s.cache.GetService(serviceID)
	if !ok {
		return nil
	}

	if s.checkRead(ctx, "service", svc.Spec.Name) != nil {
		return nil
	}

	return &svc
}

// readableNode resolves the node a task is assigned to, or nil when the caller
// may not read it — see readableService.
func (s *Server) readableNode(ctx context.Context, nodeID string) *swarm.Node {
	if nodeID == "" {
		return nil
	}

	node, ok := s.cache.GetNode(nodeID)
	if !ok {
		return nil
	}

	if s.checkRead(ctx, "node", nodeACLName(node)) != nil {
		return nil
	}

	return &node
}

// digestibleResourceType reports the plural resource type of a cetacean:// URI
// that addresses exactly one resource of a type Digest covers, so the
// templated resource reads can serve the digest and everything else — the
// listings, the singletons, the service log stream — stays as it was.
func digestibleResourceType(uri string) (string, bool) {
	path, ok := strings.CutPrefix(uri, "cetacean://")
	if !ok {
		return "", false
	}

	// Exactly two segments: a type and one identifier. Three would be the log
	// stream, whose payload is a log response rather than a digest.
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] == "" {
		return "", false
	}

	// Gate on the same map describe resolves its `type` against, not on
	// listableResourceTypes: a type added to the listing without a digest
	// builder would otherwise be accepted here and then fall to digestOf's
	// default branch, turning a working resource read into an error.
	if _, ok := pluralToSingularRowType[parts[0]]; !ok {
		return "", false
	}

	return parts[0], true
}
