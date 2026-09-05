package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
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
	UpdateServiceHealthcheck(
		ctx context.Context,
		id string,
		hc *container.HealthConfig,
	) (swarm.Service, error)

	// UpdateServiceContainerConfig takes a mutator rather than a value
	// because it covers several unrelated container fields; the command
	// section writes two of them and must leave the rest untouched.
	UpdateServiceContainerConfig(
		ctx context.Context,
		id string,
		apply func(spec *swarm.ContainerSpec),
	) (swarm.Service, error)
	UpdateServiceSecrets(
		ctx context.Context,
		id string,
		secrets []*swarm.SecretReference,
	) (swarm.Service, error)
	UpdateServiceConfigs(
		ctx context.Context,
		id string,
		configs []*swarm.ConfigReference,
	) (swarm.Service, error)
	UpdateServiceMounts(
		ctx context.Context,
		id string,
		mounts []mount.Mount,
	) (swarm.Service, error)
}

// ResourceCreator creates the two resource types a service can reference by
// name. Narrow, like every other write interface here, so a test fake
// implements only what it exercises.
type ResourceCreator interface {
	CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
	CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error)
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
	"get_events":          "read",
	"get_cluster_status":  "read",
	"watch":               "read",
	"find":                "search",
	"describe":            "read",

	"scale_service": "scale",

	"create_secret":          "edit",
	"create_config":          "edit",
	"update_service_secrets": "edit",
	"update_service_configs": "edit",
	"update_service_mounts":  "edit",
	"update_service_image":   "edit",
	"rollback_service":       "edit",
	"restart_service":        "edit",
	"update_service":         "edit",

	"update_node_labels": "node",
	"update_node":        "node",

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
				ctx, annotations := withResultAnnotations(ctx)

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

				// Every result is structured: a tool that advertises an
				// output schema must return content conforming to it, so a
				// handler has no way to opt out of the shape it declared.
				return withResourceLinks(
					structuredToolResult(text),
					annotations.links,
				), nil
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
					"Fetch recent log lines from one service, one task, a whole stack, or the whole cluster. Name exactly one scope: `service` merges the output of every live replica; `task` reads a single replica and is the only way to reach one that has already exited; `stack` and `cluster` merge across services server-side, so grepping the cluster is one call rather than one per service, and every line says which service it came from. Swarm discards a task's output once its record falls out of the history window (five per replica slot by default), so on a service restarting in a loop that history is only seconds deep and should be read first. Narrow with `contains` for a substring, `level` for a minimum severity and `since` for a start time — on a wide read `contains` is what keeps the answer small. A wide read covers at most 25 services and reports any it could not reach in `errors` rather than failing. This is a one-shot read; for live tails subscribe to the cetacean://services/{id}/logs resource.",
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
				mcplib.WithString("stack",
					mcplib.Description(
						"Stack name; merges the logs of every service in it. Mutually exclusive with `service` and `task`.",
					),
				),
				mcplib.WithBoolean("cluster",
					mcplib.Description(
						"Read every service in the cluster, merged. Mutually exclusive with `service`, `task` and `stack`.",
					),
				),
				mcplib.WithString("contains",
					mcplib.Description(
						"Keep only lines containing this substring (case-insensitive). Applied server-side, so a wide read costs the matches rather than every line.",
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
					"Locate cluster resources. Give `type` (nodes, services, tasks, stacks, configs, secrets, networks, or volumes) to enumerate that type, paged, optionally narrowed by query/state/stack/node/image/label; the returned records are the same ones the cetacean:// resources expose, filtered to what the caller may see. Omit `type` and give `query` to search by name, label, or image reference across every type at once, returned as one list sorted by name, each row carrying its own type; there `limit` caps the matches per type and there is no paging, so `total` counts every match across the cluster and `counts` breaks it down per type — narrow with `type` to page through one of them. Use the cross-type search to locate a resource before fetching its details or applying a write; use a typed listing to browse or tabulate a whole type.",
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
						"With `type`, add each match's untouched resource record to the result, under `raw`, beside the compact rows. Default false.",
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
				mcplib.WithOutputSchema[describeResult](),
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
						"Add the untouched Docker record to the result, under `raw`, beside the digest. Default false.",
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
		{
			tool: mcplib.NewTool(
				"get_events",
				mcplib.WithToolTitle("Read the cluster change timeline"),
				mcplib.WithDescription(
					"Return what changed in the cluster and when — resource creates, updates and deletes — newest first, narrowed by time, type or a single resource. Use it to answer what happened overnight, whether a fault is new, and what else changed around the moment something broke; pair it with get_logs, which returns the same entry shape, to read changes and output on one timeline. Filtering by `types` is usually necessary: a service restarting in a loop produces a task event every few seconds and buries everything else.",
				),
				mcplib.WithOutputSchema[eventsResult](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("since",
					mcplib.Description(
						"RFC 3339 timestamp; only entries strictly after this time.",
					),
				),
				mcplib.WithString("until",
					mcplib.Description(
						"RFC 3339 timestamp; only entries at or before this time.",
					),
				),
				mcplib.WithArray("types",
					mcplib.Description(
						"Resource types to include: node, service, task, stack, config, secret, network, volume. Omit for all.",
					),
					mcplib.Items(map[string]any{"type": "string"}),
				),
				mcplib.WithString("resource",
					mcplib.Description("Only entries for this resource ID."),
				),
				mcplib.WithNumber("limit",
					mcplib.Description(
						"Maximum entries to return (default 100, maximum 500).",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetEvents,
		},
		{
			tool: mcplib.NewTool(
				"get_cluster_status",
				mcplib.WithToolTitle("Check overall cluster health"),
				mcplib.WithDescription(
					"Answer whether the cluster is healthy and, when it is not, name what is wrong: the services not in their desired state, the nodes that are down or draining, the rollouts in flight, and how much CPU and memory is reserved against what the cluster has. Start here — it is the one call that says whether to look further and where, and every entry carries the id and name to describe next.",
				),
				mcplib.WithOutputSchema[cluster.ClusterStatus](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolGetClusterStatus,
		},
		{
			tool: mcplib.NewTool(
				"watch",
				mcplib.WithToolTitle("Wait for a service to settle"),
				mcplib.WithDescription(
					"Block until a service has reached its desired state — every replica running and no rolling update in flight — then report whether it settled and how long it took. Use it after a deploy or a scale instead of describing the service in a loop. On a timeout it reports how far the rollout actually got (\"waiting: 1/3 replicas running\"), which is the answer when a deploy is stuck. The wait cannot be cancelled once started, so `timeout` is the real bound; it defaults to 60 seconds and is capped at 5 minutes.",
				),
				mcplib.WithOutputSchema[watchResult](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("service",
					mcplib.Required(),
					mcplib.Description("Service ID or name to wait on."),
				),
				mcplib.WithNumber("timeout",
					mcplib.Description(
						"Seconds to wait before giving up (default 60, maximum 300).",
					),
				),
			),
			tier:    config.OpsReadOnly,
			handler: s.toolWatch,
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
				"update_service",
				mcplib.WithToolTitle("Update a service's specification"),
				mcplib.WithDescription(
					"Change one section of a service's specification, named by `section`. `env` and `labels` take a JSON Merge Patch (RFC 7396) object — a string sets or replaces a key, null deletes it, an omitted key is preserved. The other six replace their section wholesale: pass the complete object, because a field you omit is cleared. `resources` takes ResourceRequirements (CPU/memory limits and reservations); `placement` takes Placement (constraints, preferences, max replicas per node, platforms) and may reschedule tasks onto other nodes to satisfy new constraints; `ports` takes an array of PortConfig and drops connections to any port it removes or remaps; `update-policy` and `rollback-policy` take an UpdateConfig (parallelism, delay, failure action, monitor window, max failure ratio, order) and change nothing by themselves, applying to the next spec change and to rollback_service respectively; `log-driver` takes a Driver (name plus options) and routes subsequent log lines through it. `healthcheck` takes a probe: `test` is the command as an array, for instance [`CMD`, `curl`, `-f`, `http://localhost/`], and `interval`, `timeout` and `startPeriod` are duration strings such as `10s` or `1m30s` rather than numbers; an empty `test` removes the healthcheck. `command` takes `command` (the entrypoint) and `args` (what follows it), the split Docker itself makes, and omitting one clears it. Every section except labels and the two policies triggers a rolling deploy. Returns the section as it now stands, not the whole service — describe the service for the rest.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				// Destructive because two of the sections are: replacing the
				// ports drops the connections to a port it removes, and
				// replacing the placement can evict running tasks. One
				// annotation covers every section, so it has to describe the
				// worst of them — a host deciding whether to confirm must not
				// be told a port remap is safe because a label edit is.
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithString("section",
					mcplib.Required(),
					mcplib.Enum(
						sectionEnv,
						sectionLabels,
						sectionResources,
						sectionPlacement,
						sectionPorts,
						sectionUpdatePolicy,
						sectionRollbackPolicy,
						sectionLogDriver,
						sectionHealthcheck,
						sectionCommand,
					),
					mcplib.Description("Which part of the specification to change."),
				),
				mcplib.WithAny("value",
					mcplib.Required(),
					mcplib.Description(
						"The section's new value: a merge-patch object for `env` and `labels`, an array of PortConfig for `ports`, and the replacement object for the rest. See the tool description for the shape each section expects.",
					),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateService,
		},
		{
			tool: mcplib.NewTool("update_node_labels",
				mcplib.WithToolTitle("Update node labels"),
				mcplib.WithDescription(
					"Patch the labels on a node using JSON Merge Patch (RFC 7396) semantics: string values set or replace a key, null values delete it, omitted keys are preserved. Node labels are commonly used as service placement constraints. Returns the updated node spec.",
				),
				mcplib.WithOutputSchema[nodeUpdateResult](),
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
				"create_secret",
				mcplib.WithToolTitle("Create a secret"),
				mcplib.WithDescription(
					"Create a Swarm secret. Swarm secrets cannot be changed once created, so rotating one is three calls in order: this tool for the replacement, update_service_secrets to repoint each service that uses the old one (describe the old secret first — its related array names them), then remove_secret once nothing references it. The value is write-only: no tool ever returns it, including this one. Pass encoding as base64 for binary content or anything JSON string escaping would mangle, such as a certificate or a key file; without it the value is stored exactly as given, since plenty of ordinary passwords are also valid base64 and guessing would silently store the wrong thing.",
				),
				mcplib.WithOutputSchema[createResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString(
					"name",
					mcplib.Required(),
					mcplib.Description(
						"Name for the new secret. Must not already exist in the cluster.",
					),
				),
				mcplib.WithString("data",
					mcplib.Required(),
					mcplib.Description("The secret's value."),
				),
				mcplib.WithString(
					"encoding",
					mcplib.Enum("utf8", "base64"),
					mcplib.Description(
						"How `data` is encoded. Defaults to utf8, which stores it verbatim.",
					),
				),
				mcplib.WithObject(
					"labels",
					mcplib.Description(
						"Labels to set on the secret. Set com.docker.stack.namespace to attach it to a stack.",
					),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolCreateSecret,
		},
		{
			tool: mcplib.NewTool(
				"create_config",
				mcplib.WithToolTitle("Create a config"),
				mcplib.WithDescription(
					"Create a Swarm config. Like a secret, a config cannot be changed once created — replace it by creating a new one, repointing services with update_service_configs, and removing the old one. Unlike a secret, its content can be read back afterwards, so use a secret for anything sensitive. Pass encoding as base64 for a file whose content JSON string escaping would mangle.",
				),
				mcplib.WithOutputSchema[createResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(false),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString(
					"name",
					mcplib.Required(),
					mcplib.Description(
						"Name for the new config. Must not already exist in the cluster.",
					),
				),
				mcplib.WithString("data",
					mcplib.Required(),
					mcplib.Description("The config's content."),
				),
				mcplib.WithString(
					"encoding",
					mcplib.Enum("utf8", "base64"),
					mcplib.Description(
						"How `data` is encoded. Defaults to utf8, which stores it verbatim.",
					),
				),
				mcplib.WithObject(
					"labels",
					mcplib.Description(
						"Labels to set on the config. Set com.docker.stack.namespace to attach it to a stack.",
					),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolCreateConfig,
		},
		{
			tool: mcplib.NewTool(
				"update_service_secrets",
				mcplib.WithToolTitle("Change which secrets a service receives"),
				mcplib.WithDescription(
					"Replace the complete set of secrets a service's containers receive. Swarm secrets cannot be changed in place, so rotating one is: create_secret for the replacement, this tool to repoint each service that uses the old one, then remove_secret once nothing references it. Describe the old secret first — its related array names the services to repoint. The list replaces rather than merges: pass every secret the service should end up with, because one you leave out is detached and the container loses it. Each entry names a secret rather than giving its ID. Triggers a rolling deploy, and you must be permitted to read a secret in order to attach it.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithArray("secrets",
					mcplib.Required(),
					mcplib.Description(
						"The complete set of secrets the service should receive. Each entry takes `name` (the secret's name) and an optional `target` (the file it is mounted as, relative to /run/secrets unless absolute; defaults to the secret's name). An empty array detaches every secret.",
					),
					mcplib.Items(map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":   map[string]any{"type": "string"},
							"target": map[string]any{"type": "string"},
						},
						"required": []string{"name"},
					}),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceSecrets,
		},
		{
			tool: mcplib.NewTool(
				"update_service_configs",
				mcplib.WithToolTitle("Change which configs a service receives"),
				mcplib.WithDescription(
					"Replace the complete set of configs a service's containers receive. Like secrets, Swarm configs cannot be changed in place: create the replacement with create_config, repoint the services here, then remove the old one. The list replaces rather than merges, so pass every config the service should end up with — one you leave out is detached. Each entry names a config rather than giving its ID. Triggers a rolling deploy, and you must be permitted to read a config in order to attach it.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithArray("configs",
					mcplib.Required(),
					mcplib.Description(
						"The complete set of configs the service should receive. Each entry takes `name` (the config's name) and an optional `target` (the absolute path it is mounted at; defaults to the config's name). An empty array detaches every config.",
					),
					mcplib.Items(map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":   map[string]any{"type": "string"},
							"target": map[string]any{"type": "string"},
						},
						"required": []string{"name"},
					}),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceConfigs,
		},
		{
			tool: mcplib.NewTool(
				"update_service_mounts",
				mcplib.WithToolTitle("Change a service's filesystem mounts"),
				mcplib.WithDescription(
					"Replace the complete set of filesystem mounts a service's containers receive: named volumes, host bind mounts and tmpfs. The list replaces rather than merges, so pass every mount the service should end up with — one you leave out is unmounted, and a container may lose data it was writing there. Triggers a rolling deploy. Note that a bind mount hands the container the host's filesystem at that path, and binding the Docker socket (/var/run/docker.sock) gives it control of the whole cluster; this tool will do it if asked, so check with the operator before mounting a host path they did not name.",
				),
				mcplib.WithOutputSchema[serviceUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Service ID or name."),
				),
				mcplib.WithArray("mounts",
					mcplib.Required(),
					mcplib.Description(
						"The complete set of mounts the service should receive. Each entry takes `type` (volume, bind or tmpfs), `target` (the absolute path inside the container), an optional `source` (the volume name for a volume, the host path for a bind; a tmpfs has none) and an optional `readOnly`. An empty array unmounts everything.",
					),
					mcplib.Items(map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type": map[string]any{
								"type": "string",
								"enum": []string{
									string(mount.TypeVolume),
									string(mount.TypeBind),
									string(mount.TypeTmpfs),
								},
							},
							"source":   map[string]any{"type": "string"},
							"target":   map[string]any{"type": "string"},
							"readOnly": map[string]any{"type": "boolean"},
						},
						"required": []string{"type", "target"},
					}),
				),
			),
			tier:    config.OpsConfiguration,
			handler: s.toolUpdateServiceMounts,
		},

		// Tier 3 — Impactful.
		{
			tool: mcplib.NewTool(
				"update_node",
				mcplib.WithToolTitle("Update a node's role or availability"),
				mcplib.WithDescription(
					"Change a node's scheduling state, named by `section`. `availability` takes one of `active` (accepts new tasks), `pause` (rejects new tasks, keeps existing ones running) or `drain` (evicts every task to other nodes); draining a manager may destabilise the swarm, so prefer draining workers. `role` takes `worker` or `manager`: promoting adds a Raft voter, demoting removes one — never reduce the manager count below the quorum threshold, and for a three-manager cluster demote at most one at a time. Returns the node's role and availability as they now stand. Node labels are edited with update_node_labels, which needs a lower operations level than this.",
				),
				mcplib.WithOutputSchema[nodeUpdateResult](),
				mcplib.WithReadOnlyHintAnnotation(false),
				mcplib.WithDestructiveHintAnnotation(true),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("id",
					mcplib.Required(),
					mcplib.Description("Node ID or hostname."),
				),
				mcplib.WithString("section",
					mcplib.Required(),
					mcplib.Enum(sectionAvailability, sectionRole),
					mcplib.Description("Which part of the node's scheduling state to change."),
				),
				mcplib.WithString("value",
					mcplib.Required(),
					mcplib.Description(
						"For `availability`: active, pause or drain. For `role`: worker or manager.",
					),
				),
			),
			tier:    config.OpsImpactful,
			handler: s.toolUpdateNode,
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

// --- tool handlers ---

func (s *Server) toolGetLogs(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	if stack := req.GetString("stack", ""); stack != "" {
		resp, err := s.readScopedLogs(ctx, "stack", stack, optsFromToolRequest(req))
		if err != nil {
			return "", err
		}

		return marshalResult(resp)
	}

	if req.GetBool("cluster", false) {
		resp, err := s.readScopedLogs(ctx, "cluster", "", optsFromToolRequest(req))
		if err != nil {
			return "", err
		}

		return marshalResult(resp)
	}

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
	return marshalResult(nodeUpdate(sectionLabels, updated))
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

// resultAnnotationsKey is the context key backing withResultAnnotations and
// the function handlers use to write to it.
type resultAnnotationsKey struct{}

// resultAnnotations is what a handler learned about its own result and cannot
// express in the string it returns. registerTools is the only place a
// CallToolResult is assembled, so this is how the two halves meet.
type resultAnnotations struct {
	// links are the cetacean:// resources the result refers to, offered to
	// the client as resource_link content — see attachResourceLinks.
	links []mcplib.ResourceLink
}

// withResultAnnotations installs the block registerTools reads once a handler
// returns. It travels on ctx rather than becoming a second return value every
// other handler would have to thread through unused.
func withResultAnnotations(ctx context.Context) (context.Context, *resultAnnotations) {
	annotations := new(resultAnnotations)

	return context.WithValue(ctx, resultAnnotationsKey{}, annotations), annotations
}

// resultAnnotationsFrom recovers the block, or nil when ctx was not set up by
// registerTools — which is the case whenever a handler is invoked directly, as
// the tool tests do. The writer below is then a no-op, since there is nothing
// on the other end to read it.
func resultAnnotationsFrom(ctx context.Context) *resultAnnotations {
	annotations, _ := ctx.Value(resultAnnotationsKey{}).(*resultAnnotations)

	return annotations
}

// attachResourceLinks offers the cetacean:// resources a result refers to as
// resource_link content items, so a host can render them as somewhere to go
// next and a client can resources/read one without the model first working
// out how to spell the URI.
//
// They ride alongside the result rather than inside it: the shapes find and
// describe advertise as output schemas describe cluster resources, and a link
// is a statement about where to read one — the spec gives content items for
// exactly that, and putting URIs in the schema would make every widget and
// every consumer of structuredContent carry them too.
func attachResourceLinks(ctx context.Context, links []mcplib.ResourceLink) {
	if annotations := resultAnnotationsFrom(ctx); annotations != nil {
		annotations.links = links
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
