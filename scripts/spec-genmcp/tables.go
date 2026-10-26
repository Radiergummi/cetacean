package main

import "github.com/radiergummi/cetacean/internal/spec"

// suites are the conformance suite's requirement files this registry follows,
// each with the published page it is filed under.
var suites = []suite{
	{
		sep: 2575, title: "The stateless protocol core", at: "basic/versioning#",
		pages: corePages, lane: spec.LaneE2E,
	},
	{
		sep: 2243, title: "Standard request headers",
		at:    "basic/transports/streamable-http#standard-request-headers",
		pages: corePages, lane: spec.LaneE2E,
	},
	{
		sep: 2549, title: "Caching hints", at: "server/utilities/caching#",
		pages: corePages, lane: spec.LaneE2E,
	},
	{
		sep: 2164, title: "Reading a resource that does not exist",
		at:    "server/resources#error-handling",
		pages: corePages, lane: spec.LaneE2E,
	},
	{
		sep: 2663, title: "The Tasks extension", at: "tasks#",
		pages: []pageRef{tasksPage},
	},
}

// gaps are requirements of ours that no test reaches yet. They are registered
// rather than dismissed: the obligation is real, only the evidence is missing.
var gaps = map[string]string{
	"sep-2575-http-server-no-independent-requests-on-stream": "The stream lane reads " +
		"notifications and never asserts that nothing else arrives on the stream.",
}

// deferred are requirements this server knowingly fails, each pinned by a test
// that asserts today's answer, so fixing one is a visible change.
var deferred = map[string]string{
	"sep-2663-server-rejects-undeclared-client": "Task creation follows 2025-11-25's " +
		"opt-in: a tools/call carrying params.task gets a CreateTaskResult whether or not " +
		"the request declares the extension. mcp-go v1.3.0 has no server-directed task " +
		"creation to hang the extension's rule on.",
	"sep-2663-result-type-task-on-create": "mcp-go v1.3.0 stamps resultType complete on " +
		"every result it writes, CreateTaskResult included.",
	"sep-2663-failed-status-jsonrpc-only":       reasonToolErrorFails,
	"sep-2663-tool-error-uses-completed-status": reasonToolErrorFails,
	"sep-2663-tasks-get-status-completed": "mcp-go v1.3.0's tasks/get returns the Task " +
		"alone. The result was tasks/result's to deliver, and this revision removed that " +
		"method without moving the result onto tasks/get.",
	"sep-2663-tasks-get-status-failed":              reasonNoErrorField,
	"sep-2663-failed-includes-statusmessage":        reasonNoErrorField,
	"sep-2663-update-ack-empty-result":              reasonNoUpdate,
	"sep-2663-update-error-unknown-task-id":         reasonNoUpdate,
	"sep-2663-update-ignore-unknown-keys":           reasonNoUpdate,
	"sep-2663-result-type-complete-on-tasks-update": reasonNoUpdate,
	"sep-2663-tasks-update-cancel-invalid-task-id-32602": "tasks/cancel answers an " +
		"unknown taskId with -32602; tasks/update answers -32601 whatever the taskId. " +
		reasonNoUpdate,
	"sep-2663-cancel-ack-empty-result": "tasks/cancel answers with the cancelled Task, " +
		"the 2025-11-25 shape mcp-go v1.3.0 implements, not an empty result.",
	"sep-2663-subscribe-missing-capability-non-declaring": "subscriptions/listen drops " +
		"a taskIds request and acknowledges the rest, declared extension or not: " +
		"nothing in mcp-go v1.3.0 knows taskIds.",
	"sep-2663-tasks-methods-non-declaring": "tasks/get and tasks/cancel answer a client " +
		"that declares no extension as they answer one that does; mcp-go v1.3.0 gates " +
		"them on its own task store and nothing else.",
}

const reasonToolErrorFails = "A tool error on a task-augmented call is returned as a " +
	"Go error, so mcp-go marks the task failed rather than completed. Completed would " +
	"read as an applied mutation, since mcp-go v1.3.0's tasks/get carries no result to " +
	"show the isError in."

const reasonNoErrorField = "A failed task carries its cause in statusMessage only: " +
	"mcp-go v1.3.0's Task has no error field."

const reasonNoUpdate = "mcp-go v1.3.0 implements no tasks/update, which therefore " +
	"answers -32601; no task of ours ever asks for input, so a client has nothing to " +
	"send it."

