package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

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
//
// Each tool sets all four behaviour hints explicitly (read-only, destructive,
// idempotent, open-world) so clients can render confirmation UI accurately —
// mcp-go's NewTool defaults destructive and open-world to true, which is the
// wrong shape for a closed cluster-management surface.
func (s *Server) toolCatalog() []toolDef {
	return []toolDef{
		// Tier 0 — parameterized reads, no gating.
		{
			tool: mcplib.NewTool(
				"get_logs",
				mcplib.WithToolTitle("Read service logs"),
				mcplib.WithDescription(
					"Fetch recent log lines for a Swarm service, merged across every replica task. Returns the most recent `tail` lines (default 100), optionally filtered by start time and minimum severity. This is a one-shot read; for live tails subscribe to the cetacean://services/{id}/logs resource.",
				),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("service",
					mcplib.Required(),
					mcplib.Description("Service ID or name to read logs from."),
				),
				mcplib.WithNumber("tail",
					mcplib.Description("Number of recent log lines to return per replica. Default 100."),
				),
				mcplib.WithString("since",
					mcplib.Description("RFC 3339 timestamp; only return lines emitted at or after this time."),
				),
				mcplib.WithString("level",
					mcplib.Description("Minimum log level (debug, info, warn, error). Best-effort — depends on the service emitting structured levels."),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetLogs,
		},
		{
			tool: mcplib.NewTool(
				"search",
				mcplib.WithToolTitle("Search swarm resources"),
				mcplib.WithDescription(
					"Search across every cluster resource (nodes, services, tasks, stacks, configs, secrets, networks, volumes) by name, label, or image reference. Returns ranked matches grouped by resource type. Use this to locate a resource before fetching its details or applying a write.",
				),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("query",
					mcplib.Required(),
					mcplib.Description("Substring to match against resource names, labels, and (for services) image references. Case-insensitive."),
				),
				mcplib.WithNumber("limit",
					mcplib.Description("Maximum results per resource type. Default 3."),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolSearch,
		},

		// Tier 1 — Operational.
		{
			tool: mcplib.NewTool(
				"scale_service",
				mcplib.WithToolTitle("Scale service replicas"),
				mcplib.WithDescription(
					"Set the desired replica count of a replicated service. Swarm reconciles asynchronously; calling with the current count is a no-op. Setting to 0 stops every task without removing the service. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithNumber("replicas",
					mcplib.Required(),
					mcplib.Description("Target replica count. Must be >= 0."),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolScaleService,
		},
		{
			tool: mcplib.NewTool(
				"update_service_image",
				mcplib.WithToolTitle("Update service image"),
				mcplib.WithDescription(
					"Set the container image for a service, triggering a rolling deploy per the service's update policy. The previous spec is retained on the service history and reachable via rollback_service. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithString("image",
					mcplib.Required(),
					mcplib.Description("Container image reference including tag or digest (e.g. \"nginx:1.27\" or \"nginx@sha256:...\"). Must not be empty."),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolUpdateServiceImage,
		},
		{
			tool: mcplib.NewTool("rollback_service",
				mcplib.WithToolTitle("Roll back service to previous spec"),
				mcplib.WithDescription(
					"Revert a service to its previous spec, discarding the current one. Only the immediately previous spec is retained — calling twice does not unwind further. Returns the rolled-back service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolRollbackService,
		},
		{
			tool: mcplib.NewTool("restart_service",
				mcplib.WithToolTitle("Force-restart service"),
				mcplib.WithDescription(
					"Force a rolling restart of every task in the service by bumping its spec ForceUpdate counter. No other spec fields change. Honours the service's update policy (parallelism, delay). Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolRestartService,
		},
		{
			tool: mcplib.NewTool("remove_task",
				mcplib.WithToolTitle("Remove (reschedule) task"),
				mcplib.WithDescription(
					"Delete a task by ID. Swarm immediately reschedules a replacement on the parent service's behalf, so this is a forced reschedule rather than a permanent removal. Use this to evict a misbehaving task without scaling the whole service.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Task ID (long-form Docker ID)."),
				),
			),
			tier: config.OpsOperational,
			handler: s.removeHandler("id", s.checkTaskWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveTask(ctx, id)
				}),
		},

		// Tier 2 — Configuration.
		{
			tool: mcplib.NewTool(
				"update_service_env",
				mcplib.WithToolTitle("Update service environment variables"),
				mcplib.WithDescription(
					"Patch the environment-variable map of a service using JSON Merge Patch (RFC 7396) semantics: string values set or replace a key, null values delete it, omitted keys are preserved. Triggers a rolling deploy. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithObject("env",
					mcplib.Required(),
					mcplib.Description("Merge-patch object mapping env-var names to a string value (set) or null (delete). Example: {\"LOG_LEVEL\":\"debug\",\"DEPRECATED_FLAG\":null}."),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceEnv,
		},
		{
			tool: mcplib.NewTool("update_service_labels",
				mcplib.WithToolTitle("Update service labels"),
				mcplib.WithDescription(
					"Patch the labels on a service using JSON Merge Patch (RFC 7396) semantics: string values set or replace a key, null values delete it, omitted keys are preserved. Does not restart tasks. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithObject("labels",
					mcplib.Required(),
					mcplib.Description("Merge-patch object mapping label keys to a string value (set) or null (delete)."),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceLabels,
		},
		{
			tool: mcplib.NewTool("update_node_labels",
				mcplib.WithToolTitle("Update node labels"),
				mcplib.WithDescription(
					"Patch the labels on a node using JSON Merge Patch (RFC 7396) semantics: string values set or replace a key, null values delete it, omitted keys are preserved. Node labels are commonly used as service placement constraints. Returns the updated node spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Node ID or hostname."),
				),
				mcplib.WithObject("labels",
					mcplib.Required(),
					mcplib.Description("Merge-patch object mapping label keys to a string value (set) or null (delete)."),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateNodeLabels,
		},
		{
			tool: mcplib.NewTool(
				"update_service_resources",
				mcplib.WithToolTitle("Update service resource limits"),
				mcplib.WithDescription(
					"Replace the ResourceRequirements (CPU/memory limits and reservations) on a service. Pass the full object — fields you omit are cleared. Triggers a rolling deploy. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithObject("resources",
					mcplib.Required(),
					mcplib.Description("Full ResourceRequirements object: {\"Limits\":{\"NanoCPUs\":int,\"MemoryBytes\":int},\"Reservations\":{\"NanoCPUs\":int,\"MemoryBytes\":int}}. NanoCPUs are in 1e-9 of a CPU; MemoryBytes is in bytes."),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(s, "resources",
				func(wc DockerWriteClient, ctx context.Context, id string, r swarm.ResourceRequirements) (swarm.Service, error) {
					return wc.UpdateServiceResources(ctx, id, &r)
				}),
		},
		{
			tool: mcplib.NewTool(
				"update_service_placement",
				mcplib.WithToolTitle("Update service placement constraints"),
				mcplib.WithDescription(
					"Replace the Placement object on a service (constraints, preferences, max replicas per node, platforms). Triggers a rolling deploy that may reschedule tasks to satisfy new constraints. Pass the full object — fields you omit are cleared. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithObject("placement",
					mcplib.Required(),
					mcplib.Description("Full Placement object. Constraints use Docker Swarm syntax (e.g. \"node.role==worker\", \"node.labels.zone==eu-west\")."),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(s, "placement",
				func(wc DockerWriteClient, ctx context.Context, id string, p swarm.Placement) (swarm.Service, error) {
					return wc.UpdateServicePlacement(ctx, id, &p)
				}),
		},
		{
			tool: mcplib.NewTool(
				"update_service_ports",
				mcplib.WithToolTitle("Update service published ports"),
				mcplib.WithDescription(
					"Replace the ingress port map on a service. Adds, removes, or remaps published ports; connections to removed or remapped ports are dropped. Triggers a rolling deploy. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithArray("ports",
					mcplib.Required(),
					mcplib.Description("Array of PortConfig objects: {\"Protocol\":\"tcp|udp|sctp\",\"TargetPort\":int,\"PublishedPort\":int,\"PublishMode\":\"ingress|host\"}."),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(s, "ports",
				func(wc DockerWriteClient, ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error) {
					return wc.UpdateServicePorts(ctx, id, ports)
				}),
		},
		{
			tool: mcplib.NewTool(
				"update_service_update_policy",
				mcplib.WithToolTitle("Update service rolling-update policy"),
				mcplib.WithDescription(
					"Replace the UpdateConfig (rolling-update parallelism, delay, failure action, monitor window, max failure ratio, order) on a service. Does NOT trigger a deploy by itself — applies to the next image or spec change. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithObject("policy",
					mcplib.Required(),
					mcplib.Description("Full UpdateConfig object: {\"Parallelism\":int,\"Delay\":nanoseconds,\"FailureAction\":\"pause|continue|rollback\",\"Monitor\":nanoseconds,\"MaxFailureRatio\":float,\"Order\":\"stop-first|start-first\"}."),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(s, "policy",
				func(wc DockerWriteClient, ctx context.Context, id string, p swarm.UpdateConfig) (swarm.Service, error) {
					return wc.UpdateServiceUpdatePolicy(ctx, id, &p)
				}),
		},
		{
			tool: mcplib.NewTool(
				"update_service_rollback_policy",
				mcplib.WithToolTitle("Update service rollback policy"),
				mcplib.WithDescription(
					"Replace the RollbackConfig on a service. Does NOT trigger a rollback by itself — controls how rollback_service and automatic rollback-on-failure behave. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithObject("policy",
					mcplib.Required(),
					mcplib.Description("Full UpdateConfig object (same shape as the rolling-update policy): {\"Parallelism\":int,\"Delay\":nanoseconds,\"FailureAction\":\"pause|continue\",\"Monitor\":nanoseconds,\"MaxFailureRatio\":float,\"Order\":\"stop-first|start-first\"}."),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(s, "policy",
				func(wc DockerWriteClient, ctx context.Context, id string, p swarm.UpdateConfig) (swarm.Service, error) {
					return wc.UpdateServiceRollbackPolicy(ctx, id, &p)
				}),
		},
		{
			tool: mcplib.NewTool(
				"update_service_log_driver",
				mcplib.WithToolTitle("Update service log driver"),
				mcplib.WithDescription(
					"Set the log driver (name + options) for a service. Triggers a rolling deploy; subsequent log lines are routed through the new driver. Returns the updated service spec.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithObject("driver",
					mcplib.Required(),
					mcplib.Description("Driver object: {\"Name\":\"json-file|syslog|journald|gelf|fluentd|awslogs|...\",\"Options\":{\"key\":\"value\"}}."),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(s, "driver",
				func(wc DockerWriteClient, ctx context.Context, id string, d swarm.Driver) (swarm.Service, error) {
					return wc.UpdateServiceLogDriver(ctx, id, &d)
				}),
		},

		// Tier 3 — Impactful.
		{
			tool: mcplib.NewTool("update_node_availability",
				mcplib.WithToolTitle("Update node availability"),
				mcplib.WithDescription(
					"Set a node's availability state. `active` accepts new tasks; `pause` rejects new tasks but keeps existing ones running; `drain` evicts every task to other nodes. Draining a manager may destabilise the swarm — prefer draining workers.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Node ID or hostname."),
				),
				mcplib.WithString("availability",
					mcplib.Required(),
					mcplib.Description("Target availability state."),
					mcplib.Enum("active", "pause", "drain"),
				),
			),
			tier:    config.OpsImpactful,
			handler: s.toolUpdateNodeAvailability,
		},
		{
			tool: mcplib.NewTool("update_node_role",
				mcplib.WithToolTitle("Promote or demote node"),
				mcplib.WithDescription(
					"Change a node's role between `worker` and `manager`. Promoting adds a Raft voter; demoting removes one. Never reduce the manager count below the quorum threshold — for a 3-manager cluster, demote at most one manager at a time.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Node ID or hostname."),
				),
				mcplib.WithString("role",
					mcplib.Required(),
					mcplib.Description("Target node role."),
					mcplib.Enum("worker", "manager"),
				),
			),
			tier:    config.OpsImpactful,
			handler: s.toolUpdateNodeRole,
		},
		{
			tool: mcplib.NewTool("remove_service",
				mcplib.WithToolTitle("Remove service"),
				mcplib.WithDescription(
					"Delete a service and stop every task it owns. Does not delete the volumes, networks, configs, or secrets it referenced. Irreversible — no spec history is retained after removal.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkServiceWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveService(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_config",
				mcplib.WithToolTitle("Remove config"),
				mcplib.WithDescription(
					"Delete a Docker config object. Fails if any service still references it. Irreversible — the config payload cannot be recovered.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Config ID."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkConfigWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveConfig(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_secret",
				mcplib.WithToolTitle("Remove secret"),
				mcplib.WithDescription(
					"Delete a Docker secret. Fails if any service still references it. Irreversible — the secret payload cannot be recovered.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Secret ID."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkSecretWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveSecret(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_network",
				mcplib.WithToolTitle("Remove network"),
				mcplib.WithDescription(
					"Delete an overlay network. Fails if any service is still attached. Built-in networks (`ingress`, `docker_gwbridge`) cannot be removed.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Network ID."),
				),
			),
			tier: config.OpsImpactful,
			handler: s.removeHandler("id", s.checkNetworkWrite,
				func(wc DockerWriteClient, ctx context.Context, id string) error {
					return wc.RemoveNetwork(ctx, id)
				}),
		},
		{
			tool: mcplib.NewTool("remove_volume",
				mcplib.WithToolTitle("Remove volume"),
				mcplib.WithDescription(
					"Delete a Docker volume by name. Fails if any container is currently using it unless `force` is set. Volume contents are irrecoverable.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("name",
					mcplib.Required(),
					mcplib.Description("Volume name."),
				),
				mcplib.WithBoolean("force",
					mcplib.Description("Force removal even if the volume is in use. Default false."),
				),
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
func (s *Server) checkServiceRead(ctx context.Context, id string) error {
	if s.acl == nil {
		return nil
	}
	svc, ok := s.cache.GetService(id)
	if !ok {
		return fmt.Errorf("service %q not found", id)
	}
	return s.checkRead(ctx, "service", svc.Spec.Name)
}

// --- tool handlers ---

func (s *Server) toolSearch(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	query, err := req.RequireString("query")
	if err != nil {
		return "", err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query must not be empty")
	}
	limit := req.GetInt("limit", 3)

	results := s.filterSearchResults(ctx, cluster.Search(ctx, s.cache, query, limit))
	return marshalResult(results)
}

func (s *Server) toolGetLogs(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	service, err := req.RequireString("service")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceRead(ctx, service); err != nil {
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
	image = strings.TrimSpace(image)
	if image == "" {
		return "", fmt.Errorf("image must not be empty")
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

// removeHandler builds a tool handler for the common `{ id } → {"removed":true}`
// shape shared by remove_task / remove_service / remove_config / remove_secret
// / remove_network. idKey is the JSON-Schema property name (`id` for most,
// `name` for volumes); aclCheck enforces write permission against the right
// resource (often delegating to checkServiceWrite/checkNodeWrite when the ACL
// key derives from a cached name); remove invokes the actual writer.
func (s *Server) removeHandler(
	idKey string,
	aclCheck func(ctx context.Context, id string) error,
	remove func(wc DockerWriteClient, ctx context.Context, id string) error,
) func(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
		id, err := req.RequireString(idKey)
		if err != nil {
			return "", err
		}
		if err := aclCheck(ctx, id); err != nil {
			return "", err
		}
		wc, err := s.requireWriteClient()
		if err != nil {
			return "", err
		}
		if err := remove(wc, ctx, id); err != nil {
			return "", err
		}
		return `{"removed":true}`, nil
	}
}

func (s *Server) toolUpdateServiceEnv(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	patch, err := requireStringMapPatch(req, "env")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}

	svc, ok := s.cache.GetService(id)
	if !ok {
		return "", fmt.Errorf("service %q not found", id)
	}
	current := serviceEnvMap(svc)
	merged := applyMergePatch(current, patch)

	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	updated, err := wc.UpdateServiceEnv(ctx, id, merged)
	if err != nil {
		return "", err
	}
	return marshalResult(updated)
}

func (s *Server) toolUpdateServiceLabels(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	patch, err := requireStringMapPatch(req, "labels")
	if err != nil {
		return "", err
	}
	if err := s.checkServiceWrite(ctx, id); err != nil {
		return "", err
	}

	svc, ok := s.cache.GetService(id)
	if !ok {
		return "", fmt.Errorf("service %q not found", id)
	}
	merged := applyMergePatch(copyStringMap(svc.Spec.Labels), patch)

	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	updated, err := wc.UpdateServiceLabels(ctx, id, merged)
	if err != nil {
		return "", err
	}
	return marshalResult(updated)
}

func (s *Server) toolUpdateNodeLabels(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	id, err := req.RequireString("id")
	if err != nil {
		return "", err
	}
	patch, err := requireStringMapPatch(req, "labels")
	if err != nil {
		return "", err
	}
	if err := s.checkNodeWrite(ctx, id); err != nil {
		return "", err
	}

	node, ok := s.cache.GetNode(id)
	if !ok {
		return "", fmt.Errorf("node %q not found", id)
	}
	merged := applyMergePatch(copyStringMap(node.Spec.Labels), patch)

	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	updated, err := wc.UpdateNodeLabels(ctx, id, merged)
	if err != nil {
		return "", err
	}
	return marshalResult(updated)
}

// updateServiceHandler builds a tool handler for the recurring
// `{id, <argKey>: T} → swarm.Service` shape used by every Tier 2 service-spec
// update tool. T is decoded via JSON round-trip through decodeArgInto so
// callers can pass the same shape they see in the REST API.
//
// Go doesn't allow generic methods, so this is a free function taking *Server.
func updateServiceHandler[T any](
	s *Server,
	argKey string,
	call func(wc DockerWriteClient, ctx context.Context, id string, arg T) (swarm.Service, error),
) func(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	return func(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return "", err
		}
		var arg T
		if err := decodeArgInto(req, argKey, &arg); err != nil {
			return "", err
		}
		if err := s.checkServiceWrite(ctx, id); err != nil {
			return "", err
		}
		wc, err := s.requireWriteClient()
		if err != nil {
			return "", err
		}
		svc, err := call(wc, ctx, id, arg)
		if err != nil {
			return "", err
		}
		return marshalResult(svc)
	}
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

func (s *Server) toolRemoveVolume(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	name, err := req.RequireString("name")
	if err != nil {
		return "", err
	}
	force := req.GetBool("force", false)
	if err := s.checkWrite(ctx, "volume", name); err != nil {
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

// requireStringMapPatch extracts a JSON Merge Patch (RFC 7396) of string keys
// from a tool argument. Values may be strings (set) or null (delete); any
// other type is rejected. Mirrors how REST patches env/label maps.
func requireStringMapPatch(req mcplib.CallToolRequest, key string) (map[string]*string, error) {
	args := req.GetArguments()
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("missing required argument %q", key)
	}
	switch m := raw.(type) {
	case map[string]string:
		out := make(map[string]*string, len(m))
		for k, v := range m {
			out[k] = &v
		}
		return out, nil
	case map[string]any:
		out := make(map[string]*string, len(m))
		for k, v := range m {
			if v == nil {
				out[k] = nil
				continue
			}
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("argument %q[%s] must be a string or null", key, k)
			}
			out[k] = &s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("argument %q must be an object", key)
	}
}

// applyMergePatch merges patch into base, returning a new map. Nil values in
// patch delete the corresponding key (RFC 7396 merge semantics).
func applyMergePatch(base map[string]string, patch map[string]*string) map[string]string {
	out := make(map[string]string, len(base)+len(patch))
	maps.Copy(out, base)
	for k, v := range patch {
		if v == nil {
			delete(out, k)
			continue
		}
		out[k] = *v
	}
	return out
}

// copyStringMap returns an independent copy of m. Nil input yields an empty
// (non-nil) map so callers can apply patches without nil-checks.
func copyStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	maps.Copy(out, m)
	return out
}

// serviceEnvMap converts a service's `KEY=VALUE` env slice to a string map.
// Bare-key entries (no `=`) map to the empty string. Matches REST's
// envSliceToMap so the merge semantics are identical.
func serviceEnvMap(svc swarm.Service) map[string]string {
	out := map[string]string{}
	if svc.Spec.TaskTemplate.ContainerSpec == nil {
		return out
	}
	for _, e := range svc.Spec.TaskTemplate.ContainerSpec.Env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			out[e] = ""
			continue
		}
		out[k] = v
	}
	return out
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
