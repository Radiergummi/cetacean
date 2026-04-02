package api

import (
	"net/http"
	"sort"

	json "github.com/goccy/go-json"
)

// ErrorDef defines a well-known error type with a stable code, human-readable
// description, and suggested resolution. Each error is served as documentation
// at GET /api/errors/{code}.
type ErrorDef struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Status      int    `json:"status"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
}

// Error codes by domain prefix:
//
//	API — API-level / protocol errors
//	AUT — authentication
//	OPS — operations level
//	FLT — filter expressions
//	SEA — search
//	MTR — metrics / Prometheus
//	LOG — log streaming
//	SSE — SSE streaming
//	ENG — Docker Engine level
//	SWM — swarm operations
//	PLG — plugin operations
//	NOD — node operations
//	SVC — service operations
//	TSK — task operations
//	STK — stack operations
//	VOL — volume operations
//	NET — network operations
//	CFG — config operations
//	SEC — secret operations
var errorRegistry = map[string]ErrorDef{
	// ── API: protocol / content negotiation ────────────────────────────
	"API001": {Code: "API001", Title: "SSE Not Supported", Status: http.StatusNotAcceptable,
		Description: "This endpoint does not support Server-Sent Events.",
		Suggestion:  "Use Accept: application/json instead of text/event-stream."},
	"API002": {Code: "API002", Title: "SSE Required", Status: http.StatusNotAcceptable,
		Description: "This endpoint only supports Server-Sent Events.",
		Suggestion:  "Use Accept: text/event-stream."},
	"API003": {Code: "API003", Title: "Not Acceptable", Status: http.StatusNotAcceptable,
		Description: "The Accept header does not match any media type this endpoint can produce.",
		Suggestion:  "Use Accept: application/json, application/atom+xml, text/event-stream, or text/html."},
	"API004": {
		Code:        "API004",
		Title:       "Invalid Patch Content-Type",
		Status:      http.StatusUnsupportedMediaType,
		Description: "The Content-Type header does not match a supported patch format.",
		Suggestion:  "Use Content-Type: application/merge-patch+json or application/json-patch+json.",
	},
	"API005": {
		Code:        "API005",
		Title:       "Streaming Not Supported",
		Status:      http.StatusInternalServerError,
		Description: "The server's response writer does not support streaming (no http.Flusher).",
		Suggestion:  "This is a server configuration issue. Check that no middleware is buffering responses.",
	},
	"API006": {Code: "API006", Title: "Invalid Request Body", Status: http.StatusBadRequest,
		Description: "The request body could not be decoded as valid JSON.",
		Suggestion:  "Ensure the request body is well-formed JSON matching the expected schema."},
	"API007": {Code: "API007", Title: "Unreadable Request Body", Status: http.StatusBadRequest,
		Description: "The request body could not be read.",
		Suggestion:  "Ensure the request includes a body and Content-Length is correct."},
	"API008": {Code: "API008", Title: "Invalid JSON", Status: http.StatusBadRequest,
		Description: "The request body is not valid JSON.",
		Suggestion:  "Check for syntax errors in the JSON payload."},
	"API009": {
		Code:        "API009",
		Title:       "Internal Serialization Error",
		Status:      http.StatusInternalServerError,
		Description: "The server failed to serialize or deserialize internal state.",
		Suggestion:  "This is a server bug. Check the Cetacean logs for details.",
	},
	"API010": {
		Code:        "API010",
		Title:       "Patch Test Failed",
		Status:      http.StatusConflict,
		Description: "A JSON Patch test operation failed, indicating the resource state does not match the expected value.",
		Suggestion:  "Reload the resource and retry the patch with updated test values.",
	},
	"API011": {Code: "API011", Title: "Patch Application Failed", Status: http.StatusBadRequest,
		Description: "The JSON Patch could not be applied to the resource.",
		Suggestion:  "Check the patch operations for correctness."},

	// ── AUT: authentication ───────────────────────────────────────────
	"AUT001": {Code: "AUT001", Title: "Not Authenticated", Status: http.StatusUnauthorized,
		Description: "The request requires authentication but no valid credentials were provided.",
		Suggestion:  "Log in or provide a valid authentication token."},
	"AUT002": {Code: "AUT002", Title: "Authorization Denied", Status: http.StatusForbidden,
		Description: "The identity provider denied authorization.",
		Suggestion:  "Check your account permissions with the identity provider."},
	"AUT003": {
		Code:        "AUT003",
		Title:       "Authentication Callback Error",
		Status:      http.StatusBadRequest,
		Description: "The authentication callback contained invalid or missing parameters.",
		Suggestion:  "Retry the login flow from the beginning.",
	},
	"AUT004": {
		Code:        "AUT004",
		Title:       "Authentication Server Error",
		Status:      http.StatusInternalServerError,
		Description: "An internal error occurred during authentication.",
		Suggestion:  "Retry the login flow. If the problem persists, check server logs.",
	},

	// ── ACL: access control ──────────────────────────────────────────
	"ACL001": {Code: "ACL001", Title: "Access Denied", Status: http.StatusForbidden,
		Description: "You do not have permission to access this resource.",
		Suggestion:  "Check your ACL policy grants."},
	"ACL002": {Code: "ACL002", Title: "Write Access Denied", Status: http.StatusForbidden,
		Description: "You do not have write permission on this resource.",
		Suggestion:  "Check your ACL policy grants for write permissions."},

	// ── OPS: operations level ─────────────────────────────────────────
	"OPS001": {
		Code:        "OPS001",
		Title:       "Operations Level Too Low",
		Status:      http.StatusForbidden,
		Description: "The requested operation requires a higher operations level than the server is configured for.",
		Suggestion:  "Increase the server.operations_level setting and restart the server.",
	},

	// ── FLT: filter expressions ───────────────────────────────────────
	"FLT001": {Code: "FLT001", Title: "Filter Expression Too Long", Status: http.StatusBadRequest,
		Description: "The filter expression exceeds the maximum allowed length.",
		Suggestion:  "Shorten the filter expression."},
	"FLT002": {Code: "FLT002", Title: "Invalid Filter Expression", Status: http.StatusBadRequest,
		Description: "The filter expression could not be compiled.",
		Suggestion:  "Check the expression syntax. Filters use the expr-lang expression language."},
	"FLT003": {Code: "FLT003", Title: "Filter Evaluation Error", Status: http.StatusBadRequest,
		Description: "The filter expression compiled but failed during evaluation.",
		Suggestion:  "Check that the expression references valid fields for this resource type."},

	// ── SEA: search ───────────────────────────────────────────────────
	"SEA001": {Code: "SEA001", Title: "Missing Search Query", Status: http.StatusBadRequest,
		Description: "The required query parameter q is missing.",
		Suggestion:  "Provide a search query: /search?q=term."},
	"SEA002": {Code: "SEA002", Title: "Search Query Too Long", Status: http.StatusBadRequest,
		Description: "The search query exceeds the maximum allowed length of 200 characters.",
		Suggestion:  "Shorten the search query."},

	// ── MTR: metrics / Prometheus ─────────────────────────────────────
	"MTR001": {
		Code:        "MTR001",
		Title:       "Prometheus Not Configured",
		Status:      http.StatusServiceUnavailable,
		Description: "Prometheus metrics are not available because no Prometheus URL is configured.",
		Suggestion:  "Set the prometheus.url setting and restart the server.",
	},
	"MTR002": {Code: "MTR002", Title: "Prometheus Unreachable", Status: http.StatusBadGateway,
		Description: "The configured Prometheus server is not responding.",
		Suggestion:  "Check that Prometheus is running and reachable at the configured URL."},
	"MTR003": {Code: "MTR003", Title: "Missing Metrics Query", Status: http.StatusBadRequest,
		Description: "The required query parameter is missing for the metrics endpoint.",
		Suggestion:  "Provide a PromQL query parameter."},
	"MTR004": {Code: "MTR004", Title: "Invalid Metrics Step", Status: http.StatusBadRequest,
		Description: "The step parameter is outside the allowed range.",
		Suggestion:  "Use a step value between 5 and 300 seconds."},
	"MTR005": {
		Code:        "MTR005",
		Title:       "Too Many Metrics Streams",
		Status:      http.StatusTooManyRequests,
		Description: "The maximum number of concurrent metrics stream connections has been reached.",
		Suggestion:  "Close an existing metrics stream connection before opening a new one.",
	},
	"MTR006": {Code: "MTR006", Title: "Missing Label Name", Status: http.StatusBadRequest,
		Description: "The label name path parameter is missing.",
		Suggestion:  "Provide a label name in the URL path."},
	"MTR007": {
		Code:        "MTR007",
		Title:       "Prometheus Request Failed",
		Status:      http.StatusInternalServerError,
		Description: "Failed to create the request to the Prometheus server.",
		Suggestion:  "This is a server-side error. Check the Cetacean logs for details.",
	},

	// ── LOG: log streaming ────────────────────────────────────────────
	"LOG001": {Code: "LOG001", Title: "Too Many Log Streams", Status: http.StatusTooManyRequests,
		Description: "The maximum number of concurrent log stream connections has been reached.",
		Suggestion:  "Close an existing log stream before opening a new one."},
	"LOG002": {Code: "LOG002", Title: "Invalid Stream Parameter", Status: http.StatusBadRequest,
		Description: "The stream parameter must be either stdout or stderr.",
		Suggestion:  "Use stream=stdout or stream=stderr."},
	"LOG003": {Code: "LOG003", Title: "Invalid After Parameter", Status: http.StatusBadRequest,
		Description: "The after parameter must be an RFC 3339 timestamp or a Go duration string.",
		Suggestion:  "Use a format like 2024-01-01T00:00:00Z or 1h30m."},
	"LOG004": {Code: "LOG004", Title: "Invalid Before Parameter", Status: http.StatusBadRequest,
		Description: "The before parameter must be an RFC 3339 timestamp or a Go duration string.",
		Suggestion:  "Use a format like 2024-01-01T00:00:00Z or 1h30m."},
	"LOG005": {
		Code:        "LOG005",
		Title:       "Before Not Supported For SSE",
		Status:      http.StatusBadRequest,
		Description: "The before parameter is not supported for SSE log streams because they are open-ended.",
		Suggestion:  "Remove the before parameter when using SSE, or use a JSON request instead.",
	},
	"LOG006": {
		Code:        "LOG006",
		Title:       "Log Retrieval Failed",
		Status:      http.StatusInternalServerError,
		Description: "Failed to retrieve logs from the Docker Engine.",
		Suggestion:  "Check that the service or task still exists and the Docker Engine is reachable.",
	},
	"LOG007": {Code: "LOG007", Title: "Log Parse Failed", Status: http.StatusInternalServerError,
		Description: "The logs were retrieved but could not be parsed.",
		Suggestion:  "This is a server-side error. Check the Cetacean logs for details."},
	"LOG008": {
		Code:        "LOG008",
		Title:       "Log Stream Failed",
		Status:      http.StatusInternalServerError,
		Description: "Failed to open the log stream from the Docker Engine.",
		Suggestion:  "Check that the service or task still exists and the Docker Engine is reachable.",
	},

	// ── SSE: SSE connections ──────────────────────────────────────────
	"SSE001": {
		Code:        "SSE001",
		Title:       "Too Many SSE Connections",
		Status:      http.StatusTooManyRequests,
		Description: "The maximum number of concurrent SSE connections has been reached.",
		Suggestion:  "Close an existing SSE connection before opening a new one.",
	},

	// ── ENG: Docker Engine ────────────────────────────────────────────
	"ENG001": {
		Code:        "ENG001",
		Title:       "Docker Engine Unavailable",
		Status:      http.StatusServiceUnavailable,
		Description: "The Docker Engine is not responding. The daemon may be stopped, restarting, or the socket may be unreachable.",
		Suggestion:  "Check that the Docker daemon is running and that Cetacean has access to the Docker socket.",
	},
	"ENG002": {
		Code:        "ENG002",
		Title:       "Docker Version Check Failed",
		Status:      http.StatusServiceUnavailable,
		Description: "Could not determine the latest Docker Engine version from the GitHub API.",
		Suggestion:  "This is a transient network error. Try again later.",
	},
	"ENG003": {Code: "ENG003", Title: "Docker Validation Error", Status: http.StatusBadRequest,
		Description: "The Docker Engine rejected the request due to invalid arguments.",
		Suggestion:  "Check the request parameters for correctness."},
	"ENG004": {Code: "ENG004", Title: "Docker Engine Error", Status: http.StatusInternalServerError,
		Description: "An unexpected error occurred while communicating with the Docker Engine.",
		Suggestion:  "Check the Cetacean and Docker daemon logs for details."},
	// ── SWM: swarm operations ─────────────────────────────────────────
	"SWM001": {
		Code:        "SWM001",
		Title:       "Swarm API Not Available",
		Status:      http.StatusNotImplemented,
		Description: "The swarm API is not available. This node may not be a swarm manager, or the Docker Engine may not support swarm mode.",
		Suggestion:  "Ensure Cetacean is connected to a swarm manager node.",
	},
	"SWM002": {
		Code:        "SWM002",
		Title:       "Swarm Inspect Failed",
		Status:      http.StatusServiceUnavailable,
		Description: "Failed to inspect the current swarm state. The swarm may be temporarily unavailable.",
		Suggestion:  "Check that the swarm is healthy and retry the operation.",
	},
	"SWM003": {Code: "SWM003", Title: "Swarm Update Failed", Status: http.StatusInternalServerError,
		Description: "The swarm configuration update failed.",
		Suggestion:  "Check the Cetacean and Docker daemon logs for details."},
	"SWM004": {
		Code:        "SWM004",
		Title:       "Disk Usage Not Available",
		Status:      http.StatusNotImplemented,
		Description: "Disk usage information is not available from the Docker Engine.",
		Suggestion:  "Ensure Cetacean is connected to a Docker Engine that supports the disk usage API.",
	},
	"SWM005": {Code: "SWM005", Title: "Disk Usage Failed", Status: http.StatusInternalServerError,
		Description: "Failed to retrieve disk usage information from the Docker Engine.",
		Suggestion:  "Check the Docker daemon logs for details."},
	"SWM006": {
		Code:        "SWM006",
		Title:       "Token Rotation Failed",
		Status:      http.StatusInternalServerError,
		Description: "Failed to rotate the swarm join token.",
		Suggestion:  "Check the Docker daemon logs for details.",
	},
	"SWM007": {
		Code:        "SWM007",
		Title:       "Unlock Key Rotation Failed",
		Status:      http.StatusInternalServerError,
		Description: "Failed to rotate the swarm unlock key.",
		Suggestion:  "Check the Docker daemon logs for details.",
	},
	"SWM008": {Code: "SWM008", Title: "Swarm Unlock Failed", Status: http.StatusInternalServerError,
		Description: "Failed to unlock the swarm with the provided key.",
		Suggestion:  "Verify the unlock key is correct and try again."},
	"SWM009": {
		Code:        "SWM009",
		Title:       "Unlock Key Retrieval Failed",
		Status:      http.StatusInternalServerError,
		Description: "Failed to retrieve the swarm unlock key.",
		Suggestion:  "Check the Docker daemon logs for details.",
	},
	"SWM010": {Code: "SWM010", Title: "Unlock Key Required", Status: http.StatusBadRequest,
		Description: "The unlock key is required to unlock the swarm.",
		Suggestion:  "Provide the unlockKey field in the request body."},
	"SWM011": {Code: "SWM011", Title: "Invalid Token Target", Status: http.StatusBadRequest,
		Description: "The token rotation target must be either worker or manager.",
		Suggestion:  "Use target=worker or target=manager."},

	// ── PLG: plugin operations ────────────────────────────────────────
	"PLG001": {Code: "PLG001", Title: "Plugin List Failed", Status: http.StatusInternalServerError,
		Description: "Failed to list plugins from the Docker Engine.",
		Suggestion:  "Check that the Docker daemon is running and reachable."},
	"PLG002": {Code: "PLG002", Title: "Plugin Remote Required", Status: http.StatusBadRequest,
		Description: "The remote field is required for plugin operations.",
		Suggestion:  "Provide the remote field specifying the plugin image reference."},
	"PLG003": {
		Code:        "PLG003",
		Title:       "Plugin Privilege Check Failed",
		Status:      http.StatusInternalServerError,
		Description: "Failed to check the required privileges for the plugin.",
		Suggestion:  "Check the Docker daemon logs for details.",
	},
	"PLG004": {Code: "PLG004", Title: "Plugin Not Found", Status: http.StatusNotFound,
		Description: "The requested plugin does not exist on this Docker host.",
		Suggestion:  "Check the plugin name and verify it is installed."},

	// ── NOD: node operations ──────────────────────────────────────────
	"NOD001": {
		Code:        "NOD001",
		Title:       "Node Not Down",
		Status:      http.StatusConflict,
		Description: "The node cannot be removed because it is not in the down state. A node must be drained and marked as down before it can be removed from the swarm.",
		Suggestion:  "Drain the node first by setting its availability to drain, then wait for it to reach the down state. Alternatively, use force removal to remove the node immediately.",
	},
	"NOD002": {
		Code:        "NOD002",
		Title:       "Node Version Conflict",
		Status:      http.StatusConflict,
		Description: "The node was modified by another client between the time it was read and the time the update was submitted.",
		Suggestion:  "Reload the node and retry the operation.",
	},
	"NOD003": {Code: "NOD003", Title: "Node Not Found", Status: http.StatusNotFound,
		Description: "The specified node does not exist in the swarm.",
		Suggestion:  "Check the node ID or hostname and try again."},
	"NOD004": {Code: "NOD004", Title: "Invalid Availability Value", Status: http.StatusBadRequest,
		Description: "The availability value must be one of: active, drain, pause.",
		Suggestion:  "Use availability=active, availability=drain, or availability=pause."},
	"NOD005": {Code: "NOD005", Title: "Invalid Role Value", Status: http.StatusBadRequest,
		Description: "The role value must be one of: worker, manager.",
		Suggestion:  "Use role=worker or role=manager."},

	// ── SVC: service operations ───────────────────────────────────────
	"SVC001": {
		Code:        "SVC001",
		Title:       "Service Version Conflict",
		Status:      http.StatusConflict,
		Description: "The service was modified by another client between the time it was read and the time the update was submitted.",
		Suggestion:  "Reload the service and retry the operation.",
	},
	"SVC002": {
		Code:        "SVC002",
		Title:       "Service In Use",
		Status:      http.StatusConflict,
		Description: "The service cannot be removed because it is still referenced by other resources or managed by a stack.",
		Suggestion:  "Remove the stack that manages this service, or detach the service from the stack first.",
	},
	"SVC003": {Code: "SVC003", Title: "Service Not Found", Status: http.StatusNotFound,
		Description: "The specified service does not exist in the swarm.",
		Suggestion:  "Check the service ID or name and try again."},
	"SVC004": {Code: "SVC004", Title: "Replicas Required", Status: http.StatusBadRequest,
		Description: "The replicas field is required for this operation.",
		Suggestion:  "Provide the replicas field in the request body."},
	"SVC005": {
		Code:        "SVC005",
		Title:       "Cannot Scale Global Service",
		Status:      http.StatusBadRequest,
		Description: "Global-mode services run exactly one task per node and cannot be scaled manually.",
		Suggestion:  "To change the number of tasks, switch the service to replicated mode first.",
	},
	"SVC006": {Code: "SVC006", Title: "Image Required", Status: http.StatusBadRequest,
		Description: "The image field is required for service image updates.",
		Suggestion:  "Provide the image field in the request body."},
	"SVC007": {
		Code:        "SVC007",
		Title:       "No Previous Spec",
		Status:      http.StatusBadRequest,
		Description: "The service has no previous specification to rollback to.",
		Suggestion:  "Rollback is only available after at least one update has been applied to the service.",
	},
	"SVC008": {Code: "SVC008", Title: "Invalid Service Mode", Status: http.StatusBadRequest,
		Description: "The service mode must be one of: replicated, global.",
		Suggestion:  "Use mode=replicated or mode=global."},
	"SVC009": {
		Code:        "SVC009",
		Title:       "Replicas Required For Replicated Mode",
		Status:      http.StatusBadRequest,
		Description: "When switching to replicated mode, the replicas field is required.",
		Suggestion:  "Provide the replicas field alongside the mode change.",
	},
	"SVC010": {Code: "SVC010", Title: "Invalid Endpoint Mode", Status: http.StatusBadRequest,
		Description: "The endpoint mode must be one of: vip, dnsrr.",
		Suggestion:  "Use mode=vip or mode=dnsrr."},
	"SVC011": {
		Code:        "SVC011",
		Title:       "Invalid Resource Specification",
		Status:      http.StatusBadRequest,
		Description: "The merged resource specification is not valid.",
		Suggestion:  "Check the resource limits and reservations in the request body.",
	},
	"SVC012": {Code: "SVC012", Title: "Invalid Update Policy", Status: http.StatusBadRequest,
		Description: "The merged update policy specification is not valid.",
		Suggestion:  "Check the update policy fields in the request body."},
	"SVC013": {Code: "SVC013", Title: "Invalid Rollback Policy", Status: http.StatusBadRequest,
		Description: "The merged rollback policy specification is not valid.",
		Suggestion:  "Check the rollback policy fields in the request body."},
	"SVC014": {Code: "SVC014", Title: "Invalid Healthcheck", Status: http.StatusBadRequest,
		Description: "The merged healthcheck specification is not valid.",
		Suggestion:  "Check the healthcheck fields in the request body."},
	"SVC015": {
		Code:        "SVC015",
		Title:       "Config Missing Required Fields",
		Status:      http.StatusBadRequest,
		Description: "Each config reference must include configID and configName.",
		Suggestion:  "Provide both configID and configName for every config entry.",
	},
	"SVC016": {
		Code:        "SVC016",
		Title:       "Secret Missing Required Fields",
		Status:      http.StatusBadRequest,
		Description: "Each secret reference must include secretID and secretName.",
		Suggestion:  "Provide both secretID and secretName for every secret entry.",
	},
	"SVC017": {Code: "SVC017", Title: "Network Missing Target", Status: http.StatusBadRequest,
		Description: "Each network attachment must include a target network ID.",
		Suggestion:  "Provide the target field for every network entry."},
	"SVC018": {Code: "SVC018", Title: "Invalid Patch Result", Status: http.StatusBadRequest,
		Description: "The JSON patch produced an invalid result that could not be applied.",
		Suggestion:  "Check the patch operations for correctness."},
	"SVC019": {
		Code:        "SVC019",
		Title:       "Invalid Log Driver Specification",
		Status:      http.StatusBadRequest,
		Description: "The merged log driver specification is not valid.",
		Suggestion:  "Check the log driver name and options in the request body.",
	},

	// ── TSK: task operations ──────────────────────────────────────────
	"TSK001": {
		Code:        "TSK001",
		Title:       "Task Already Removed",
		Status:      http.StatusConflict,
		Description: "The task could not be removed because the Docker Engine no longer tracks it.",
		Suggestion:  "The task may have already been cleaned up. Refresh the page to see the current state.",
	},
	"TSK002": {
		Code:        "TSK002",
		Title:       "Task Not Found",
		Status:      http.StatusNotFound,
		Description: "The specified task does not exist.",
		Suggestion:  "Check the task ID and try again. Tasks are ephemeral and may have been cleaned up.",
	},

	// ── STK: stack operations ─────────────────────────────────────────
	"STK001": {
		Code:        "STK001",
		Title:       "Stack Not Found",
		Status:      http.StatusNotFound,
		Description: "The specified stack does not exist. Stacks are derived from service labels and may disappear when all services in the stack are removed.",
		Suggestion:  "Check the stack name and try again.",
	},

	// ── VOL: volume operations ────────────────────────────────────────
	"VOL001": {
		Code:        "VOL001",
		Title:       "Volume In Use",
		Status:      http.StatusConflict,
		Description: "The volume cannot be removed because it is currently mounted by one or more containers.",
		Suggestion:  "Stop or remove the containers using this volume first. Alternatively, use force removal.",
	},
	"VOL002": {Code: "VOL002", Title: "Volume Not Found", Status: http.StatusNotFound,
		Description: "The specified volume does not exist.",
		Suggestion:  "Check the volume name and try again."},

	// ── NET: network operations ───────────────────────────────────────
	"NET001": {
		Code:        "NET001",
		Title:       "Network Has Active Endpoints",
		Status:      http.StatusConflict,
		Description: "The network cannot be removed because it has active endpoints from running containers or services.",
		Suggestion:  "Disconnect or remove the services and containers attached to this network first.",
	},
	"NET002": {Code: "NET002", Title: "Network Not Found", Status: http.StatusNotFound,
		Description: "The specified network does not exist.",
		Suggestion:  "Check the network ID and try again."},

	// ── CFG: config operations ────────────────────────────────────────
	"CFG001": {
		Code:        "CFG001",
		Title:       "Config In Use",
		Status:      http.StatusConflict,
		Description: "The config cannot be removed because it is referenced by one or more services.",
		Suggestion:  "Remove the config reference from all services before deleting it.",
	},
	"CFG002": {Code: "CFG002", Title: "Config Not Found", Status: http.StatusNotFound,
		Description: "The specified config does not exist.",
		Suggestion:  "Check the config ID and try again."},
	"CFG003": {
		Code:        "CFG003",
		Title:       "Config Name Conflict",
		Status:      http.StatusConflict,
		Description: "A config with this name already exists.",
		Suggestion:  "Choose a different name or remove the existing config first.",
	},
	"CFG004": {
		Code:        "CFG004",
		Title:       "Invalid Config",
		Status:      http.StatusBadRequest,
		Description: "The config creation request is invalid.",
		Suggestion:  "Provide a non-empty name and valid base64-encoded data.",
	},
	"CFG005": {
		Code:        "CFG005",
		Title:       "Config Version Conflict",
		Status:      http.StatusConflict,
		Description: "The config was modified concurrently.",
		Suggestion:  "Retry the operation with the latest version.",
	},

	// ── SEC: secret operations ────────────────────────────────────────
	"SEC001": {
		Code:        "SEC001",
		Title:       "Secret In Use",
		Status:      http.StatusConflict,
		Description: "The secret cannot be removed because it is referenced by one or more services.",
		Suggestion:  "Remove the secret reference from all services before deleting it.",
	},
	"SEC002": {Code: "SEC002", Title: "Secret Not Found", Status: http.StatusNotFound,
		Description: "The specified secret does not exist.",
		Suggestion:  "Check the secret ID and try again."},
	"SEC003": {
		Code:        "SEC003",
		Title:       "Secret Name Conflict",
		Status:      http.StatusConflict,
		Description: "A secret with this name already exists.",
		Suggestion:  "Choose a different name or remove the existing secret first.",
	},
	"SEC004": {
		Code:        "SEC004",
		Title:       "Invalid Secret",
		Status:      http.StatusBadRequest,
		Description: "The secret creation request is invalid.",
		Suggestion:  "Provide a non-empty name and valid base64-encoded data.",
	},
	"SEC005": {
		Code:        "SEC005",
		Title:       "Secret Version Conflict",
		Status:      http.StatusConflict,
		Description: "The secret was modified concurrently.",
		Suggestion:  "Retry the operation with the latest version.",
	},
}

// WriteErrorCode writes an RFC 9457 problem details response using a
// well-known error code from the registry. Exported so sub-packages (sse,
// prometheus) can use it as an ErrorWriter callback.
func WriteErrorCode(w http.ResponseWriter, r *http.Request, code string, detail string) {
	writeErrorCode(w, r, code, detail)
}

func writeErrorCode(w http.ResponseWriter, r *http.Request, code string, detail string) {
	def, ok := errorRegistry[code]
	if !ok {
		writeProblem(w, r, http.StatusInternalServerError, detail)
		return
	}

	writeProblemTyped(w, r, ProblemDetail{
		Type:   absPath(r.Context(), "/api/errors/"+code),
		Title:  def.Title,
		Status: def.Status,
		Detail: detail,
	})
}

func sortedErrorCodes() []string {
	codes := make([]string, 0, len(errorRegistry))
	for code := range errorRegistry {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// HandleErrorIndex serves the list of all well-known error codes.
func HandleErrorIndex(w http.ResponseWriter, r *http.Request) {
	codes := sortedErrorCodes()

	defs := make([]ErrorDef, len(codes))
	for i, code := range codes {
		defs[i] = errorRegistry[code]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(defs) // best-effort: status already sent
}

// HandleErrorDetail serves documentation for a single error code.
func HandleErrorDetail(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	def, ok := errorRegistry[code]
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(def) // best-effort: status already sent
}