// dismissed says why there is nothing of ours to test. A check absent from
// here becomes a registered requirement instead, which the static gate fails
// on until a test claims it — so an upstream addition is never silent.
var dismissed = map[string]string{
	"sep-2575-client-populates-meta":                  reasonClientSide,
	"sep-2575-client-retry-supported-version":         reasonClientSide,
	"sep-2575-http-client-sends-version-header":       reasonClientSide,
	"sep-2575-client-declares-elicitation-capability": reasonClientSide,
	"sep-2575-client-declares-roots-capability":       reasonClientSide,
	"sep-2575-client-declares-sampling-capability":    reasonClientSide,
	"sep-2243-client-includes-standard-headers":       reasonClientSide,
	"sep-2243-client-supports-custom-headers":         reasonClientSide,
	"sep-2243-client-mirrors-designated-params":       reasonClientSide,
	"sep-2243-client-reject-invalid-tool":             reasonClientSide,
	"sep-2243-client-encode-values":                   reasonClientSide,
	"sep-2243-client-base64-unsafe":                   reasonClientSide,
	"sep-2243-client-omit-null":                       reasonClientSide,
	"sep-2575-http-version-header-matches-meta": "Addressed to the client sending the " +
		"header; the obligation it puts on this server is http-server-header-mismatch-400.",
	"sep-2575-server-sends-prompts-list-changed-on-subscription": "The prompt catalog is " +
		"built once at startup, so the list this notification reports never changes.",
	"sep-2575-server-sends-tools-list-changed-on-subscription": "The tool catalog is built " +
		"once at startup and filtered per identity at list time; the list itself never changes.",
	"sep-2575-server-no-log-without-loglevel": "This server emits no notifications/message " +
		"at all, per-request log level or not.",
	"sep-2243-x-mcp-header-not-empty":            reasonNoDesignations,
	"sep-2243-x-mcp-header-charset":              reasonNoDesignations,
	"sep-2243-x-mcp-header-unique":               reasonNoDesignations,
	"sep-2243-x-mcp-header-primitive-only":       reasonNoDesignations,
	"sep-2243-server-decode-base64":              reasonNoDesignatedParams,
	"sep-2243-server-not-expect-null":            reasonNoDesignatedParams,
	"sep-2243-server-reject-missing-required":    reasonNoDesignatedParams,
	"sep-2243-server-reject-invalid-param-chars": reasonNoDesignatedParams,
	"sep-2243-server-validate-param-match":       reasonNoDesignatedParams,
	"sep-2243-server-reject-param-mismatch":      reasonNoDesignatedParams,

	"sep-2663-client-handles-polymorphic-result":         reasonClientSide,
	"sep-2663-client-rejects-task-result-on-unsupported": reasonClientSide,
	"sep-2663-client-emits-mcp-name-on-tasks-methods":    reasonClientSide,
	"sep-2663-cancel-not-via-cancelled-notification": "Addressed to the client: it " +
		"names the message a task cancellation is sent as.",
	"sep-2663-server-returns-missing-capability-when-required": reasonTasksOptional,
	"sep-2663-server-returns-missing-capability-data-shape":    reasonTasksOptional,
	"sep-2663-mrtr-synchronous-before-task-creation":           reasonNoInput,
	"sep-2663-tasks-get-status-input-required":                 reasonNoInput,
	"sep-2663-input-request-key-unique-per-task":               reasonNoInput,
	"sep-2663-no-key-reuse-after-response":                     reasonNoInput,
	"sep-2663-no-progress-or-message-on-task-stream": "This server agrees to no taskIds " +
		"in an acknowledgement, so no listen stream of its is a task's; and it emits no " +
		"notifications/message at all.",
	"sep-2663-routing-headers-accepted-on-tasks-get": "Not adopted: the suite's own " +
		"derivation, which the extension's 2026-07-28 text does not state.",
	"sep-2663-server-rejects-mismatched-mcp-name-on-tasks-get": "Not a statement of the " +
		"extension: the suite quotes the header rule registered as " +
		"mcp/sep-2243/server-reject-invalid-headers, which covers every method.",
	"sep-2663-tasks-result-removed-method-not-found":      reasonNotAdopted,
	"sep-2663-legacy-task-param-ignored":                  reasonNotAdopted,
	"sep-2663-migrate-from-legacy-capabilities":           reasonNotAdopted,
	"sep-2663-legacy-capability-treated-as-non-declaring": reasonNotAdopted,
}

