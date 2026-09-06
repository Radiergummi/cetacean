package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	mcplib "github.com/mark3labs/mcp-go/mcp"

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
	return slices.Concat(
		s.readTools(),
		s.operationalTools(),
		s.configurationTools(),
		s.impactfulTools(),
	)
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

// --- tool handlers ---

func (s *Server) toolGetLogs(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	stack := strings.TrimSpace(req.GetString("stack", ""))
	clusterWide := req.GetBool("cluster", false)
	service := strings.TrimSpace(req.GetString("service", ""))
	task := strings.TrimSpace(req.GetString("task", ""))

	// Naming two scopes would leave the tool to guess which stream the caller
	// meant, and they differ: a service merges its live replicas, a task is one
	// replica including a dead one, and the two wide scopes fan out over many.
	// Checking them in order and returning from the first match would resolve
	// the conflict by the order they happen to be written in — a call passing
	// `service` and `cluster` would silently read the whole cluster.
	named := make([]string, 0, 4)

	for _, scope := range []struct {
		name  string
		given bool
	}{
		{"service", service != ""},
		{"task", task != ""},
		{"stack", stack != ""},
		{"cluster", clusterWide},
	} {
		if scope.given {
			named = append(named, "`"+scope.name+"`")
		}
	}

	switch {
	case len(named) == 0:
		return "", fmt.Errorf("one of `service`, `task`, `stack` or `cluster` is required")
	case len(named) > 1:
		return "", fmt.Errorf(
			"%s are mutually exclusive; name exactly one scope",
			strings.Join(named, ", "),
		)
	}

	if stack != "" {
		resp, err := s.readScopedLogs(ctx, "stack", stack, optsFromToolRequest(req))
		if err != nil {
			return "", err
		}

		return marshalResult(resp)
	}

	if clusterWide {
		resp, err := s.readScopedLogs(ctx, "cluster", "", optsFromToolRequest(req))
		if err != nil {
			return "", err
		}

		return marshalResult(resp)
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
