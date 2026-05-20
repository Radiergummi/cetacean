package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// ServiceLifecycleWriter is the subset of Docker service-lifecycle operations
// exposed via Tier 1 (Operational) MCP tools.
type ServiceLifecycleWriter interface {
	ScaleService(ctx context.Context, id string, replicas uint64) (swarm.Service, error)
	UpdateServiceImage(ctx context.Context, id string, image string) (swarm.Service, error)
	RollbackService(ctx context.Context, id string) (swarm.Service, error)
	RestartService(ctx context.Context, id string) (swarm.Service, error)
	RemoveService(ctx context.Context, id string) error
}

// ServiceSpecWriter is the subset of Docker service-spec operations exposed via
// Tier 2 (Configuration) MCP tools.
type ServiceSpecWriter interface {
	UpdateServiceEnv(ctx context.Context, id string, env map[string]string) (swarm.Service, error)
	UpdateServiceLabels(
		ctx context.Context,
		id string,
		labels map[string]string,
	) (swarm.Service, error)
	UpdateServiceResources(
		ctx context.Context,
		id string,
		resources *swarm.ResourceRequirements,
	) (swarm.Service, error)
	UpdateServicePlacement(
		ctx context.Context,
		id string,
		placement *swarm.Placement,
	) (swarm.Service, error)
	UpdateServicePorts(
		ctx context.Context,
		id string,
		ports []swarm.PortConfig,
	) (swarm.Service, error)
	UpdateServiceUpdatePolicy(
		ctx context.Context,
		id string,
		policy *swarm.UpdateConfig,
	) (swarm.Service, error)
	UpdateServiceRollbackPolicy(
		ctx context.Context,
		id string,
		policy *swarm.UpdateConfig,
	) (swarm.Service, error)
	UpdateServiceLogDriver(
		ctx context.Context,
		id string,
		driver *swarm.Driver,
	) (swarm.Service, error)
}

// NodeWriter is the subset of Docker node operations exposed via MCP tools.
// UpdateNodeLabels is Tier 2; UpdateNodeAvailability/UpdateNodeRole are Tier 3.
type NodeWriter interface {
	UpdateNodeAvailability(
		ctx context.Context,
		id string,
		availability swarm.NodeAvailability,
	) (swarm.Node, error)
	UpdateNodeLabels(ctx context.Context, id string, labels map[string]string) (swarm.Node, error)
	UpdateNodeRole(ctx context.Context, id string, role swarm.NodeRole) (swarm.Node, error)
}

// ResourceRemover is the subset of Docker delete operations exposed via Tier 1
// (RemoveTask) and Tier 3 (Remove{Config,Secret,Network,Volume}) MCP tools.
type ResourceRemover interface {
	RemoveTask(ctx context.Context, id string) error
	RemoveConfig(ctx context.Context, id string) error
	RemoveSecret(ctx context.Context, id string) error
	RemoveNetwork(ctx context.Context, id string) error
	RemoveVolume(ctx context.Context, name string, force bool) error
}

// toolDef bundles an mcp-go tool, the operations tier required to expose it,
// and the handler invoked by mcp-go when the tool is called.
type toolDef struct {
	tool    mcplib.Tool
	tier    config.OperationsLevel
	handler func(ctx context.Context, req mcplib.CallToolRequest) (string, error)
}

func (s *Server) registerTools() {
	tools := s.toolCatalog()

	opsLevel := s.config.EffectiveOperationsLevel(s.globalOpsLevel)

	s.registeredTools = make([]toolDef, 0, len(tools))
	for _, td := range tools {
		if opsLevel < td.tier {
			continue
		}
		s.registeredTools = append(s.registeredTools, td)

		handler := td.handler
		s.mcpServer.AddTool(
			td.tool,
			func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
				text, err := handler(ctx, req)
				if err != nil {
					return mcplib.NewToolResultError(err.Error()), nil
				}
				return mcplib.NewToolResultText(text), nil
			},
		)
	}
}

