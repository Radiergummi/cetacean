package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

const mcpMIMEType = "application/json"

// namedResource describes a cetacean:// resource — either a fixed singleton
// (cluster, recommendations, history) or an RFC 6570 URI template that mcp-go
// dispatches via its template router. `name` is the programmatic MCP resource
// name (stable, used by clients as a key); `title` is the human-readable label
// MCP clients prefer for display.
type namedResource struct {
	uri         string
	name        string
	title       string
	description string
}

var (
	staticResources = []namedResource{
		{
			uri:         "cetacean://cluster",
			name:        "cluster",
			title:       "Swarm cluster overview",
			description: "High-level cluster facts: swarm ID, raft state, CA config, orchestration defaults, manager and worker counts, installed plugins. Returns a single JSON document; updates push via notifications/resources/updated when any of those fields change.",
		},
		{
			uri:         "cetacean://recommendations",
			name:        "recommendations",
			title:       "Cluster recommendations",
			description: "Currently surfaced recommendations from the built-in engine: right-sizing, missing health checks or restart policies, flaky services, single-replica risks, manager workload imbalance, uneven node distribution. Each entry includes severity, category, target resource, and rationale.",
		},
		{
			uri:         "cetacean://history",
			name:        "history",
			title:       "Recent change history",
			description: "Ring buffer (up to 10,000 entries) of recent create/update/delete events across every cluster resource type, newest first. Useful for incident triage and 'what changed' questions.",
		},
	}

	resourceTemplates = []namedResource{
		{
			uri:         "cetacean://nodes/{id}",
			name:        "node",
			title:       "Node detail",
			description: "Full node detail by ID or hostname: role, availability, engine version, platform, labels, manager status, resource totals, and the services currently scheduled onto it.",
		},
		{
			uri:         "cetacean://services/{id}",
			name:        "service",
			title:       "Service detail",
			description: "Full service detail by ID or name: spec (image, env, labels, resources, placement, ports, update/rollback policy, log driver), running and desired task counts, derived state, and the configs/secrets/networks/volumes it references.",
		},
		{
			uri:         "cetacean://services/{id}/logs",
			name:        "service_logs",
			title:       "Service logs (streaming)",
			description: "Live log stream for a service, merged across replicas. Subscribe via resources/subscribe to receive new log lines as notifications/resources/updated events. For a one-shot fetch of recent lines use the get_logs tool.",
		},
		{
			uri:         "cetacean://tasks/{id}",
			name:        "task",
			title:       "Task detail",
			description: "Full task detail by ID, enriched with the parent service name and assigned node hostname. Includes desired and current state, status timestamps, the container spec the task ran from, and any failure messages.",
		},
		{
			uri:         "cetacean://stacks/{name}",
			name:        "stack",
			title:       "Stack detail",
			description: "Stack detail by name. Stacks are derived from the com.docker.stack.namespace label, so this rolls up every service, network, config, and secret tagged with the stack name. Secret payloads are redacted.",
		},
		{
			uri:         "cetacean://configs/{id}",
			name:        "config",
			title:       "Config detail",
			description: "Full Docker config by ID: name, labels, creation/update timestamps, and the base64-encoded data payload.",
		},
		{
			uri:         "cetacean://secrets/{id}",
			name:        "secret",
			title:       "Secret metadata (data redacted)",
			description: "Docker secret metadata by ID: name, labels, driver, creation/update timestamps. The data field is always zeroed before serialization — secret payloads are never exposed through MCP.",
		},
		{
			uri:         "cetacean://networks/{id}",
			name:        "network",
			title:       "Network detail",
			description: "Overlay network detail by ID: driver, scope, IPAM configuration, attachable flag, ingress flag, labels, and the services currently attached.",
		},
		{
			uri:         "cetacean://volumes/{name}",
			name:        "volume",
			title:       "Volume detail",
			description: "Volume detail by name (volumes are keyed by name, not ID): driver, mountpoint, options, labels, scope, and the services that mount it.",
		},
	}
)

func (s *Server) registerResources() {
	// Each resource's icon file is named after its MCP resource name, served
	// under the un-authed /assets/mcp-icons/resources/ prefix. icon() returns
	// nil when no external base URL is configured, which NewResource treats as
	// "no icons".
	for _, r := range staticResources {
		s.mcpServer.AddResource(
			mcplib.NewResource(r.uri, r.name,
				mcplib.WithResourceTitle(r.title),
				mcplib.WithResourceDescription(r.description),
				mcplib.WithMIMEType(mcpMIMEType),
				mcplib.WithResourceIcons(s.icon("resources", r.name)...),
			),
			s.handleReadResource,
		)
	}

	for _, t := range resourceTemplates {
		s.mcpServer.AddResourceTemplate(
			mcplib.NewResourceTemplate(t.uri, t.name,
				mcplib.WithTemplateTitle(t.title),
				mcplib.WithTemplateDescription(t.description),
				mcplib.WithTemplateMIMEType(mcpMIMEType),
				mcplib.WithTemplateIcons(s.icon("resources", t.name)...),
			),
			s.handleReadResource,
		)
	}
}

