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

// resourceTemplate describes a templated cetacean:// resource. The URI template
// follows RFC 6570 — mcp-go parses it and dispatches reads through the matching
// handler.
type resourceTemplate struct {
	uri         string
	name        string
	description string
}

// staticResource describes a non-templated cetacean:// resource that exists in
// a single instance (cluster, recommendations, history).
type staticResource struct {
	uri         string
	name        string
	description string
}

var (
	staticResources = []staticResource{
		{uri: "cetacean://cluster", name: "Cluster", description: "Swarm cluster info"},
		{
			uri:         "cetacean://recommendations",
			name:        "Recommendations",
			description: "Current cluster recommendations",
		},
		{uri: "cetacean://history", name: "History", description: "Recent change history"},
	}

	resourceTemplates = []resourceTemplate{
		{uri: "cetacean://nodes/{id}", name: "Node", description: "Node detail"},
		{uri: "cetacean://services/{id}", name: "Service", description: "Service detail"},
		{
			uri:         "cetacean://services/{id}/logs",
			name:        "Service Logs",
			description: "Service log stream",
		},
		{uri: "cetacean://tasks/{id}", name: "Task", description: "Task detail"},
		{uri: "cetacean://stacks/{name}", name: "Stack", description: "Stack detail"},
		{uri: "cetacean://configs/{id}", name: "Config", description: "Config detail"},
		{
			uri:         "cetacean://secrets/{id}",
			name:        "Secret",
			description: "Secret metadata (data redacted)",
		},
		{uri: "cetacean://networks/{id}", name: "Network", description: "Network detail"},
		{uri: "cetacean://volumes/{name}", name: "Volume", description: "Volume detail"},
	}
)

func (s *Server) registerResources() {
	for _, r := range staticResources {
		s.mcpServer.AddResource(
			mcplib.NewResource(r.uri, r.name,
				mcplib.WithResourceDescription(r.description),
				mcplib.WithMIMEType(mcpMIMEType),
			),
			s.handleReadResource,
		)
	}

	for _, t := range resourceTemplates {
		s.mcpServer.AddResourceTemplate(
			mcplib.NewResourceTemplate(t.uri, t.name,
				mcplib.WithTemplateDescription(t.description),
				mcplib.WithTemplateMIMEType(mcpMIMEType),
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
	path := strings.TrimPrefix(uri, "cetacean://")
	if path == uri || path == "" {
		return "", fmt.Errorf("invalid resource URI: %s", uri)
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

	data, err := s.lookupResource(ctx, uri, resourceType, resourceID, subResource)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal resource: %w", err)
	}
	return string(b), nil
}

// lookupResource resolves the cache slice for a parsed URI. It returns an error
// when the requested ID is unknown so handleReadResource can surface a
// "resource not found" JSON-RPC error rather than serializing a zero value.
//
// ACL enforcement is deferred to Task 11 — the identity is already on the
// context (see WithHTTPContextFunc) and individual handlers will filter list
// reads / refuse detail reads once the policy plumbing lands.
func (s *Server) lookupResource(
	ctx context.Context,
	uri, resourceType, resourceID, subResource string,
) (any, error) {
	switch resourceType {
	case "cluster":
		return s.cache.Snapshot(), nil

	case "recommendations":
		if s.recEngine == nil {
			return []any{}, nil
		}
		return s.recEngine.Results(), nil

	case "history":
		return s.cache.History().List(cache.HistoryQuery{Limit: 100}), nil

	case "nodes":
		if resourceID == "" {
			return s.cache.ListNodes(), nil
		}
		node, ok := s.cache.GetNode(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		return node, nil

	case "services":
		if resourceID == "" {
			return s.cache.ListServices(), nil
		}
		if subResource == "logs" {
			return s.readServiceLogs(ctx, resourceID)
		}
		svc, ok := s.cache.GetService(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		return svc, nil

	case "tasks":
		if resourceID == "" {
			return cluster.EnrichTasks(s.cache, s.cache.ListTasks()), nil
		}
		task, ok := s.cache.GetTask(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		return cluster.EnrichTask(s.cache, task), nil

	case "stacks":
		if resourceID == "" {
			return s.cache.ListStacks(), nil
		}
		stack, ok := s.cache.GetStackDetail(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		return stack, nil

	case "configs":
		if resourceID == "" {
			return s.cache.ListConfigs(), nil
		}
		cfg, ok := s.cache.GetConfig(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		return cfg, nil

	case "secrets":
		if resourceID == "" {
			return cluster.RedactSecrets(s.cache.ListSecrets()), nil
		}
		sec, ok := s.cache.GetSecret(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		return cluster.RedactSecret(sec), nil

	case "networks":
		if resourceID == "" {
			return s.cache.ListNetworks(), nil
		}
		net, ok := s.cache.GetNetwork(resourceID)
		if !ok {
			return nil, notFound(uri)
		}
		return net, nil

	case "volumes":
		if resourceID == "" {
			return s.cache.ListVolumes(), nil
		}
		vol, ok := s.cache.GetVolume(resourceID)
		if !ok {
			return nil, notFound(uri)
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