const reasonTasksOptional = "Every tool of ours that supports tasks declares the support " +
	"optional and answers a plain call in full, so no request is unserviceable without " +
	"a CreateTaskResult."

const reasonNoInput = "No tool of ours asks the client for input, so no task of ours " +
	"enters input_required or issues an input request."

const reasonNotAdopted = "Not adopted: SEP-2663 states it in a compatibility table for " +
	"a 2026-06-30 revision, and the extension's 2026-07-28 text carries no such statement."

const reasonNoDesignations = "This server designates no x-mcp-header parameter, so no " +
	"tool definition of ours can carry a value this constrains."

const reasonNoDesignatedParams = "Mcp-Param-{Name} headers exist only for parameters a " +
	"tool designates with x-mcp-header; no tool of this server designates any."

const reasonClientSide = "Client-side requirement."

// levels carries the strength of a requirement whose text states a closed
// enumeration rather than an RFC 2119 keyword. The suite fails a server that
// violates it either way, so it has a strength even where the sentence does not.
var levels = map[string]string{
	"sep-2549-cache-scope-valid": "MUST",
}

// located places a check whose suite wording is not a quotation of the
// published revision: the section it lives in, and, where the suite rewords
// it, the published text it stands for. [Brackets] mark what the quote elides.
var located = map[string]loc{
	"sep-2575-http-server-no-independent-requests-on-stream": {
		at: "basic/transports/streamable-http#receiving-messages",
		quote: "The server MUST NOT send independent JSON-RPC requests on this stream. " +
			"Server-to-client interactions (sampling, elicitation, list-roots) are " +
			"embedded as input requests inside an InputRequiredResult [per the multi " +
			"round-trip pattern], not delivered as separate requests on this or any " +
			"other stream.",
	},
	"sep-2575-http-server-unsupported-version-400": {
		at: "basic/transports/streamable-http#protocol-version-header",
		quote: "If the server does not implement the requested protocol version (whether " +
			"the version is unknown to the server, or is a known version the server has " +
			"chosen not to support), it MUST respond with 400 Bad Request and an " +
			"UnsupportedProtocolVersionError listing its supported versions.",
	},
	"sep-2243-server-reject-invalid-headers": {
		at: "basic/transports/streamable-http#server-validation",
		quote: "Servers that process the request body MUST reject requests where the " +
			"values specified in the headers do not match the corresponding values in " +
			"the request body. [...] Validation failure conditions include: A required " +
			"standard header (MCP-Protocol-Version, Mcp-Method, Mcp-Name) is missing.",
	},
	"sep-2243-server-reject-error-code": {
		at: "basic/transports/streamable-http#server-validation",
		quote: "When rejecting a request due to header validation failure, servers MUST " +
			"return HTTP status 400 Bad Request and MUST include a JSON-RPC error response " +
			"using the following error code: [-32020, HeaderMismatch]",
	},
	"sep-2549-tools-list-caching-hints":               cacheable("tools/list"),
	"sep-2549-prompts-list-caching-hints":             cacheable("prompts/list"),
	"sep-2549-resources-list-caching-hints":           cacheable("resources/list"),
	"sep-2549-resources-templates-list-caching-hints": cacheable("resources/templates/list"),
	"sep-2549-resources-read-caching-hints":           cacheable("resources/read"),
	"sep-2549-cache-scope-valid": {
		at: "server/utilities/caching#cacheable-model",
		quote: "The Cache Scope Field [cacheScope] indicates the intended scope of the " +
			`cached response, either "public" or "private".`,
	},
	"sep-2164-error-code": {
		at: "server/resources#error-handling",
		quote: "If the requested resource does not exist, servers MUST return a JSON-RPC " +
			"error with code -32602 (Invalid Params).",
	},
	"sep-2663-update-ack-empty-result":      {at: "tasks#response-1"},
	"sep-2663-update-error-unknown-task-id": {at: "tasks#response-1"},
	"sep-2663-cancel-error-unknown-task-id": {at: "tasks#response-2"},
	"sep-2663-tasks-get-invalid-task-id-32602": protocolError(invalidTaskID,
		"Servers MUST return this error for tasks/get."),
	"sep-2663-tasks-update-cancel-invalid-task-id-32602": protocolError(invalidTaskID,
		"Servers SHOULD return this error for tasks/update and tasks/cancel."),
	"sep-2663-tasks-methods-non-declaring": protocolError(missingCapability,
		"Servers MUST return this error for non-declaring clients issuing tasks/get, "+
			"tasks/update, and tasks/cancel requests."),
	"sep-2663-cancel-ack-empty-result": {
		at:    "tasks#response-2",
		quote: "On success, the server MUST acknowledge the request with an empty result.",
	},
	"sep-2575-server-sends-subscription-ack": {
		at: "basic/patterns/subscriptions#acknowledgment",
		quote: "The server MUST send notifications/subscriptions/acknowledged as the first " +
			"message carrying the subscription's ID in _meta under " +
			"io.modelcontextprotocol/subscriptionId, and MUST NOT send any notification " +
			"on the subscription before it.",
	},
}

