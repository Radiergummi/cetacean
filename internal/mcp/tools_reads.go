package mcp

import (
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// readTools returns the tier 0 tools: parameterized reads, ungated by
// operations level. Each ACL-filters its own results, so a caller with no
// grants sees them in tools/list and gets empty answers rather than errors.
//
// See toolCatalog for the conventions every entry here follows.
func (s *Server) readTools() []toolDef {
	return []toolDef{
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
					"Return the cluster as a graph. The network view joins services to the overlay networks they attach to, answering which services can reach each other; the placement view joins cluster nodes to the services they run, answering what runs where; the drain-impact view takes a `node` and joins the services running on it to the nodes that could take them, answering what would move if you drained it and what could not. In drain-impact a service with no edges is stranded and its `detail` names the placement constraint that blocked it — check this before draining a node rather than discovering it afterwards. Use this instead of listing services and nodes separately when the question is about relationships.",
				),
				mcplib.WithOutputSchema[cluster.TopologyGraph](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString(
					"view",
					mcplib.Description(
						"Which projection to return: \"network\" (services joined to overlay networks, the default), \"placement\" (cluster nodes joined to the services they run), or \"drain-impact\" (the services on one node joined to the nodes that could take them; requires `node`).",
					),
				),
				mcplib.WithString(
					"node",
					mcplib.Description(
						"Node ID or hostname to assess. Required by the \"drain-impact\" view and ignored by the others.",
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
					"Return a time series of CPU, memory or network use, over the last hour, six hours, day or week. Chart one resource by naming `target` \"service\" or \"node\" with its `id`. Rank instead by asking for `top`: `target` \"cluster\" ranks the busiest services cluster-wide (or nodes, with `by`), and `target` \"node\" with an `id` and `top` ranks the services running on that one host — which is how to answer what is using the most CPU, and why a given node is hot. A ranking returns one series per member, named the way the cluster names it. Requires Prometheus (and cAdvisor for service metrics, node-exporter for node metrics); a Cetacean without them reports that metrics are unavailable rather than returning empty series. Cetacean owns the queries — name a target and a metric, not PromQL.",
				),
				mcplib.WithOutputSchema[metricsResult](),
				mcplib.WithReadOnlyHintAnnotation(true),
				mcplib.WithDestructiveHintAnnotation(false),
				mcplib.WithIdempotentHintAnnotation(true),
				mcplib.WithOpenWorldHintAnnotation(false),
				mcplib.WithString("target",
					mcplib.Required(),
					mcplib.Description(
						"What to measure: \"service\", \"node\", or \"cluster\" to rank across the whole cluster.",
					),
				),
				mcplib.WithString("id",
					mcplib.Description(
						"The service (ID or name) or node (ID or hostname) to measure. Required for the \"service\" and \"node\" targets; the \"cluster\" target takes none.",
					),
				),
				mcplib.WithNumber("top",
					mcplib.Description(
						"Return a ranking of the busiest members rather than one resource's own series: the top N (default 5, maximum 10). With target \"cluster\" it ranks the whole cluster; with target \"node\" it ranks the services running on that node.",
					),
				),
				mcplib.WithString("by",
					mcplib.Description(
						"What a cluster-wide ranking ranks: \"service\" (default) or \"node\". Ignored by every other target.",
					),
				),
				mcplib.WithString("metric",
					mcplib.Description(
						"Which metric: \"cpu\" (default), \"memory\" or \"network\". Service CPU is percent of a core and service memory is bytes; node CPU and memory are percentages. Network is two series, receive and transmit, in bytes per second — a ranking sums the two, since it asks which member moves the most traffic.",
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
					"Return what changed in the cluster and when — resource creates, updates and deletes — newest first, narrowed by time, type or a single resource. Use it to answer what happened overnight, whether a fault is new, and what else changed around the moment something broke; pair it with get_logs, whose lines carry timestamps in the same format, to read changes and output on one timeline — merge the two on time, not by field name, since an event is `at`/`kind`/`type`/`name`/`resourceId`/`message` and a log line is `timestamp`/`message`/`stream`/`attrs`. Filtering by `types` is usually necessary: a service restarting in a loop produces a task event every few seconds and buries everything else.",
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
	}
}