// handleReadResource implements mcp-go's ResourceHandlerFunc /
// ResourceTemplateHandlerFunc shape. It parses the cetacean:// URI, reads the
// matching cache slice (with redaction and enrichment), and returns a single
// TextResourceContents entry.
func (s *Server) handleReadResource(
	ctx context.Context,
	req mcplib.ReadResourceRequest,
) ([]mcplib.ResourceContents, error) {
	uri := req.Params.URI
	body, err := s.readResource(ctx, uri)
	if err != nil {
		return nil, err
	}
	return []mcplib.ResourceContents{
		mcplib.TextResourceContents{
			URI:      uri,
			MIMEType: mcpMIMEType,
			Text:     body,
		},
	}, nil
}

// readResource is the URI-dispatch core, separated from handleReadResource so
// tests can drive it without constructing an mcp-go request.
func (s *Server) readResource(ctx context.Context, uri string) (string, error) {
	data, err := s.lookupResource(ctx, uri)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal resource: %w", err)
	}
	return string(b), nil
}

// lookupResource parses the cetacean:// URI and resolves the cache slice it
// addresses. Returns an error when the requested ID is unknown so
// handleReadResource can surface a "resource not found" JSON-RPC error rather
// than serializing a zero value.
func (s *Server) lookupResource(ctx context.Context, uri string) (any, error) {
	path := strings.TrimPrefix(uri, "cetacean://")
	if path == uri || path == "" {
		return nil, fmt.Errorf("invalid resource URI: %s", uri)
	}

	parts := strings.SplitN(path, "/", 3)
	resourceType := parts[0]
	resourceID := ""
	if len(parts) > 1 {
		resourceID = parts[1]
	}
	subResource := ""
	if len(parts) > 2 {
		subResource = parts[2]
	}

	switch resourceType {
	case "cluster":
		return s.cache.Snapshot(), nil

	case "recommendations":
		if s.recEngine == nil {
			return []any{}, nil
		}
		return s.filterRecommendations(ctx, s.recEngine.Results()), nil

	case "history":
		return s.filterHistory(ctx, s.cache.History().List(cache.HistoryQuery{Limit: 100})), nil

	case "nodes":
		if resourceID == "" {
			return s.filterNodes(ctx, s.cache.ListNodes()), nil
		}
		node, ok := s.cache.GetNode(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "node", nodeACLName(node)); err != nil {
			return nil, err
		}
		return node, nil

	case "services":
		if resourceID == "" {
			return s.filterServices(ctx, s.cache.ListServices()), nil
		}
		svc, ok := s.cache.GetService(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "service", svc.Spec.Name); err != nil {
			return nil, err
		}
		if subResource == "logs" {
			return s.readServiceLogs(ctx, resourceID)
		}
		return svc, nil

	case "tasks":
		if resourceID == "" {
			return s.filterTasks(ctx, cluster.EnrichTasks(s.cache, s.cache.ListTasks())), nil
		}
		task, ok := s.cache.GetTask(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "task", task.ID); err != nil {
			return nil, err
		}
		return cluster.EnrichTask(s.cache, task), nil

	case "stacks":
		if resourceID == "" {
			return s.filterStacks(ctx, s.cache.ListStacks()), nil
		}
		// Look up first, then ACL — keeps "exists but denied" indistinguishable
		// from "not found" so callers can't probe stack existence via error
		// classes. Matches the order used for every other resource type.
		stack, ok := s.cache.GetStackDetail(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "stack", resourceID); err != nil {
			return nil, err
		}
		return stack, nil

	case "configs":
		if resourceID == "" {
			return s.filterConfigs(ctx, s.cache.ListConfigs()), nil
		}
		cfg, ok := s.cache.GetConfig(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "config", cfg.Spec.Name); err != nil {
			return nil, err
		}
		return cfg, nil

	case "secrets":
		if resourceID == "" {
			return cluster.RedactSecrets(s.filterSecrets(ctx, s.cache.ListSecrets())), nil
		}
		sec, ok := s.cache.GetSecret(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "secret", sec.Spec.Name); err != nil {
			return nil, err
		}
		return cluster.RedactSecret(sec), nil

	case "networks":
		if resourceID == "" {
			return s.filterNetworks(ctx, s.cache.ListNetworks()), nil
		}
		net, ok := s.cache.GetNetwork(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "network", net.Name); err != nil {
			return nil, err
		}
		return net, nil

	case "volumes":
		if resourceID == "" {
			return s.filterVolumes(ctx, s.cache.ListVolumes()), nil
		}
		// Look up first, then ACL — see the "stacks" case for rationale.
		vol, ok := s.cache.GetVolume(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		if err := s.checkRead(ctx, "volume", resourceID); err != nil {
			return nil, err
		}
		return vol, nil

	default:
		return nil, fmt.Errorf("unknown resource type: %s", resourceType)
	}
}

// readServiceLogs returns a LogResourceResponse for a single service. The
// resource read uses default options (most recent 100 lines, no level filter);
// callers wanting cursored pagination use the get_logs tool which accepts
// `since` and `tail` arguments.
func (s *Server) readServiceLogs(ctx context.Context, serviceID string) (any, error) {
	resp, err := s.readServiceLogsImpl(ctx, serviceID, logOptions{tail: defaultLogTail})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func notFound(uri string) error {
	return fmt.Errorf("resource not found: %s", uri)
}