type loc struct{ at, quote string }

const (
	invalidTaskID     = "Invalid or nonexistent taskId: -32602 (Invalid params)"
	missingCapability = "Missing required client capabilities: -32021 (Missing Required " +
		"Client Capability)"
)

// protocolError quotes a statement of the Tasks extension's error list with
// the case it hangs under, which "this error" otherwise leaves unsaid.
func protocolError(errorCase, statement string) loc {
	return loc{at: "tasks#protocol-errors", quote: errorCase + " [...] " + statement}
}

// cacheable is one operation of the list the published sentence ends in,
// which the suite splits into a check per method.
func cacheable(method string) loc {
	return loc{
		at: "server/utilities/caching#cacheable-results",
		quote: `Servers MUST include caching hints on results with resultType: "complete" ` +
			"returned by the following operations: [...] " + method,
	}
}

// mutants are the edits each requirement's claimants must refuse, keyed by
// document and requirement.
var mutants = map[string][]spec.Mutant{
	"sep-414/trace-context-values-are-w3c": {
		{
			File:    "internal/mcp/tracing/otel.go",
			Replace: "\t\tpropagation.TraceContext{},\n",
			With:    "",
		},
		{File: "internal/mcp/tracing/otel.go", Replace: "\t\tpropagation.Baggage{},\n", With: ""},
	},
	"sep-1303/input-validation-is-a-tool-execution-error": {{
		File:    "internal/mcp/tools.go",
		Replace: "return mcplib.NewToolResultError(err.Error()), nil",
		With:    "return nil, err",
	}},
	"sep-2133/identifiers-follow-meta-naming": {{
		File:    "internal/mcp/extensions.go",
		Replace: `extensionUI = "io.modelcontextprotocol/ui"`,
		With:    `extensionUI = "ui"`,
	}},
	"sep-2577/logging-not-adopted": {
		{
			File:    "internal/mcp/server.go",
			Replace: "mcpserver.WithMaxConcurrentTasks(opts.Config.MaxConcurrentTasks),",
			With:    "mcpserver.WithLogging(), mcpserver.WithMaxConcurrentTasks(opts.Config.MaxConcurrentTasks),",
		},
	},
}