// toolCatalog returns every tool the MCP server knows about. registerTools
// filters this list by tier; per-identity ACL is enforced inside each handler
// at call time.
func (s *Server) toolCatalog() []toolDef {
	return []toolDef{
		// Tier 0 — parameterized reads, no gating.
		{
			tool: mcplib.NewTool(
				"get_logs",
				mcplib.WithDescription("Get recent log lines for a service"),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithString(
					"service",
					mcplib.Required(),
					mcplib.Description("Service ID or name"),
				),
				mcplib.WithNumber("tail", mcplib.Description("Number of lines (default 100)")),
				mcplib.WithString("since", mcplib.Description("Start time (RFC3339)")),
				mcplib.WithString("level", mcplib.Description("Minimum log level")),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetLogs,
		},
		{
			tool: mcplib.NewTool(
				"search",
				mcplib.WithDescription("Search across all cluster resources"),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithString("query", mcplib.Required(), mcplib.Description("Search query")),
				mcplib.WithString(
					"types",
					mcplib.Description("Comma-separated resource types to search"),
				),
				mcplib.WithNumber("limit", mcplib.Description("Max results per type (default 3)")),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolSearch,
		},

		// Tier 1 — Operational.
		{
			tool: mcplib.NewTool(
				"scale_service",
				mcplib.WithDescription("Scale a service to a specific replica count"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithNumber(
					"replicas",
					mcplib.Required(),
					mcplib.Description("Target replica count"),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolScaleService,
		},
		{
			tool: mcplib.NewTool(
				"update_service_image",
				mcplib.WithDescription("Update a service's container image"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithString(
					"image",
					mcplib.Required(),
					mcplib.Description("Image reference"),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolUpdateServiceImage,
		},
		{
			tool: mcplib.NewTool("rollback_service",
				mcplib.WithDescription("Roll a service back to its previous spec"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
			),
			tier:    config.OpsOperational,
			handler: s.toolRollbackService,
		},
		{
			tool: mcplib.NewTool("restart_service",
				mcplib.WithDescription("Force-restart a service"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
			),
			tier:    config.OpsOperational,
			handler: s.toolRestartService,
		},
		{
			tool: mcplib.NewTool("remove_task",
				mcplib.WithDescription("Force-reschedule a task by removing it"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Task ID")),
			),
			tier:    config.OpsOperational,
			handler: s.toolRemoveTask,
		},

		// Tier 2 — Configuration.
		{
			tool: mcplib.NewTool(
				"update_service_env",
				mcplib.WithDescription("Set environment variables on a service (merge patch)"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithObject(
					"env",
					mcplib.Required(),
					mcplib.Description("Environment variable map"),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceEnv,
		},
		{
			tool: mcplib.NewTool("update_service_labels",
				mcplib.WithDescription("Set labels on a service (merge patch)"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithObject("labels", mcplib.Required(), mcplib.Description("Label map")),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceLabels,
		},
		{
			tool: mcplib.NewTool("update_node_labels",
				mcplib.WithDescription("Set labels on a node (merge patch)"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Node ID")),
				mcplib.WithObject("labels", mcplib.Required(), mcplib.Description("Label map")),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateNodeLabels,
		},
		{
			tool: mcplib.NewTool(
				"update_service_resources",
				mcplib.WithDescription("Set CPU/memory limits and reservations on a service"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithObject(
					"resources",
					mcplib.Required(),
					mcplib.Description("ResourceRequirements JSON object"),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceResources,
		},
		{
			tool: mcplib.NewTool(
				"update_service_placement",
				mcplib.WithDescription("Set placement constraints on a service"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithObject(
					"placement",
					mcplib.Required(),
					mcplib.Description("Placement JSON object"),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServicePlacement,
		},
		{
			tool: mcplib.NewTool(
				"update_service_ports",
				mcplib.WithDescription("Set published ports on a service"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithArray(
					"ports",
					mcplib.Required(),
					mcplib.Description("Array of PortConfig objects"),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServicePorts,
		},
		{
			tool: mcplib.NewTool(
				"update_service_update_policy",
				mcplib.WithDescription("Set the update policy on a service"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithObject(
					"policy",
					mcplib.Required(),
					mcplib.Description("UpdateConfig JSON object"),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceUpdatePolicy,
		},
		{
			tool: mcplib.NewTool(
				"update_service_rollback_policy",
				mcplib.WithDescription("Set the rollback policy on a service"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithObject(
					"policy",
					mcplib.Required(),
					mcplib.Description("UpdateConfig JSON object"),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceRollbackPolicy,
		},
		{
			tool: mcplib.NewTool(
				"update_service_log_driver",
				mcplib.WithDescription("Set the log driver on a service"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
				mcplib.WithObject(
					"driver",
					mcplib.Required(),
					mcplib.Description("Driver JSON object (name + options)"),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceLogDriver,
		},

		// Tier 3 — Impactful.
		{
			tool: mcplib.NewTool("update_node_availability",
				mcplib.WithDescription("Set a node's availability (active/pause/drain)"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Node ID")),
				mcplib.WithString("availability", mcplib.Required(),
					mcplib.Description("Availability state"),
					mcplib.Enum("active", "pause", "drain"),
				),
			),
			tier:    config.OpsImpactful,
			handler: s.toolUpdateNodeAvailability,
		},
		{
			tool: mcplib.NewTool("update_node_role",
				mcplib.WithDescription("Promote or demote a node (worker/manager)"),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Node ID")),
				mcplib.WithString("role", mcplib.Required(),
					mcplib.Description("Node role"),
					mcplib.Enum("worker", "manager"),
				),
			),
			tier:    config.OpsImpactful,
			handler: s.toolUpdateNodeRole,
		},
		{
			tool: mcplib.NewTool("remove_service",
				mcplib.WithDescription("Delete a service"),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Service ID")),
			),
			tier:    config.OpsImpactful,
			handler: s.toolRemoveService,
		},
		{
			tool: mcplib.NewTool("remove_config",
				mcplib.WithDescription("Delete a config"),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Config ID")),
			),
			tier:    config.OpsImpactful,
			handler: s.toolRemoveConfig,
		},
		{
			tool: mcplib.NewTool("remove_secret",
				mcplib.WithDescription("Delete a secret"),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Secret ID")),
			),
			tier:    config.OpsImpactful,
			handler: s.toolRemoveSecret,
		},
		{
			tool: mcplib.NewTool("remove_network",
				mcplib.WithDescription("Delete a network"),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithString("id", mcplib.Required(), mcplib.Description("Network ID")),
			),
			tier:    config.OpsImpactful,
			handler: s.toolRemoveNetwork,
		},
		{
			tool: mcplib.NewTool("remove_volume",
				mcplib.WithDescription("Delete a volume"),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithString("name", mcplib.Required(), mcplib.Description("Volume name")),
				mcplib.WithBoolean("force", mcplib.Description("Force removal")),
			),
			tier:    config.OpsImpactful,
			handler: s.toolRemoveVolume,
		},
	}
}

// requireWriteClient returns the write client or an error explaining that
// tools cannot run without one. Wired into every tier 1+ handler so unit
// tests can omit a write client when exercising read tools.
func (s *Server) requireWriteClient() (DockerWriteClient, error) {
	if s.writeClient == nil {
		return nil, fmt.Errorf("MCP server has no write client configured")
	}
	return s.writeClient, nil
}

// checkServiceWrite enforces the "write" permission on a service before the
// tool calls the write client. Looks up the service in the cache so the ACL
// resource is `service:<name>` rather than `service:<id>`, matching what REST
// uses. Returns nil if ACL is disabled (no policy / no identity in context).
func (s *Server) checkServiceWrite(ctx context.Context, id string) error {
	if s.acl == nil {
		return nil
	}
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return nil
	}
	name := id
	if svc, ok := s.cache.GetService(id); ok && svc.Spec.Name != "" {
		name = svc.Spec.Name
	}
	if !s.acl.Can(identity, "write", "service:"+name) {
		return fmt.Errorf("write access denied for service:%s", name)
	}
	return nil
}

// checkNodeWrite mirrors checkServiceWrite for the node resource type. Node
// ACL keys are `node:<hostname>` to match REST.
func (s *Server) checkNodeWrite(ctx context.Context, id string) error {
	if s.acl == nil {
		return nil
	}
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return nil
	}
	name := id
	if node, ok := s.cache.GetNode(id); ok && node.Description.Hostname != "" {
		name = node.Description.Hostname
	}
	if !s.acl.Can(identity, "write", "node:"+name) {
		return fmt.Errorf("write access denied for node:%s", name)
	}
	return nil
}

// checkSimpleWrite is the generic ACL check for resources whose ACL key is just
// `<type>:<id>` (config, secret, network, volume, task).
func (s *Server) checkSimpleWrite(ctx context.Context, resourceType, id string) error {
	if s.acl == nil {
		return nil
	}
	identity := auth.IdentityFromContext(ctx)
	if identity == nil {
		return nil
	}
	if !s.acl.Can(identity, "write", resourceType+":"+id) {
		return fmt.Errorf("write access denied for %s:%s", resourceType, id)
	}
	return nil
}

// --- tool handlers ---

func (s *Server) toolSearch(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return "", err
	}
	limit := req.GetInt("limit", 3)

	results := cluster.Search(ctx, s.cache, query, limit)
	return marshalResult(results)
}

func (s *Server) toolGetLogs(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	service, err := req.RequireString("service")
	if err != nil {
		return "", err
	}
	resp, err := s.readServiceLogsImpl(ctx, service, optsFromToolRequest(req))
	if err != nil {
		return "", err
	}
	return marshalResult(resp)
}

func (s *Server) toolScaleService(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	replicas, err := req.RequireInt("replicas")
	if err != nil {
		return "", err
	}
	if replicas < 0 {
		return "", fmt.Errorf("replicas must be >= 0")
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.ScaleService(ctx, id, uint64(replicas))
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateServiceImage(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	image, err := req.RequireString("image")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServiceImage(ctx, id, image)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolRollbackService(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.RollbackService(ctx, id)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolRestartService(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.RestartService(ctx, id)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolRemoveTask(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	if err := s.checkSimpleWrite(ctx, "task", id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	if err := wc.RemoveTask(ctx, id); err != nil {
		return "", err
	}
	return `{"removed":true}`, nil
}

func (s *Server) toolUpdateServiceEnv(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	env, err := requireStringMap(req, "env")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServiceEnv(ctx, id, env)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateServiceLabels(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	labels, err := requireStringMap(req, "labels")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServiceLabels(ctx, id, labels)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateNodeLabels(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	labels, err := requireStringMap(req, "labels")
	if err != nil {
		return "", err
	}
	if err := s.checkNodeWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	node, err := wc.UpdateNodeLabels(ctx, id, labels)
	if err != nil {
		return "", err
	}
	return marshalResult(node)
}

func (s *Server) toolUpdateServiceResources(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	var resources swarm.ResourceRequirements
	if err := decodeArgInto(req, "resources", &resources); err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServiceResources(ctx, id, &resources)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateServicePlacement(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	var placement swarm.Placement
	if err := decodeArgInto(req, "placement", &placement); err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServicePlacement(ctx, id, &placement)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateServicePorts(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	var ports []swarm.PortConfig
	if err := decodeArgInto(req, "ports", &ports); err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServicePorts(ctx, id, ports)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateServiceUpdatePolicy(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	var policy swarm.UpdateConfig
	if err := decodeArgInto(req, "policy", &policy); err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServiceUpdatePolicy(ctx, id, &policy)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateServiceRollbackPolicy(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	var policy swarm.UpdateConfig
	if err := decodeArgInto(req, "policy", &policy); err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServiceRollbackPolicy(ctx, id, &policy)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateServiceLogDriver(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	var driver swarm.Driver
	if err := decodeArgInto(req, "driver", &driver); err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	svc, err := wc.UpdateServiceLogDriver(ctx, id, &driver)
	if err != nil {
		return "", err
	}
	return marshalResult(svc)
}

func (s *Server) toolUpdateNodeAvailability(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	availability, err := req.RequireString("availability")
	if err != nil {
		return "", err
	}
	avail, err := parseNodeAvailability(availability)
	if err != nil {
		return "", err
	}
	if err := s.checkNodeWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	node, err := wc.UpdateNodeAvailability(ctx, id, avail)
	if err != nil {
		return "", err
	}
	return marshalResult(node)
}

func (s *Server) toolUpdateNodeRole(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	role, err := req.RequireString("role")
	if err != nil {
		return "", err
	}
	r, err := parseNodeRole(role)
	if err != nil {
		return "", err
	}
	if err := s.checkNodeWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	node, err := wc.UpdateNodeRole(ctx, id, r)
	if err != nil {
		return "", err
	}
	return marshalResult(node)
}

func (s *Server) toolRemoveService(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	if err := wc.RemoveService(ctx, id); err != nil {
		return "", err
	}
	return `{"removed":true}`, nil
}

func (s *Server) toolRemoveConfig(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	if err := s.checkSimpleWrite(ctx, "config", id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	if err := wc.RemoveConfig(ctx, id); err != nil {
		return "", err
	}
	return `{"removed":true}`, nil
}

func (s *Server) toolRemoveSecret(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	if err := s.checkSimpleWrite(ctx, "secret", id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	if err := wc.RemoveSecret(ctx, id); err != nil {
		return "", err
	}
	return `{"removed":true}`, nil
}

func (s *Server) toolRemoveNetwork(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	if err := s.checkSimpleWrite(ctx, "network", id); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	if err := wc.RemoveNetwork(ctx, id); err != nil {
		return "", err
	}
	return `{"removed":true}`, nil
}

func (s *Server) toolRemoveVolume(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return "", err
	}
	force := req.GetBool("force", false)
	if err := s.checkSimpleWrite(ctx, "volume", name); err != nil {
		return "", err
	}
	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	if err := wc.RemoveVolume(ctx, name, force); err != nil {
		return "", err
	}
	return `{"removed":true}`, nil
}

// --- argument helpers ---

func marshalResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// requireStringMap extracts a `map[string]string` from a tool argument.
// Accepts the value either as `map[string]any` (raw JSON object) or
// `map[string]string` (already coerced).
func requireStringMap(req mcplib.CallToolRequest, key string) (map[string]string, error) {
	args := req.GetArguments()
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("missing required argument %q", key)
	}
	switch m := raw.(type) {
	case map[string]string:
		return m, nil
	case map[string]any:
		out := make(map[string]string, len(m))
		for k, v := range m {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("argument %q[%s] must be a string", key, k)
			}
			out[k] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("argument %q must be an object of strings", key)
	}
}

// decodeArgInto round-trips the named argument through JSON into target. The
// caller passes a pointer to a struct/slice; this lets handlers accept rich
// Docker types from MCP clients without per-field decoding.
func decodeArgInto(req mcplib.CallToolRequest, key string, target any) error {
	args := req.GetArguments()
	raw, ok := args[key]
	if !ok {
		return fmt.Errorf("missing required argument %q", key)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("re-encode %q: %w", key, err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		return fmt.Errorf("decode %q: %w", key, err)
	}
	return nil
}

func parseNodeAvailability(s string) (swarm.NodeAvailability, error) {
	switch s {
	case "active":
		return swarm.NodeAvailabilityActive, nil
	case "pause":
		return swarm.NodeAvailabilityPause, nil
	case "drain":
		return swarm.NodeAvailabilityDrain, nil
	default:
		return "", fmt.Errorf("invalid availability %q (expected active/pause/drain)", s)
	}
}

func parseNodeRole(s string) (swarm.NodeRole, error) {
	switch s {
	case "worker":
		return swarm.NodeRoleWorker, nil
	case "manager":
		return swarm.NodeRoleManager, nil
	default:
		return "", fmt.Errorf("invalid role %q (expected worker/manager)", s)
	}
}
