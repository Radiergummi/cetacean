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
	"github.com/radiergummi/cetacean/internal/docker"
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
	UpdateServiceEnv(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Service, error)
	UpdateServiceLabels(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
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
	UpdateNodeLabels(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (swarm.Node, error)
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

	// widget names the MCP Apps view that renders this tool's result, if any.
	// Declared here rather than built into the tool above so registerTools can
	// withhold it when the widget was not built — see registerTools.
	widget string
}

// toolIconCategory maps each tool to one of six verb-category icons served
// from the embedded frontend under /assets/mcp-icons/<category>.svg. Tools not
// listed here (there are none currently) are served without an icon.
var toolIconCategory = map[string]string{
	"get_logs":            "read",
	"get_topology":        "read",
	"get_metrics":         "read",
	"get_recommendations": "read",
	"find":                "search",
	"describe":            "read",

	"scale_service": "scale",

	"update_service_image":           "edit",
	"rollback_service":               "edit",
	"restart_service":                "edit",
	"update_service_env":             "edit",
	"update_service_labels":          "edit",
	"update_service_resources":       "edit",
	"update_service_placement":       "edit",
	"update_service_ports":           "edit",
	"update_service_update_policy":   "edit",
	"update_service_rollback_policy": "edit",
	"update_service_log_driver":      "edit",

	"update_node_labels":       "node",
	"update_node_availability": "node",
	"update_node_role":         "node",

	"remove_task":    "remove",
	"remove_service": "remove",
	"remove_config":  "remove",
	"remove_secret":  "remove",
	"remove_network": "remove",
	"remove_volume":  "remove",
}

// icon builds a single-element icon set pointing at
// {base}/assets/mcp-icons/{group}/{name}.svg, or nil when icons are disabled
// (no base URL). The `src` is an absolute URL under the un-authed /assets/
// prefix so MCP clients can fetch it without a bearer token; per the MCP spec
// icons must be an HTTPS or data URI, never relative.
func (s *Server) icon(group, name string) []mcplib.Icon {
	if s.iconBaseURL == "" {
		return nil
	}
	return []mcplib.Icon{{
		Src:      s.iconBaseURL + "/assets/mcp-icons/" + group + "/" + name + ".svg",
		MIMEType: "image/svg+xml",
		Sizes:    []string{"any"},
	}}
}

// iconsForTool returns the verb-category icon for a tool, or nil when the tool
// has no category mapping (there are none currently).
func (s *Server) iconsForTool(name string) []mcplib.Icon {
	category, ok := toolIconCategory[name]
	if !ok {
		return nil
	}
	return s.icon("tools", category)
}

func (s *Server) registerTools() {
	tools := s.toolCatalog()

	opsLevel := s.config.EffectiveOperationsLevel(s.globalOpsLevel)

	s.registeredTools = make([]toolDef, 0, len(tools))
	for _, td := range tools {
		if opsLevel < td.tier {
			continue
		}

		td.tool.Icons = s.iconsForTool(td.tool.Name)

		// Point the host at this tool's widget, but only if the widget build
		// actually produced it. A binary built without `npm run build:widgets`
		// serves no ui:// resources, and a tool naming one anyway would send a
		// host off to fetch a resource that does not exist.
		if td.widget != "" && hasWidget(td.widget) {
			td.tool.Meta = toolUIMeta(td.widget)
		}
		s.registeredTools = append(s.registeredTools, td)

		handler := td.handler
		s.mcpServer.AddTool(
			td.tool,
			func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
				ctx, textOnly := withTextOnlyResultSignal(ctx)

				text, err := handler(ctx, req)
				if err != nil {
					// On a plain call the failure belongs in the result, so
					// the model reads it in its context window and can
					// self-correct (SEP-1303).
					//
					// As a task it must be a real error instead: mcp-go marks
					// a task completed whenever the handler returns no error,
					// so a tool error returned this way would leave an agent
					// polling tasks/get and reading "completed" for a mutation
					// that was refused.
					if req.Params.Task != nil {
						return nil, err
					}

					return mcplib.NewToolResultError(err.Error()), nil
				}

				// A handler that called markTextOnlyResult built a result it
				// knows does not conform to the tool's declared outputSchema
				// (find's and describe's raw modes) — presenting it as
				// structuredContent anyway would fail
				// WithOutputSchemaValidation's check on the very call the
				// handler asked for something other than the compact shape.
				if *textOnly {
					return mcplib.NewToolResultText(text), nil
				}

				return structuredToolResult(text), nil
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
				mcplib.WithToolTitle("Read service or task logs"),
				mcplib.WithDescription(
					"Fetch recent log lines for a Swarm service or for one of its tasks. Name exactly one: `service` merges the output of every live replica; `task` reads a single replica, and is the only way to reach one that has already exited — use it to find out why a replica died, since a dead replica's lines are not in the service stream. Swarm discards a task's output once its record falls out of the history window (five per replica slot by default), so on a service that is restarting in a loop that history is only seconds deep and should be read first. Returns the most recent `tail` lines (default 100), optionally filtered by start time and minimum severity. This is a one-shot read; for live tails subscribe to the cetacean://services/{id}/logs resource.",
				),
				mcplib.WithOutputSchema[LogResourceResponse](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("service",
					mcplib.Description(
						"Service ID or name to read logs from, merged across its live replicas. Mutually exclusive with `task`.",
					),
				),
				mcplib.WithString("task",
					mcplib.Description(
						"Task ID to read logs from, including a task that has already exited. Mutually exclusive with `service`.",
					),
				),
				mcplib.WithNumber(
					"tail",
					mcplib.Description(
						"Number of recent log lines to return per replica. Default 100.",
					),
				),
				mcplib.WithString(
					"since",
					mcplib.Description(
						"RFC 3339 timestamp; only return lines emitted strictly after this time.",
					),
				),
				mcplib.WithString(
					"level",
					mcplib.Description(
						"Minimum log level (debug, info, warn, error). Best-effort — depends on the service emitting structured levels.",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetLogs,
			widget:  "logs",
		},
		{
			tool: mcplib.NewTool(
				"find",
				mcplib.WithToolTitle("Find cluster resources"),
				mcplib.WithDescription(
					"Locate cluster resources. Give `type` (nodes, services, tasks, stacks, configs, secrets, networks, or volumes) to enumerate that type, paged, optionally narrowed by query/state/stack/node/image/label; the returned records are the same ones the cetacean:// resources expose, filtered to what the caller may see. Omit `type` and give `query` to search by name, label, or image reference across every type at once, returned as one list sorted by name, each row carrying its own type. Use the cross-type search to locate a resource before fetching its details or applying a write; use a typed listing to browse or tabulate a whole type.",
				),
				mcplib.WithOutputSchema[findResult](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString(
					"type",
					mcplib.Description(
						"Resource type to enumerate: nodes, services, tasks, stacks, configs, secrets, networks, or volumes. Omit to search across every type at once (requires `query`).",
					),
				),
				mcplib.WithString(
					"query",
					mcplib.Description(
						"Substring to match against resource names — and, when `type` is omitted, also labels and image references. Case-insensitive. Required when `type` is omitted.",
					),
				),
				mcplib.WithString("state",
					mcplib.Description(
						"With `type`, keep only rows whose derived state matches exactly (case-insensitive).",
					),
				),
				mcplib.WithString("stack",
					mcplib.Description(
						"With `type`, keep only rows in this stack namespace (exact match, case-insensitive).",
					),
				),
				mcplib.WithString("node",
					mcplib.Description(
						"With `type: tasks`, keep only rows running on a node whose hostname contains this (case-insensitive).",
					),
				),
				mcplib.WithString("image",
					mcplib.Description(
						"With `type: services`, keep only rows whose image contains this (case-insensitive).",
					),
				),
				mcplib.WithString("label",
					mcplib.Description(
						"With `type`, keep only rows carrying this label: `key` for presence, `key=value` for an exact value.",
					),
				),
				mcplib.WithNumber("limit",
					mcplib.Description(
						"With `type`, the maximum records to return (default and maximum 200). Without `type`, the maximum matches per resource type in the cross-type search (default 3).",
					),
				),
				mcplib.WithNumber("offset",
					mcplib.Description(
						"With `type`, records to skip, for paging. Default 0. Ignored in the cross-type search.",
					),
				),
				mcplib.WithBoolean("raw",
					mcplib.Description(
						"With `type`, return each match's untouched resource record instead of the compact row shape. Default false.",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolFind,
			widget:  "table",
		},
		{
			tool: mcplib.NewTool(
				"describe",
				mcplib.WithToolTitle("Describe one cluster resource"),
				mcplib.WithDescription(
					"Return everything needed to act on one resource: its derived state, the reason Swarm gave for that state when it is not healthy, how long the state has held, the type-specific facts (a service's image, replica counts, reserved CPU in cores and memory in bytes, ports, placement constraints and environment variable *names*; a node's role, availability, capacity; a network's driver and subnets), the resources it references or is referenced by, and the recent task failures behind a failing state. Name the type in the singular (service, node, task, stack, config, secret, network, volume) and give an ID or a name. Use this instead of a resource read when following up on a finding from find or get_recommendations; secret payloads and environment variable values are never returned.",
				),
				mcplib.WithOutputSchema[cluster.Digest](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("type",
					mcplib.Required(),
					mcplib.Description(
						"Resource type, singular: service, node, task, stack, config, secret, network or volume.",
					),
				),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description(
						"The resource's ID, or its name (hostname for a node, name for a stack or volume).",
					),
				),
				mcplib.WithBoolean("raw",
					mcplib.Description(
						"Return the untouched Docker record instead of the digest. Default false.",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolDescribe,
		},
		{
			tool: mcplib.NewTool(
				"get_topology",
				mcplib.WithToolTitle("Get cluster topology"),
				mcplib.WithDescription(
					"Return the cluster as a graph. The network view joins services to the overlay networks they attach to, answering which services can reach each other; the placement view joins cluster nodes to the services they run, answering what runs where. Use this instead of listing services and nodes separately when the question is about relationships.",
				),
				mcplib.WithOutputSchema[cluster.TopologyGraph](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString(
					"view",
					mcplib.Description(
						"Which projection to return: \"network\" (services joined to overlay networks, the default) or \"placement\" (cluster nodes joined to the services they run).",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetTopology,
			widget:  "topology",
		},
		{
			tool: mcplib.NewTool(
				"get_metrics",
				mcplib.WithToolTitle("Chart a resource metric"),
				mcplib.WithDescription(
					"Return a time series of CPU, memory or network use for one service or one cluster node, over the last hour, six hours, day or week. Requires Prometheus (and cAdvisor for service metrics, node-exporter for node metrics); a Cetacean without them reports that metrics are unavailable rather than returning empty series. Cetacean owns the queries — name a target and a metric, not PromQL.",
				),
				mcplib.WithOutputSchema[metricsResult](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("target",
					mcplib.Required(),
					mcplib.Description(
						"What to measure: \"service\" or \"node\".",
					),
				),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description(
						"The service (ID or name) or node (ID or hostname) to measure.",
					),
				),
				mcplib.WithString("metric",
					mcplib.Description(
						"Which metric: \"cpu\" (default), \"memory\" or \"network\". Service CPU is percent of a core and service memory is bytes; node CPU and memory are percentages. Network is two series, receive and transmit, in bytes per second.",
					),
				),
				mcplib.WithString("range",
					mcplib.Description(
						"Window to chart: \"1h\" (default), \"6h\", \"24h\" or \"7d\".",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetMetrics,
			widget:  "metrics",
		},
		{
			tool: mcplib.NewTool(
				"get_recommendations",
				mcplib.WithToolTitle("Get cluster recommendations"),
				mcplib.WithDescription(
					"Return what Cetacean's recommendation engine currently finds: over- and under-provisioned resources, missing health checks or restart policies, flaky services, single-replica risks, manager workload imbalance and uneven node distribution. Each entry carries a severity, the resource it concerns and why it was raised. Only findings about resources the caller may read are returned.",
				),
				mcplib.WithOutputSchema[recommendationsResult](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("severity",
					mcplib.Description(
						"Return only findings of this severity: \"critical\", \"warning\" or \"info\". Omit for all of them.",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetRecommendations,
			widget:  "recommendations",
		},

		// Tier 1 — Operational.
		{
			tool: mcplib.NewTool(
				"scale_service",
				mcplib.WithToolTitle("Scale service replicas"),
				mcplib.WithDescription(
					"Set the desired replica count of a replicated service. Swarm reconciles asynchronously; calling with the current count is a no-op. Setting to 0 stops every task without removing the service. Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
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
					"Set the container image for a service, triggering a rolling deploy per the service's update policy. The previous spec is retained on the service history and reachable via rollback_service. Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithString(
					"image",
					mcplib.Required(),
					mcplib.Description(
						"Container image reference including tag or digest (e.g. \"nginx:1.27\" or \"nginx@sha256:...\"). Must not be empty.",
					),
				),
			),
			tier:    config.OpsOperational,
			handler: s.toolUpdateServiceImage,
		},
		{
			tool: mcplib.NewTool("rollback_service",
				mcplib.WithToolTitle("Roll back service to previous spec"),
				mcplib.WithDescription(
					"Revert a service to its previous spec, discarding the current one. Only the immediately previous spec is retained — calling twice does not unwind further. Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
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
					"Force a rolling restart of every task in the service by bumping its spec ForceUpdate counter. No other spec fields change. Honours the service's update policy (parallelism, delay). Returns a summary of where the service ended up: id, name, image, mode, desired replicas, running count, derived state and version.",
				),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				taskSupportOptional(),
				mcplib.WithOutputSchema[serviceMutationResult](),
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
				mcplib.WithOutputSchema[removalResult](),
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
				mcplib.WithObject(
					"env",
					mcplib.Required(),
					mcplib.Description(
						"Merge-patch object mapping env-var names to a string value (set) or null (delete). Example: {\"LOG_LEVEL\":\"debug\",\"DEPRECATED_FLAG\":null}.",
					),
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
				mcplib.WithObject(
					"labels",
					mcplib.Required(),
					mcplib.Description(
						"Merge-patch object mapping label keys to a string value (set) or null (delete).",
					),
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
				mcplib.WithObject(
					"labels",
					mcplib.Required(),
					mcplib.Description(
						"Merge-patch object mapping label keys to a string value (set) or null (delete).",
					),
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
				mcplib.WithObject(
					"resources",
					mcplib.Required(),
					mcplib.Description(
						"Full ResourceRequirements object: {\"Limits\":{\"NanoCPUs\":int,\"MemoryBytes\":int},\"Reservations\":{\"NanoCPUs\":int,\"MemoryBytes\":int}}. NanoCPUs are in 1e-9 of a CPU; MemoryBytes is in bytes.",
					),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(
				s,
				"resources",
				func(wc DockerWriteClient, ctx context.Context, id string, r swarm.ResourceRequirements) (swarm.Service, error) {
					return wc.UpdateServiceResources(ctx, id, &r)
				},
			),
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
				mcplib.WithObject(
					"placement",
					mcplib.Required(),
					mcplib.Description(
						"Full Placement object. Constraints use Docker Swarm syntax (e.g. \"node.role==worker\", \"node.labels.zone==eu-west\").",
					),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(
				s,
				"placement",
				func(wc DockerWriteClient, ctx context.Context, id string, p swarm.Placement) (swarm.Service, error) {
					return wc.UpdateServicePlacement(ctx, id, &p)
				},
			),
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
				mcplib.WithArray(
					"ports",
					mcplib.Required(),
					mcplib.Description(
						"Array of PortConfig objects: {\"Protocol\":\"tcp|udp|sctp\",\"TargetPort\":int,\"PublishedPort\":int,\"PublishMode\":\"ingress|host\"}.",
					),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(
				s,
				"ports",
				func(wc DockerWriteClient, ctx context.Context, id string, ports []swarm.PortConfig) (swarm.Service, error) {
					return wc.UpdateServicePorts(ctx, id, ports)
				},
			),
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
				mcplib.WithObject(
					"policy",
					mcplib.Required(),
					mcplib.Description(
						"Full UpdateConfig object: {\"Parallelism\":int,\"Delay\":nanoseconds,\"FailureAction\":\"pause|continue|rollback\",\"Monitor\":nanoseconds,\"MaxFailureRatio\":float,\"Order\":\"stop-first|start-first\"}.",
					),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(
				s,
				"policy",
				func(wc DockerWriteClient, ctx context.Context, id string, p swarm.UpdateConfig) (swarm.Service, error) {
					return wc.UpdateServiceUpdatePolicy(ctx, id, &p)
				},
			),
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
				mcplib.WithObject(
					"policy",
					mcplib.Required(),
					mcplib.Description(
						"Full UpdateConfig object (same shape as the rolling-update policy): {\"Parallelism\":int,\"Delay\":nanoseconds,\"FailureAction\":\"pause|continue\",\"Monitor\":nanoseconds,\"MaxFailureRatio\":float,\"Order\":\"stop-first|start-first\"}.",
					),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(
				s,
				"policy",
				func(wc DockerWriteClient, ctx context.Context, id string, p swarm.UpdateConfig) (swarm.Service, error) {
					return wc.UpdateServiceRollbackPolicy(ctx, id, &p)
				},
			),
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
				mcplib.WithObject(
					"driver",
					mcplib.Required(),
					mcplib.Description(
						"Driver object: {\"Name\":\"json-file|syslog|journald|gelf|fluentd|awslogs|...\",\"Options\":{\"key\":\"value\"}}.",
					),
				),
			),
			tier: config.OpsConfiguration,
			handler: updateServiceHandler(
				s,
				"driver",
				func(wc DockerWriteClient, ctx context.Context, id string, d swarm.Driver) (swarm.Service, error) {
					return wc.UpdateServiceLogDriver(ctx, id, &d)
				},
			),
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
				mcplib.WithOutputSchema[removalResult](),
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
				mcplib.WithOutputSchema[removalResult](),
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
				mcplib.WithOutputSchema[removalResult](),
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
				mcplib.WithOutputSchema[removalResult](),
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
				mcplib.WithOutputSchema[removalResult](),
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
				mcplib.WithBoolean(
					"force",
					mcplib.Description(
						"Force removal even if the volume is in use. Default false.",
					),
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

// --- tool handlers ---

func (s *Server) toolGetLogs(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	service := strings.TrimSpace(req.GetString("service", ""))
	task := strings.TrimSpace(req.GetString("task", ""))

	// Naming both would leave the tool to guess which stream the caller meant,
	// and the two differ: a service merges its live replicas, a task is one
	// replica including a dead one.
	switch {
	case service == "" && task == "":
		return "", fmt.Errorf("one of `service` or `task` is required")
	case service != "" && task != "":
		return "", fmt.Errorf("`service` and `task` are mutually exclusive")
	}

	kind, target := docker.ServiceLog, service
	check := s.checkServiceRead

	if task != "" {
		kind, target = docker.TaskLog, task
		check = s.checkTaskRead
	}

	if err := check(ctx, target); err != nil {
		return "", err
	}

	resp, err := s.readLogsImpl(ctx, kind, target, optsFromToolRequest(req))
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
	if err := s.awaitServiceConvergence(ctx, req, svc); err != nil {
		return "", err
	}
	return marshalResult(s.serviceMutation(svc))
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
	if err := s.awaitServiceConvergence(ctx, req, svc); err != nil {
		return "", err
	}
	return marshalResult(s.serviceMutation(svc))
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
	if err := s.awaitServiceConvergence(ctx, req, svc); err != nil {
		return "", err
	}
	return marshalResult(s.serviceMutation(svc))
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
	if err := s.awaitServiceConvergence(ctx, req, svc); err != nil {
		return "", err
	}
	return marshalResult(s.serviceMutation(svc))
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
		return marshalResult(removalResult{Removed: true})
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

	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	updated, err := wc.UpdateServiceEnv(ctx, id, mergePatchMutator(patch))
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

	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	updated, err := wc.UpdateServiceLabels(ctx, id, mergePatchMutator(patch))
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

	wc, err := s.requireWriteClient()
	if err != nil {
		return "", err
	}
	updated, err := wc.UpdateNodeLabels(ctx, id, mergePatchMutator(patch))
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
	return marshalResult(removalResult{Removed: true})
}

// --- argument helpers ---

func marshalResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// removalResult is the result shape of the remove_* tools — a single source of
// truth for both the emitted `{"removed":true}` JSON and the advertised
// outputSchema (WithOutputSchema[removalResult]).
type removalResult struct {
	Removed bool `json:"removed"`
}

// serviceMutationResult is what the four lifecycle mutations return: a summary
// of where the service ended up, rather than its entire specification.
//
// Two reasons. A task-augmented call's result is retained for as long as the
// task lives, and a client that omits task.ttl keeps it for the life of the
// process (see docs/mcp.md, "Always send task.ttl") — a full swarm.Service runs
// to kilobytes, so the compact shape bounds what a long-running agent
// accumulates. And it is the more useful answer: after a scale or a rollback an
// agent wants to know where the service got to, not to re-read a spec it just
// supplied. The spec-editing tools still return the full service, because there
// the resulting spec *is* the answer.
type serviceMutationResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Image is empty for a service whose task template is not a container.
	Image string `json:"image,omitempty"`
	// Mode is "replicated" or "global"; Replicas is unset for a global service,
	// which has no desired count to report.
	Mode     string  `json:"mode"`
	Replicas *uint64 `json:"replicas,omitempty"`
	Running  int     `json:"running"`
	// State is the same derivation the REST API and the dashboard report, via
	// cluster.DeriveServiceState.
	State string `json:"state"`
	// Version is the service's Swarm version index, for a caller doing its own
	// optimistic-concurrency checks.
	Version uint64 `json:"version"`
}

// serviceMutation summarises a mutated service. It prefers the cached copy over
// the one the Docker write returned: after a task waited for convergence the
// cache is the fresher of the two, and taking spec, running count and state
// from one source keeps them mutually consistent.
func (s *Server) serviceMutation(svc swarm.Service) serviceMutationResult {
	// svc is Docker's own post-mutation view: every caller passes the value
	// its write returned, and docker.Client produces that with a fresh
	// InspectService. Spec and version therefore come from the write itself.
	//
	// Only the running count is read from the cache, because Docker's service
	// object does not carry one. Reading the whole service from the cache —
	// which this used to do, for the consistency of describing one moment —
	// reported the state *before* the write instead: the cache is filled
	// asynchronously by the event watcher, so it still holds the previous
	// version when this runs. A caller that scaled 2 to 3 was told 3 tasks
	// were desired only on its *next* call, and the stale Version it got back
	// would collide on any follow-up write.
	running := s.cache.RunningTaskCount(svc.ID)

	out := serviceMutationResult{
		ID:      svc.ID,
		Name:    svc.Spec.Name,
		Running: running,
		State:   cluster.DeriveServiceState(svc, running),
		Version: svc.Version.Index,
	}

	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		out.Image = svc.Spec.TaskTemplate.ContainerSpec.Image
	}

	if svc.Spec.Mode.Global != nil {
		out.Mode = "global"

		return out
	}

	out.Mode = "replicated"
	if svc.Spec.Mode.Replicated != nil {
		out.Replicas = svc.Spec.Mode.Replicated.Replicas
	}

	return out
}

// textOnlyResultKey is the context key backing withTextOnlyResultSignal /
// markTextOnlyResult.
type textOnlyResultKey struct{}

// withTextOnlyResultSignal installs the opt-out flag registerTools reads once
// a handler returns. It travels on ctx rather than becoming a second return
// value every other handler would have to thread through unused.
func withTextOnlyResultSignal(ctx context.Context) (context.Context, *bool) {
	flag := new(bool)
	return context.WithValue(ctx, textOnlyResultKey{}, flag), flag
}

// markTextOnlyResult tells registerTools this call's result must be returned
// as text only, never as structuredContent, because — despite marshalling to
// a JSON object — it does not conform to the tool's declared outputSchema.
// The callers are find's and describe's raw modes: raw hands back whatever
// shape the underlying resource has, which is neither the compact Row shape
// find's schema describes nor the Digest describe's does, and
// structuredContent must conform under WithOutputSchemaValidation. A no-op if
// ctx was not set up by registerTools (e.g. a handler invoked directly, as
// the tool tests do), since there is nothing to signal.
func markTextOnlyResult(ctx context.Context) {
	if flag, ok := ctx.Value(textOnlyResultKey{}).(*bool); ok {
		*flag = true
	}
}

// structuredToolResult wraps a handler's JSON text into a tool result that
// carries both the text representation (a fallback for clients negotiating a
// pre-2025-06-18 protocol revision) and machine-parseable structuredContent
// (per the 2025-06-18+ structured-output contract). Every MCP tool marshals a
// JSON object; the bytes are passed through as json.RawMessage rather than
// decoded into a map and re-encoded — that round-trip is wasted work and
// silently rewrites the payload (e.g. integers above 2^53 lose precision once
// decoded into float64). If the text is not a JSON object the result degrades
// to text-only, since structuredContent must be an object.
func structuredToolResult(text string) *mcplib.CallToolResult {
	if trimmed := strings.TrimLeft(text, " \t\r\n"); trimmed == "" || trimmed[0] != '{' {
		return mcplib.NewToolResultText(text)
	}
	return mcplib.NewToolResultStructured(json.RawMessage(text), text)
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

// mergePatchMutator returns a MapMutator that applies a JSON Merge Patch
// (RFC 7396) to the live map handed to it by the writer. Nil entries delete,
// non-nil entries set. The mutator runs against the freshly-inspected spec
// inside the Docker writer, so concurrent third-party mutations to other keys
// are preserved.
func mergePatchMutator(
	patch map[string]*string,
) func(map[string]string) (map[string]string, error) {
	return func(current map[string]string) (map[string]string, error) {
		out := make(map[string]string, len(current)+len(patch))
		maps.Copy(out, current)
		for k, v := range patch {
			if v == nil {
				delete(out, k)
				continue
			}
			out[k] = *v
		}
		return out, nil
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