// quoted are the documents the conformance suite publishes no requirement
// file for. Each statement is a quotation the generator finds in the pinned
// revision before it writes anything.
var quoted = []quotedDoc{
	{
		name:   "sep-2577",
		source: "SEP-2577",
		title:  "Deprecating Roots, Sampling and Logging",
		at: []string{
			"deprecated#",
			"client/roots#",
			"client/sampling#",
			"server/utilities/logging#",
		},
		from: "The deprecation notice opening each of the Roots, Sampling and Logging " +
			"pages, and the policy statement of the deprecated-features registry they " +
			"point to.",
		reqs: []quotedReq{
			{id: "roots-not-adopted", at: "client/roots#", quote: deprecation("Roots",
				"passing directories or files via tool parameters, resource URIs, or "+
					"server configuration.")},
			{id: "sampling-not-adopted", at: "client/sampling#", quote: deprecation("Sampling",
				"integrating directly with LLM provider APIs.")},
			{id: "logging-not-adopted", at: "server/utilities/logging#", quote: deprecation(
				"Logging", "logging to stderr for stdio transports, or to OpenTelemetry for "+
					"structured observability.")},
		},
		dismissed: []quotedDismissal{{
			id: "deprecated-features-policy", at: "deprecated#",
			quote: "A Deprecated feature remains part of the specification but is scheduled " +
				"for removal: new implementations SHOULD NOT adopt it, and existing " +
				"implementations SHOULD migrate before the feature's earliest removal.",
			reason: "The policy each notice restates for its own feature, registered as " +
				"mcp/sep-2577/roots-not-adopted, mcp/sep-2577/sampling-not-adopted and " +
				"mcp/sep-2577/logging-not-adopted. The registry's other entries are " +
				"deprecated by other proposals.",
		}},
	},
	{
		name: "sep-2133", source: "SEP-2133", title: "Extension negotiation",
		at:   []string{"basic/versioning#extension-negotiation"},
		from: "The three statements of the Extension Negotiation section of Versioning.",
		reqs: []quotedReq{
			{
				id: "identifiers-follow-meta-naming", at: "basic/versioning#extension-negotiation",
				quote: "Extension identifiers MUST follow the _meta key naming rules, with " +
					"a mandatory prefix.",
			},
			{
				id: "unsupported-extension-falls-back",
				at: "basic/versioning#extension-negotiation",
				quote: "If one party supports an extension but the other does not, the " +
					"supporting party MUST either revert to core protocol behavior or reject " +
					"the request with an appropriate error.",
				deferred: "Holds for MCP Apps, where a tool with a widget answers a host that " +
					"declares no UI extension in text. Not for Tasks: a client that declares " +
					"no Tasks extension can still get a CreateTaskResult, which is " +
					"mcp/sep-2663/server-rejects-undeclared-client.",
			},
		},
		dismissed: []quotedDismissal{{
			id: "extensions-document-fallback", at: "basic/versioning#extension-negotiation",
			quote:  "Extensions SHOULD document their expected fallback behavior.",
			reason: "Addressed to an extension's authors, not to a server implementing it.",
		}},
	},
	{
		name: "sep-414", source: "SEP-414", title: "Trace context in _meta",
		at: []string{"basic/index#_meta"},
		from: "The OpenTelemetry trace context paragraph of the _meta section, the one " +
			"statement SEP-414 adds.",
		reqs: []quotedReq{{
			id: "trace-context-values-are-w3c", at: "basic/index#_meta",
			quote: "As an exception to the prefix requirement above, the keys traceparent, " +
				"tracestate, and baggage are reserved for OpenTelemetry trace context " +
				"propagation. When present, their values MUST follow W3C Trace Context and " +
				"W3C Baggage formats respectively.",
		}},
	},
	{
		name:   "sep-1303",
		source: "SEP-1303",
		title:  "Input validation errors as tool execution errors",
		at:     []string{"server/tools#error-handling"},
		from: "The Error Handling section of Tools, which SEP-1303 rewrote: its " +
			"classification of input validation errors, and the two statements addressed " +
			"to clients.",
		reqs: []quotedReq{{
			id: "input-validation-is-a-tool-execution-error", at: "server/tools#error-handling",
			quote: "Tool Execution Errors contain actionable feedback that language models " +
				"can use to self-correct and retry with adjusted parameters: [...] Input " +
				"validation errors (e.g., date in wrong format, value out of range) [...] " +
				"They are reported in tool results with isError: true",
			// A definition, not a keyword: an input validation error answered as a
			// protocol error is in the other category, which the client withholds
			// from the model.
			level: "MUST",
		}},
		dismissed: []quotedDismissal{
			{
				id: "clients-may-surface-protocol-errors", at: "server/tools#error-handling",
				quote: "Clients MAY provide protocol errors to language models, though these " +
					"are less likely to result in successful recovery.",
				reason: reasonClientSide,
			},
			{
				id: "clients-should-surface-execution-errors", at: "server/tools#error-handling",
				quote: "Clients SHOULD provide tool execution errors to language models to " +
					"enable self-correction.",
				reason: reasonClientSide,
			},
		},
	},
}

// deprecation is the notice SEP-2577 opens each deprecated feature's page with.
func deprecation(feature, migration string) string {
	return "The " + feature + " feature is deprecated as of protocol version 2026-07-28 " +
		"[...] New implementations SHOULD NOT adopt it; existing implementations SHOULD " +
		"migrate to " + migration
}

type quotedDoc struct {
	name, source, title string
	at                  []string
	from                string
	reqs                []quotedReq
	dismissed           []quotedDismissal
}

type quotedReq struct {
	id, at, quote string

	// level overrides the strength read off the quote, for a statement made
	// as a definition rather than with an RFC 2119 keyword.
	level    string
	deferred string
}

type quotedDismissal struct{ id, at, quote, reason string }
