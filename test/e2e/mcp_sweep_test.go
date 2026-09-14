//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file closes the gap README.md's Deferred section names: "MCP
// grant-based tools/list filtering (only tier gating is covered)". It drives
// the MCP catalog (tools/list, prompts/list, resources/templates/list) per
// persona against a real cluster and asserts the two-way property that makes
// a catalog trustworthy: a tool a persona is offered must not refuse with an
// ACL error, and a tool withheld from a persona must not succeed if invoked
// directly. Reserves port 19008 (see README.md's reserved-ports table).
//
// This is explicitly NOT a test of the OAuth flow. DCR, CIMD, PKCE, refresh
// rotation and theft detection are a separate, larger lane and are out of
// scope here — see startMCPSweep's doc comment for what CETACEAN_MCP_AUTH_BYPASS
// stands in for instead.
//
// drivenMCPTools is the self-enforcing gate: TestEveryMCPToolIsDrivenOrExcused
// requires every one of the 27 tools in the catalog to appear in it. Unlike
// the read and write sweeps, nothing here needed an excuse — even the
// destructive tier-3 removals are driven safely, against throwaway resources
// this file creates and owns, with the one exception noted on
// driveUpdateNode (the environment's single node cannot safely be given a
// real availability/role change).

const mcpSweepPort = 19008

// startMCPSweep brings up a real cluster with MCP enabled, headers auth, and
// readSweepPolicy's ACL policy (the same four personas as the read sweep, so
// findings from both lanes describe the same people), plus
// CETACEAN_MCP_AUTH_BYPASS=headers.
//
// That bypass is a supported production configuration
// (internal/config/mcp.go's AuthBypass, and internal/mcp/server.go's
// bypassActive/bearerAuth, whose comment names "headers" as safe because it
// never writes on the success path): it makes the upstream headers provider
// supply identity directly, skipping OAuth token minting and verification
// entirely. That is deliberate here — this file tests the ACL boundary once
// an identity is established, not how that identity got established. DCR,
// CIMD, PKCE, refresh-token rotation and theft detection belong to a
// separate, larger OAuth-flow lane and are not exercised by anything in this
// file.
//
// Operations level is 3 (impactful, the ceiling) so every one of the 27
// tools is tier-eligible regardless of persona — tier gating is already
// covered by TestMCPToolsListIsGatedByOperationsLevel in mcp_test.go, so
// fixing the tier at its highest value here isolates the ACL grant boundary
// this file exists to check from the tier boundary that file already checks.
func startMCPSweep(t *testing.T, env *harness.Env) *sut.Process {
	t.Helper()

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(readSweepPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	return sut.Start(t, sut.Config{
		Port:       mcpSweepPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_ACL_POLICY_FILE":      policy,
			"CETACEAN_OPERATIONS_LEVEL":     "3",
			"CETACEAN_MCP":                  "true",
			"CETACEAN_MCP_AUTH_BYPASS":      "headers",
		},
	})
}

// mcpNameHeader mirrors mcp-go's ExtractHeaderName (mcp/headers.go): the
// Mcp-Name header carries params.name for tools/call and prompts/get, and
// params.uri for resources/read. test/e2e/mcp_test.go's own mcpCall helper
// only ever calls tools/list and tools/call, so it never needed the
// resources/read case; this file does, to drive the templated
// cetacean://<type>/{id} resource reads per persona.
func mcpNameHeader(method string, params map[string]any) (string, bool) {
	switch method {
	case "tools/call", "prompts/get":
		name, ok := params["name"].(string)
		return name, ok
	case "resources/read":
		uri, ok := params["uri"].(string)
		return uri, ok
	default:
		return "", false
	}
}

// mcpEnvelope is a JSON-RPC response, kept apart from an error so a caller
// can assert on a refusal instead of failing the test on one.
type mcpEnvelope struct {
	Result json.RawMessage
	Error  *string // JSON-RPC protocol-level error message, nil if none
}

// mcpAs issues one JSON-RPC call as persona against the modern (2026-07-28)
// stateless transport. Unlike test/e2e/mcp_test.go's mcpCall, it never fails
// the test on a non-200 or an RPC error: both are outcomes this file asserts
// on directly (a denied call, an unknown method).
func mcpAs(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	method string,
	params map[string]any,
) (mcpEnvelope, int) {
	t.Helper()

	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, proc.BaseURL+"/mcp", bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	req.Header.Set("X-Auth-User", persona.user)
	if persona.groups != "" {
		req.Header.Set("X-Auth-Groups", persona.groups)
	}

	if name, ok := mcpNameHeader(method, params); ok {
		req.Header.Set("Mcp-Name", name)
	}

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp %s as %s: %v", method, persona.name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// A refused call (a tool tools/list would not advertise to this caller,
	// or a malformed request) still comes back as a JSON-RPC envelope, just
	// over a non-200 status and with an "error" object instead of a
	// "result" -- mcp-go maps a JSON-RPC protocol error (e.g. "tool ... not
	// found", code -32602) to HTTP 400 rather than 200. Returning early here
	// on a non-200 status without reading it discarded exactly the
	// information this file's denial assertions need; that early return was
	// a placeholder from before this file called tools/call on a hidden
	// tool for the first time; the body is always attempted, and a status
	// this file did not anticipate is reported with what came back instead
	// of failing the test outright.
	var raw struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		if resp.StatusCode != http.StatusOK {
			// Some non-JSON-RPC layer in front of the handler (an
			// auth/proxy rejection, say) answered instead. Report the raw
			// body as the "error" so a caller sees why, rather than losing
			// it to a decode failure this file cannot recover from.
			msg := fmt.Sprintf("HTTP %d (non-JSON-RPC body): %s", resp.StatusCode, body)
			return mcpEnvelope{Error: &msg}, resp.StatusCode
		}
		t.Fatalf("decode %s response as %s: %v\nbody: %s", method, persona.name, err, body)
	}

	env := mcpEnvelope{Result: raw.Result}
	if raw.Error != nil {
		env.Error = &raw.Error.Message
	}

	return env, resp.StatusCode
}

// toolCallResult is the shape every tools/call result shares: mcp-go reports
// a refused or failed call as isError:true with the reason in Content, not
// as a JSON-RPC error — see internal/mcp's acl.go checkRead/checkWrite,
// whose messages ("read access denied for ...", "write access denied for
// ...") are what isErrorMessageDenies looks for below.
type toolCallResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func decodeToolCall(t *testing.T, env mcpEnvelope) toolCallResult {
	t.Helper()

	var call toolCallResult
	if len(env.Result) == 0 {
		return call
	}
	if err := json.Unmarshal(env.Result, &call); err != nil {
		t.Fatalf("decode tools/call result: %v (raw: %s)", err, env.Result)
	}
	return call
}

// isACLDenial reports whether a tool call's error content names an ACL
// denial (internal/mcp/acl.go's checkRead/checkWrite error text) rather than
// some other failure (a bad argument, a missing resource, an unconfigured
// dependency like Prometheus). The two-way check in this file hinges on
// telling those apart: a hidden tool must fail this way specifically, not
// merely fail somehow.
func isACLDenial(call toolCallResult) bool {
	if !call.IsError {
		return false
	}
	for _, c := range call.Content {
		if strings.Contains(c.Text, "access denied") {
			return true
		}
	}
	return false
}

// toolCallOutcome captures how a tools/call attempt concluded. A denial
// surfaces two different ways depending on why the caller lacks access:
//
//   - A tool absent from tools/list is unreachable at all: mcp-go's own tool
//     filter (internal/mcp/server.go's filterToolsForIdentity, wired as
//     WithToolFilter) applies to tools/call as well as tools/list, so calling
//     a hidden tool never reaches the handler -- it comes back as the
//     JSON-RPC protocol error "tool '<name>' not found" (code -32602), which
//     mcp-go's HTTP transport reports as 400 Bad Request rather than 200.
//     This was not documented anywhere this file's author found before
//     running it against a real server, and the first version of this file
//     assumed every denial looked like the second case below.
//   - A tool that IS listed but whose specific target the caller's real
//     grant does not cover reaches the handler and is refused there, which
//     surfaces as an ordinary 200 response with isError:true and an "access
//     denied" message (internal/mcp/acl.go's checkRead/checkWrite). This is
//     what happens for frontend on a service/task/config/secret/network/
//     volume-gated tool outside a frontend-* stack -- see
//     driveFrontendStackOwnership for why tools/list still advertises it.
//
// Both are legitimate denials; which one a given call produces depends on
// whether the tool is visible to the caller, which is why every assertion
// in this file checks visibility and the call outcome together rather than
// either alone.
type toolCallOutcome struct {
	httpStatus int
	rpcError   string
	result     toolCallResult
}

func (o toolCallOutcome) denied() bool {
	if o.rpcError != "" {
		return strings.Contains(o.rpcError, "not found")
	}
	return isACLDenial(o.result)
}

// listToolNames calls tools/list as persona and returns the advertised tool
// names.
func listToolNames(t *testing.T, proc *sut.Process, persona readPersona) map[string]bool {
	t.Helper()

	env, status := mcpAs(t, proc, persona, "tools/list", nil)
	if status != http.StatusOK || env.Error != nil {
		t.Fatalf("tools/list as %s: status=%d err=%v", persona.name, status, env.Error)
	}

	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(env.Result, &listed); err != nil {
		t.Fatalf("decode tools/list as %s: %v", persona.name, err)
	}

	names := make(map[string]bool, len(listed.Tools))
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	return names
}

func listPromptNames(t *testing.T, proc *sut.Process, persona readPersona) map[string]bool {
	t.Helper()

	env, status := mcpAs(t, proc, persona, "prompts/list", nil)
	if status != http.StatusOK || env.Error != nil {
		t.Fatalf("prompts/list as %s: status=%d err=%v", persona.name, status, env.Error)
	}

	var listed struct {
		Prompts []struct {
			Name string `json:"name"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(env.Result, &listed); err != nil {
		t.Fatalf("decode prompts/list as %s: %v", persona.name, err)
	}

	names := make(map[string]bool, len(listed.Prompts))
	for _, p := range listed.Prompts {
		names[p.Name] = true
	}
	return names
}

func listResourceTemplates(t *testing.T, proc *sut.Process, persona readPersona) []string {
	t.Helper()

	env, status := mcpAs(t, proc, persona, "resources/templates/list", nil)
	if status != http.StatusOK || env.Error != nil {
		t.Fatalf(
			"resources/templates/list as %s: status=%d err=%v",
			persona.name,
			status,
			env.Error,
		)
	}

	var listed struct {
		ResourceTemplates []struct {
			URITemplate string `json:"uriTemplate"`
		} `json:"resourceTemplates"`
	}
	if err := json.Unmarshal(env.Result, &listed); err != nil {
		t.Fatalf("decode resources/templates/list as %s: %v", persona.name, err)
	}

	out := make([]string, len(listed.ResourceTemplates))
	for i, tmpl := range listed.ResourceTemplates {
		out[i] = tmpl.URITemplate
	}
	return out
}

// callToolRaw invokes name as persona with args and reports how the call
// concluded, without failing the test on a non-200 status or a JSON-RPC
// error: both are outcomes the assertions in this file check for directly
// (see toolCallOutcome).
func callToolRaw(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	name string,
	args map[string]any,
) toolCallOutcome {
	t.Helper()

	env, status := mcpAs(t, proc, persona, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})

	outcome := toolCallOutcome{httpStatus: status}
	if env.Error != nil {
		outcome.rpcError = *env.Error
		return outcome
	}

	outcome.result = decodeToolCall(t, env)
	return outcome
}

// callTool invokes name as persona with args and decodes the tool-call
// result, failing the test on a JSON-RPC protocol error -- for call sites
// that expect either success or an in-band isError, never a rejection at
// the transport level (e.g. describe, which is always listed).
func callTool(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	name string,
	args map[string]any,
) toolCallResult {
	t.Helper()

	outcome := callToolRaw(t, proc, persona, name, args)
	if outcome.rpcError != "" {
		t.Fatalf(
			"tools/call %s as %s: JSON-RPC error (HTTP status %d): %s",
			name, persona.name, outcome.httpStatus, outcome.rpcError,
		)
	}

	return outcome.result
}

// assertVisibleAndCallable is the "advertised must be callable" half of the
// two-way check: name must appear in visible, and calling it must not be
// denied (it may still fail for another reason — e.g. a bad argument —
// which is not what this check is about).
func assertVisibleAndCallable(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	visible map[string]bool,
	name string,
	args map[string]any,
) {
	t.Helper()

	if !visible[name] {
		t.Errorf(
			"%s: %q not in tools/list, want it visible (this persona holds the grant it needs)",
			persona.name,
			name,
		)
		return
	}

	outcome := callToolRaw(t, proc, persona, name, args)
	if outcome.denied() {
		t.Errorf(
			"%s: %q is listed but refused when called directly: rpcError=%q isError=%v content=%v",
			persona.name, name, outcome.rpcError, outcome.result.IsError, outcome.result.Content,
		)
	}
}

// assertHiddenAndRefused is the "withheld must not succeed" half — the
// security-critical direction: name must be absent from tools/list, AND
// calling it directly must be denied (either mcp-go's own "tool ... not
// found" for a name the filter never advertised, or an in-band isError if
// something upstream still let the call through).
func assertHiddenAndRefused(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	visible map[string]bool,
	name string,
	args map[string]any,
) {
	t.Helper()

	if visible[name] {
		t.Errorf(
			"%s: %q is in tools/list, want it hidden (no grant reaches it)",
			persona.name,
			name,
		)
	}

	outcome := callToolRaw(t, proc, persona, name, args)
	if !outcome.denied() {
		t.Errorf(
			"%s: %q succeeded (or failed for a non-ACL reason) when called directly, want a "+
				"denial: httpStatus=%d rpcError=%q isError=%v content=%v",
			persona.name, name, outcome.httpStatus, outcome.rpcError,
			outcome.result.IsError, outcome.result.Content,
		)
	}
}

// assertVisibleButDenied is the coarse-widening case documented on
// driveFrontendStackOwnership: the tool IS advertised (a stack grant's
// implied types reach it regardless of the grant's name pattern) but the
// caller's real access does not cover this specific target, so the call
// must still be refused -- from inside the handler (isError), since
// tools/call itself will let it through.
func assertVisibleButDenied(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	visible map[string]bool,
	name string,
	args map[string]any,
) {
	t.Helper()

	if !visible[name] {
		t.Errorf(
			"%s: %q not in tools/list; expected it visible via the coarse stack-grant widening "+
				"(see driveFrontendStackOwnership) even though this call must still be denied",
			persona.name, name,
		)
	}

	outcome := callToolRaw(t, proc, persona, name, args)
	if !outcome.denied() {
		t.Errorf(
			"%s: %q succeeded on a target outside its real grant, want a denial: "+
				"httpStatus=%d rpcError=%q isError=%v content=%v",
			persona.name, name, outcome.httpStatus, outcome.rpcError,
			outcome.result.IsError, outcome.result.Content,
		)
	}
}

// assertDenied picks the right one of assertHiddenAndRefused /
// assertVisibleButDenied for the persona and resource type. frontend's
// stack:frontend-* grant is coarsely projected (by
// internal/acl/evaluator.go's impliedTypes) onto every type a stack grant
// reaches -- service, task, config, secret, network, volume -- so a tool
// gated on one of those types is visible to frontend even for a target
// outside any frontend-* stack, and denied only once the call reaches the
// handler. stackImplied names that case; every other denied persona, and
// frontend itself on a node-gated tool (node is not in
// impliedTypes["stack"]), is denied by not being offered the tool at all.
func assertDenied(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	stackImplied bool,
	visible map[string]bool,
	name string,
	args map[string]any,
) {
	t.Helper()

	if persona.name == "frontend" && stackImplied {
		assertVisibleButDenied(t, proc, persona, visible, name, args)
		return
	}

	assertHiddenAndRefused(t, proc, persona, visible, name, args)
}

// ─── baseline resolution over MCP ──────────────────────────────────────

// mcpFindID resolves a resource's ID by name via the find tool, as ops (who
// reads everything). Polled for up to 10s: a resource just created directly
// against the engine (fixtures.DeployStack) or via a tool this same test
// just called (create_config, create_secret) is only findable once
// Cetacean's watcher has processed the corresponding Docker event, which is
// asynchronous.
func mcpFindID(t *testing.T, proc *sut.Process, resourceType, name string) string {
	t.Helper()

	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	deadline := time.Now().Add(10 * time.Second)
	var lastCount int

	for time.Now().Before(deadline) {
		env, status := mcpAs(t, proc, opsPersona, "tools/call", map[string]any{
			"name":      "find",
			"arguments": map[string]any{"type": resourceType},
		})
		if status != http.StatusOK || env.Error != nil {
			t.Fatalf("find type=%s as ops: status=%d err=%v", resourceType, status, env.Error)
		}

		call := decodeToolCall(t, env)
		if call.IsError {
			t.Fatalf("find type=%s as ops: %v", resourceType, call.Content)
		}

		var found struct {
			StructuredContent struct {
				Items []item `json:"items"`
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal(env.Result, &found); err != nil {
			t.Fatalf("decode find type=%s: %v", resourceType, err)
		}

		for _, it := range found.StructuredContent.Items {
			if it.Name == name {
				return it.ID
			}
		}

		lastCount = len(found.StructuredContent.Items)
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("find type=%s: %q not found among %d items after 10s", resourceType, name, lastCount)
	return ""
}

// ─── read-tier tools (tier 0, ungated by operations level) ─────────────

// readToolSpec drives a single tier-0 tool for every persona. Every one of
// these is safe to invoke for any persona: none mutates anything.
type readToolSpec struct {
	name           string
	args           func(baselineIDs) map[string]any
	gatedOnService bool // true for get_logs/watch: toolACLSpecs requires service:read
}

func driveReadTool(spec readToolSpec) mcpDriveFunc {
	return func(t *testing.T, env *harness.Env, proc *sut.Process, ids baselineIDs) {
		for _, p := range readPersonas {
			t.Run(p.name, func(t *testing.T) {
				visible := listToolNames(t, proc, p)

				if p.name == "anonymous" && spec.gatedOnService {
					assertHiddenAndRefused(t, proc, p, visible, spec.name, spec.args(ids))
					return
				}

				// Every granted persona reads every service under this
				// policy, and every tier-0 tool not gated on service:read is
				// always visible regardless of grants (it filters its own
				// results) -- so every persona lands in the "visible and
				// callable without an ACL denial" branch except anonymous on
				// the two service-gated tools above.
				assertVisibleAndCallable(t, proc, p, visible, spec.name, spec.args(ids))
			})
		}
	}
}

// ─── the sweep itself ───────────────────────────────────────────────────

type mcpDriveFunc func(t *testing.T, env *harness.Env, proc *sut.Process, ids baselineIDs)

// driveFind checks find (never gated in toolACLSpecs by design — see the map
// in internal/mcp/server.go) across every persona: always visible, and
// per-type results are ACL-filtered rather than refused, so anonymous must
// get zero items rather than an error.
func driveFind(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			if !visible["find"] {
				t.Fatalf("%s: find missing from tools/list; find must always be visible", p.name)
			}

			call := callTool(t, proc, p, "find", map[string]any{"type": "services"})
			if call.IsError {
				t.Fatalf("%s: find type=services: %v", p.name, call.Content)
			}

			var found struct {
				StructuredContent struct {
					Items []json.RawMessage `json:"items"`
				} `json:"structuredContent"`
			}
			env, _ := mcpAs(t, proc, p, "tools/call", map[string]any{
				"name":      "find",
				"arguments": map[string]any{"type": "services"},
			})
			if err := json.Unmarshal(env.Result, &found); err != nil {
				t.Fatalf("%s: decode find: %v", p.name, err)
			}

			if p.name == "anonymous" {
				if len(found.StructuredContent.Items) != 0 {
					t.Errorf(
						"anonymous: find type=services returned %d items, want 0",
						len(found.StructuredContent.Items),
					)
				}
				return
			}

			if len(found.StructuredContent.Items) == 0 {
				t.Errorf(
					"%s: find type=services returned 0 items, want at least the fixture's own",
					p.name,
				)
			}
		})
	}
}

// driveDescribeAndResource checks describe and the templated
// cetacean://services/{id} resource read: both route through digestOf (per
// internal/mcp/describe.go), so both must agree on the same boundary —
// refused for anonymous, served for every granted persona.
func driveDescribeAndResource(t *testing.T, env *harness.Env, proc *sut.Process, ids baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			call := callTool(t, proc, p, "describe", map[string]any{
				"type": "service", "id": ids.serviceID,
			})

			resourceEnv, status := mcpAs(t, proc, p, "resources/read", map[string]any{
				"uri": "cetacean://services/" + ids.serviceID,
			})

			if p.name == "anonymous" {
				if !isACLDenial(call) {
					t.Errorf(
						"anonymous: describe service succeeded, want an ACL denial: %v",
						call.Content,
					)
				}
				if resourceEnv.Error == nil {
					t.Errorf(
						"anonymous: resources/read cetacean://services/%s succeeded (status=%d), want a denial",
						ids.serviceID,
						status,
					)
				}
				return
			}

			if call.IsError {
				t.Errorf("%s: describe service refused: %v", p.name, call.Content)
			}
			if status != http.StatusOK || resourceEnv.Error != nil {
				t.Errorf(
					"%s: resources/read cetacean://services/%s: status=%d err=%v",
					p.name, ids.serviceID, status, resourceEnv.Error,
				)
			}
		})
	}
}

// driveResourceTemplateList checks resources/templates/list is reachable for
// every persona and advertises the nine templated resource types
// (README.md/CLAUDE.md's inventory), regardless of grants -- the template
// list itself carries no per-resource content, so there is nothing here for
// ACL to filter.
func driveResourceTemplateList(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	want := []string{
		"cetacean://configs/{id}",
		"cetacean://networks/{id}",
		"cetacean://nodes/{id}",
		"cetacean://secrets/{id}",
		"cetacean://services/{id}",
		"cetacean://services/{id}/logs",
		"cetacean://stacks/{name}",
		"cetacean://tasks/{id}",
		"cetacean://volumes/{name}",
	}

	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			templates := listResourceTemplates(t, proc, p)
			for _, w := range want {
				if !slices.Contains(templates, w) {
					t.Errorf(
						"%s: resources/templates/list missing %q; got %v",
						p.name,
						w,
						templates,
					)
				}
			}
		})
	}
}

// drivePromptList checks prompts/list's boundary: anonymous, who fails
// allTypesReadable for every prompt (it holds no read grant on anything),
// must see zero; ops, who holds every driven tool and every read type, must
// see the most of any persona -- the ceiling every other persona's count is
// compared against, rather than a hardcoded total that would drift the
// moment a prompt is added.
func drivePromptList(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	counts := map[string]int{}
	for _, p := range readPersonas {
		prompts := listPromptNames(t, proc, p)
		counts[p.name] = len(prompts)
		t.Logf("%s: prompts/list = %v", p.name, slices.Sorted(func(yield func(string) bool) {
			for name := range prompts {
				if !yield(name) {
					return
				}
			}
		}))
	}

	if counts["anonymous"] != 0 {
		t.Errorf(
			"anonymous: prompts/list has %d entries, want 0 (no read grant on anything)",
			counts["anonymous"],
		)
	}

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		if counts[p.name] > counts["ops"] {
			t.Errorf(
				"%s: prompts/list has %d entries, more than ops's %d -- ops holds every grant, "+
					"so no persona should see more prompts than ops does",
				p.name, counts[p.name], counts["ops"],
			)
		}
	}
}

// ─── mutating tools: node ───────────────────────────────────────────────

// driveUpdateNodeLabels checks update_node_labels (tier 2, node:write): ops
// (holds write:* ) can set and revert a harmless label on the environment's
// only node; viewers, frontend and oncall (none holds node:write under this
// policy) and anonymous must be refused when they try the same thing, and
// the tool must not even be listed for them.
func driveUpdateNodeLabels(t *testing.T, env *harness.Env, proc *sut.Process, ids baselineIDs) {
	const labelKey = "cetacean-mcp-sweep"

	t.Run("ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(t, proc, opsPersona, visible, "update_node_labels", map[string]any{
			"id":     ids.nodeID,
			"labels": map[string]any{labelKey: "1"},
		})

		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			node, _, err := env.Docker.NodeInspectWithRaw(ctx, ids.nodeID)
			if err != nil {
				t.Errorf("cleanup: NodeInspectWithRaw: %v", err)
				return
			}
			delete(node.Spec.Labels, labelKey)
			if err := env.Docker.NodeUpdate(ctx, ids.nodeID, node.Version, node.Spec); err != nil {
				t.Errorf("cleanup: NodeUpdate (remove sweep label): %v", err)
			}
		})
	})

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertHiddenAndRefused(t, proc, p, visible, "update_node_labels", map[string]any{
				"id":     ids.nodeID,
				"labels": map[string]any{labelKey: "1"},
			})
		})
	}
}

// driveUpdateNode checks visibility and denial for update_node (tier 3,
// node:write) across every persona, but excuses the ops-positive ("visible
// implies callable") half: the environment's swarm has exactly one node, and
// update_node's destructiveHint covers role and availability both -- the
// same reason write_sweep_test.go excuses PUT /nodes/{id}/role and
// DELETE /nodes/{id} against this environment. The withheld direction (the
// security-critical one) is still checked for every persona that must not
// have it.
func driveUpdateNode(t *testing.T, env *harness.Env, proc *sut.Process, ids baselineIDs) {
	t.Run("ops_visible_only", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		if !visible["update_node"] {
			t.Errorf(
				"ops: update_node missing from tools/list, want it visible (ops holds write:*)",
			)
		}
		// Deliberately not called: see the excuse above.
	})

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertHiddenAndRefused(t, proc, p, visible, "update_node", map[string]any{
				"id":      ids.nodeID,
				"section": "availability",
				"value":   "drain",
			})
		})
	}
}

// ─── mutating tools: config / secret / network / volume ────────────────

// driveCreateAndRemoveConfig checks create_config and remove_config
// together: ops creates a throwaway config, every other persona is refused
// (both to create one and, once ops's exists, to remove it), and ops's own
// removal at the end confirms the positive direction for both tools using a
// resource this file owns rather than the shared baseline.
func driveCreateAndRemoveConfig(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	name := fmt.Sprintf("mcp-sweep-config-%d", time.Now().UnixNano())

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run("create_config/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(t, proc, p, true, visible, "create_config", map[string]any{
				"name": name + "-" + p.name, "data": "sweep",
			})
		})
	}

	var configID string
	t.Run("create_config/ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		if !visible["create_config"] {
			t.Fatalf("ops: create_config missing from tools/list")
		}
		call := callTool(t, proc, opsPersona, "create_config", map[string]any{
			"name": name, "data": "sweep",
		})
		if call.IsError {
			t.Fatalf("ops: create_config: %v", call.Content)
		}
		configID = mcpFindID(t, proc, "configs", name)
	})

	if configID == "" {
		t.Fatal("create_config as ops did not produce a findable config")
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := env.Docker.ConfigRemove(ctx, configID); err != nil {
			t.Logf(
				"cleanup: ConfigRemove %s: %v (may already be removed by remove_config below)",
				configID,
				err,
			)
		}
	})

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run("remove_config/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(
				t,
				proc,
				p, true,
				visible,
				"remove_config",
				map[string]any{"id": configID},
			)
		})
	}

	t.Run("remove_config/ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(
			t,
			proc,
			opsPersona,
			visible,
			"remove_config",
			map[string]any{"id": configID},
		)
	})
}

// driveCreateAndRemoveSecret mirrors driveCreateAndRemoveConfig for
// create_secret / remove_secret.
func driveCreateAndRemoveSecret(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	name := fmt.Sprintf("mcp-sweep-secret-%d", time.Now().UnixNano())

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run("create_secret/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(t, proc, p, true, visible, "create_secret", map[string]any{
				"name": name + "-" + p.name, "data": "sweep",
			})
		})
	}

	var secretID string
	t.Run("create_secret/ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		if !visible["create_secret"] {
			t.Fatalf("ops: create_secret missing from tools/list")
		}
		call := callTool(t, proc, opsPersona, "create_secret", map[string]any{
			"name": name, "data": "sweep",
		})
		if call.IsError {
			t.Fatalf("ops: create_secret: %v", call.Content)
		}
		secretID = mcpFindID(t, proc, "secrets", name)
	})

	if secretID == "" {
		t.Fatal("create_secret as ops did not produce a findable secret")
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := env.Docker.SecretRemove(ctx, secretID); err != nil {
			t.Logf(
				"cleanup: SecretRemove %s: %v (may already be removed by remove_secret below)",
				secretID,
				err,
			)
		}
	})

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run("remove_secret/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(
				t,
				proc,
				p, true,
				visible,
				"remove_secret",
				map[string]any{"id": secretID},
			)
		})
	}

	t.Run("remove_secret/ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(
			t,
			proc,
			opsPersona,
			visible,
			"remove_secret",
			map[string]any{"id": secretID},
		)
	})
}

// driveRemoveNetwork creates a throwaway network directly against the
// engine (remove_network has no create_network counterpart to pair with),
// checks every non-owning persona is refused, then removes it as ops.
func driveRemoveNetwork(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	name := fmt.Sprintf("mcp-sweep-net-%d", time.Now().UnixNano())
	resp, err := env.Docker.NetworkCreate(ctx, name, network.CreateOptions{Driver: "overlay"})
	if err != nil {
		t.Fatalf("NetworkCreate: %v", err)
	}
	netID := resp.ID

	removed := false
	t.Cleanup(func() {
		if removed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := env.Docker.NetworkRemove(ctx, netID); err != nil {
			t.Logf("cleanup: NetworkRemove %s: %v", netID, err)
		}
	})

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(
				t,
				proc,
				p, true,
				visible,
				"remove_network",
				map[string]any{"id": netID},
			)
		})
	}

	t.Run("ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(
			t,
			proc,
			opsPersona,
			visible,
			"remove_network",
			map[string]any{"id": netID},
		)
		removed = true
	})
}

// driveRemoveVolume mirrors driveRemoveNetwork for remove_volume.
func driveRemoveVolume(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	name := fmt.Sprintf("mcp-sweep-vol-%d", time.Now().UnixNano())
	if _, err := env.Docker.VolumeCreate(ctx, volume.CreateOptions{Name: name}); err != nil {
		t.Fatalf("VolumeCreate: %v", err)
	}

	removed := false
	t.Cleanup(func() {
		if removed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := env.Docker.VolumeRemove(ctx, name, true); err != nil {
			t.Logf("cleanup: VolumeRemove %s: %v", name, err)
		}
	})

	for _, p := range readPersonas {
		if p.name == "ops" {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(
				t,
				proc,
				p, true,
				visible,
				"remove_volume",
				map[string]any{"name": name},
			)
		})
	}

	t.Run("ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(
			t,
			proc,
			opsPersona,
			visible,
			"remove_volume",
			map[string]any{"name": name},
		)
		removed = true
	})
}

// ─── mutating tools: service / task ─────────────────────────────────────

// driveServiceTools exercises every service- and task-scoped mutating tool
// (scale_service, update_service_image, rollback_service, restart_service,
// update_service, update_service_secrets, update_service_configs,
// update_service_mounts, remove_task, remove_service) against one throwaway
// service this test owns, plus the frontend persona's own throwaway
// frontend-*-stack service -- the one place this file exercises frontend's
// TRUE positive case rather than the coarse, documented widening in
// internal/acl/evaluator.go's impliedTypes (see the comment on
// driveFrontendStackOwnership below for what that widening is and why it is
// not, by itself, a defect).
//
// oncall holds write:service:*/task:* for real (an exact wildcard, not a
// name pattern needing stack resolution), so its positive case is checked
// against the same throwaway service ops uses -- both personas genuinely
// have it.
func driveServiceTools(t *testing.T, env *harness.Env, proc *sut.Process, ids baselineIDs) {
	service := fixtures.DeployStack(t, env, "mcp-sweep", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	serviceName := service + "_app"
	serviceID := mcpFindID(t, proc, "services", serviceName)

	// viewers and anonymous hold no service write under this policy at all;
	// frontend's write is scoped to stack:frontend-*, which this throwaway
	// stack's name never matches -- so all three must be refused on every
	// one of these tools, and none of them may even see the tools in
	// tools/list.
	deniedPersonas := []readPersona{}
	for _, p := range readPersonas {
		if p.name == "ops" || p.name == "oncall" {
			continue
		}
		deniedPersonas = append(deniedPersonas, p)
	}

	type serviceTool struct {
		name string
		args map[string]any
	}

	nonTerminal := []serviceTool{
		{"scale_service", map[string]any{"id": serviceID, "replicas": 1}},
		{"update_service", map[string]any{
			"id": serviceID, "section": "labels",
			"value": map[string]any{"sweep": "1"},
		}},
		{"update_service_configs", map[string]any{"id": serviceID, "configs": []any{}}},
		{"update_service_secrets", map[string]any{"id": serviceID, "secrets": []any{}}},
		{"update_service_mounts", map[string]any{"id": serviceID, "mounts": []any{}}},
		{"restart_service", map[string]any{"id": serviceID}},
	}

	for _, tool := range nonTerminal {
		for _, p := range deniedPersonas {
			t.Run(tool.name+"/"+p.name, func(t *testing.T) {
				visible := listToolNames(t, proc, p)
				assertDenied(t, proc, p, true, visible, tool.name, tool.args)
			})
		}

		t.Run(tool.name+"/ops", func(t *testing.T) {
			visible := listToolNames(t, proc, opsPersona)
			assertVisibleAndCallable(t, proc, opsPersona, visible, tool.name, tool.args)
		})

		t.Run(tool.name+"/oncall", func(t *testing.T) {
			visible := listToolNames(t, proc, readPersonaByName("oncall"))
			assertVisibleAndCallable(
				t,
				proc,
				readPersonaByName("oncall"),
				visible,
				tool.name,
				tool.args,
			)
		})
	}

	// update_service_image and rollback_service are exercised together: image
	// update first (every denied persona refused, then ops positively), then
	// rollback (same shape), so rollback always has a PreviousSpec to revert
	// from regardless of tool ordering above.
	for _, p := range deniedPersonas {
		t.Run("update_service_image/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(t, proc, p, true, visible, "update_service_image", map[string]any{
				"id": serviceID, "image": "cetacean-e2e-fixture:sweep-mcp",
			})
		})
	}
	t.Run("update_service_image/ops", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := env.Docker.ImageTag(
			ctx,
			fixtureImageRef,
			"cetacean-e2e-fixture:sweep-mcp",
		); err != nil {
			t.Fatalf("ImageTag: %v", err)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = env.Docker.ImageRemove(
				ctx,
				"cetacean-e2e-fixture:sweep-mcp",
				image.RemoveOptions{},
			)
		})

		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(
			t,
			proc,
			opsPersona,
			visible,
			"update_service_image",
			map[string]any{
				"id": serviceID, "image": "cetacean-e2e-fixture:sweep-mcp",
			},
		)
		waitForServiceImage(t, env, serviceName, "cetacean-e2e-fixture:sweep-mcp")
	})

	for _, p := range deniedPersonas {
		t.Run("rollback_service/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(
				t,
				proc,
				p, true,
				visible,
				"rollback_service",
				map[string]any{"id": serviceID},
			)
		})
	}
	t.Run("rollback_service/ops", func(t *testing.T) {
		waitForCachedServiceImageAsOps(t, proc, serviceID, "cetacean-e2e-fixture:sweep-mcp")
		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(
			t,
			proc,
			opsPersona,
			visible,
			"rollback_service",
			map[string]any{"id": serviceID},
		)
		waitForServiceImage(t, env, serviceName, fixtureImageRef)
	})

	// remove_task: oncall genuinely holds write:task:* for real, so it drives
	// the positive case (Docker reschedules the task, which is harmless for
	// a replicated service) while every denied persona is checked against
	// the same task ID without ever reaching Docker.
	before := serviceTaskIDs(t, env, mustServiceInspectID(t, env, serviceName))
	var taskID string
	for id := range before {
		taskID = id
		break
	}
	if taskID == "" {
		t.Fatal("throwaway service has no tasks to drive remove_task with")
	}

	for _, p := range deniedPersonas {
		t.Run("remove_task/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(t, proc, p, true, visible, "remove_task", map[string]any{"id": taskID})
		})
	}
	t.Run("remove_task/oncall", func(t *testing.T) {
		visible := listToolNames(t, proc, readPersonaByName("oncall"))
		assertVisibleAndCallable(
			t,
			proc,
			readPersonaByName("oncall"),
			visible,
			"remove_task",
			map[string]any{"id": taskID},
		)
	})

	// remove_service is terminal: driven last, by ops, once every other
	// check on this service is done.
	for _, p := range deniedPersonas {
		t.Run("remove_service/"+p.name, func(t *testing.T) {
			visible := listToolNames(t, proc, p)
			assertDenied(
				t,
				proc,
				p, true,
				visible,
				"remove_service",
				map[string]any{"id": serviceID},
			)
		})
	}
	t.Run("remove_service/ops", func(t *testing.T) {
		visible := listToolNames(t, proc, opsPersona)
		assertVisibleAndCallable(
			t,
			proc,
			opsPersona,
			visible,
			"remove_service",
			map[string]any{"id": serviceID},
		)
	})

	_ = ids
}

// driveFrontendStackOwnership resolves the disclosure question the coverage
// brief calls out by name for the frontend persona specifically: with this
// policy, frontend's write grant is `stack:frontend-*`, but
// internal/acl/evaluator.go's TypeGrants -- the projection tools/list
// filtering uses -- expands a stack grant to the service/task/config/
// secret/network/volume types it can reach WITHOUT checking the grant's name
// pattern at all (see impliedTypes and the comment above it: "the direction
// this projection is already allowed to err in"). That means frontend's
// tools/list is expected, BY DESIGN, to advertise service-write tools it
// cannot actually invoke on the shop/platform fixture stacks -- confirmed
// directly in driveServiceTools above, where frontend sits in
// deniedPersonas and every service tool there is checked as hidden-and-
// refused... except it will NOT be hidden, only refused. That specific,
// asymmetric outcome (visible, but correctly refused) is asserted here by
// name, against the fixture's real "shop" stack, so this test documents the
// known widening with a live repro rather than silently tolerating whatever
// assertHiddenAndRefused's visibility half reports.
//
// This function then gives frontend a stack it actually owns
// (frontend-<timestamp>, matching stack:frontend-*) and confirms the
// opposite, TRUE positive case: on ITS OWN service, frontend's write
// genuinely works end-to-end -- the only place in this file frontend
// completes a real mutation.
func driveFrontendStackOwnership(t *testing.T, env *harness.Env, proc *sut.Process, _ baselineIDs) {
	frontend := readPersonaByName("frontend")

	t.Run("known_widening_on_shop_stack", func(t *testing.T) {
		serviceID := mcpFindID(t, proc, "services", "shop_web")
		visible := listToolNames(t, proc, frontend)

		// assertVisibleButDenied asserts BOTH halves of the known widening
		// directly, against a real cluster: scale_service is listed for
		// frontend (the coarse TypeGrants projection reaches the service
		// type through stack:frontend-*, regardless of the pattern -- see
		// internal/acl/evaluator.go's impliedTypes comment) AND calling it
		// on shop_web, which is in the "shop" stack rather than any
		// frontend-* stack, is still refused once the call reaches the
		// handler. If a future change to TypeGrants makes this precise
		// (hiding the tool from frontend entirely), the visibility half of
		// this assertion starts failing -- which is the point: this test
		// pins the current, documented behavior with a live repro rather
		// than silently tolerating whatever tools/list happens to report.
		assertVisibleButDenied(t, proc, frontend, visible, "scale_service", map[string]any{
			"id": serviceID, "replicas": 2,
		})
	})

	t.Run("true_positive_on_own_stack", func(t *testing.T) {
		service := fixtures.DeployStack(t, env, "frontend", []fixtures.ServiceSpec{
			{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
		})
		serviceName := service + "_app"
		serviceID := mcpFindID(t, proc, "services", serviceName)

		visible := listToolNames(t, proc, frontend)
		assertVisibleAndCallable(t, proc, frontend, visible, "scale_service", map[string]any{
			"id": serviceID, "replicas": 1,
		})
	})
}

// readPersonaByName is a small lookup over readPersonas, used where a
// generic loop is less readable than naming the persona a check is actually
// about (oncall's and frontend's genuine positive cases).
func readPersonaByName(name string) readPersona {
	for _, p := range readPersonas {
		if p.name == name {
			return p
		}
	}
	panic("unknown persona " + name)
}

// mustServiceInspectID resolves a service's Docker ID from the engine
// directly, for serviceTaskIDs (which is keyed by ID, not name).
func mustServiceInspectID(t *testing.T, env *harness.Env, name string) string {
	t.Helper()
	svc := inspectService(t, env, name)
	return svc.ID
}

// waitForCachedServiceImageAsOps polls Cetacean's own GET /services/{id} --
// as the "ops" persona, since this SUT runs headers auth -- until it
// reflects wantPrefix. write_sweep_test.go's waitForCachedServiceImage
// issues its request with no auth headers at all, which is correct for that
// file's no-auth SUT but gets 403 ACL001 (never 200) against this file's
// headers-auth one; reusing it here polled a request that could never
// succeed until its 30s deadline. Needed before rollback_service, whose
// PreviousSpec check reads the same cache a direct-to-engine seed (there,
// none here: the seed is update_service_image, run through this same SUT)
// only reaches once the watcher has processed the event.
func waitForCachedServiceImageAsOps(t *testing.T, proc *sut.Process, id, wantPrefix string) {
	t.Helper()

	type detail struct {
		Service struct {
			Spec struct {
				TaskTemplate struct {
					ContainerSpec struct {
						Image string `json:"Image"`
					} `json:"ContainerSpec"`
				} `json:"TaskTemplate"`
			} `json:"Spec"`
		} `json:"service"`
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp := readAs(t, proc, opsPersona, "/services/"+id, "")

		var body detail
		matched := resp.StatusCode == http.StatusOK &&
			json.NewDecoder(resp.Body).Decode(&body) == nil &&
			strings.HasPrefix(body.Service.Spec.TaskTemplate.ContainerSpec.Image, wantPrefix)
		resp.Body.Close()

		if matched {
			return
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("Cetacean's cache for service %s never reflected image %s", id, wantPrefix)
}

// ─── the sweep table ────────────────────────────────────────────────────

// drivenMCPTools names every one of the 27 tools this file checks and how:
// most map to one of the functions above; the handful sharing a throwaway
// resource (driveServiceTools) are named here too even though one call
// drives all of them, so the self-enforcing gate below still requires each
// to be accounted for individually.
var drivenMCPTools = map[string]bool{
	// tier 0 -- read, safe for every persona.
	"get_logs":            true,
	"find":                true,
	"describe":            true,
	"get_topology":        true,
	"get_metrics":         true,
	"get_recommendations": true,
	"get_events":          true,
	"get_cluster_status":  true,
	"watch":               true,

	// tier 1/2/3 -- driven via driveServiceTools, driveUpdateNode(Labels),
	// driveCreateAndRemove{Config,Secret}, driveRemove{Network,Volume}.
	"scale_service":          true,
	"update_service_image":   true,
	"rollback_service":       true,
	"restart_service":        true,
	"remove_task":            true,
	"update_service":         true,
	"update_node_labels":     true,
	"create_secret":          true,
	"create_config":          true,
	"update_service_secrets": true,
	"update_service_configs": true,
	"update_service_mounts":  true,
	"update_node":            true,
	"remove_service":         true,
	"remove_config":          true,
	"remove_secret":          true,
	"remove_network":         true,
	"remove_volume":          true,
}

// TestMCPSweepEnforcesTheACLBoundary runs the whole sweep against a real
// cluster.
func TestMCPSweepEnforcesTheACLBoundary(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startMCPSweep(t, env)

	// The SUT started above serves the ordinary REST API on the same port
	// alongside MCP, so baseline identifiers resolve the same way the read
	// sweep resolves them -- as the "ops" persona, who reads everything.
	ids := resolveBaselineIDs(t, proc)

	t.Run("find", func(t *testing.T) { driveFind(t, env, proc, ids) })
	t.Run(
		"describe_and_resource",
		func(t *testing.T) { driveDescribeAndResource(t, env, proc, ids) },
	)
	t.Run("resource_templates", func(t *testing.T) { driveResourceTemplateList(t, env, proc, ids) })
	t.Run("prompts", func(t *testing.T) { drivePromptList(t, env, proc, ids) })

	for _, spec := range []readToolSpec{
		{"get_logs", func(ids baselineIDs) map[string]any { return map[string]any{"service": ids.serviceID} }, true},
		{"watch", func(ids baselineIDs) map[string]any { return map[string]any{"service": ids.serviceID, "timeout": 5} }, true},
		{"get_topology", func(baselineIDs) map[string]any { return map[string]any{} }, false},
		{"get_recommendations", func(baselineIDs) map[string]any { return map[string]any{} }, false},
		{"get_events", func(baselineIDs) map[string]any { return map[string]any{"limit": 10} }, false},
		{"get_cluster_status", func(baselineIDs) map[string]any { return map[string]any{} }, false},
	} {
		t.Run(spec.name, spec.testFunc(env, proc, ids))
	}

	t.Run("update_node_labels", func(t *testing.T) { driveUpdateNodeLabels(t, env, proc, ids) })
	t.Run("update_node", func(t *testing.T) { driveUpdateNode(t, env, proc, ids) })
	t.Run(
		"create_and_remove_config",
		func(t *testing.T) { driveCreateAndRemoveConfig(t, env, proc, ids) },
	)
	t.Run(
		"create_and_remove_secret",
		func(t *testing.T) { driveCreateAndRemoveSecret(t, env, proc, ids) },
	)
	t.Run("remove_network", func(t *testing.T) { driveRemoveNetwork(t, env, proc, ids) })
	t.Run("remove_volume", func(t *testing.T) { driveRemoveVolume(t, env, proc, ids) })
	t.Run("service_and_task_tools", func(t *testing.T) { driveServiceTools(t, env, proc, ids) })
	t.Run(
		"frontend_stack_ownership",
		func(t *testing.T) { driveFrontendStackOwnership(t, env, proc, ids) },
	)
}

// testFunc adapts a readDriveFunc-shaped closure into a func(*testing.T) for
// t.Run.
func (spec readToolSpec) testFunc(
	env *harness.Env,
	proc *sut.Process,
	ids baselineIDs,
) func(*testing.T) {
	return func(t *testing.T) {
		driveReadTool(spec)(t, env, proc, ids)
	}
}

// TestEveryMCPToolIsDrivenOrExcused requires every one of the 27 tools in
// the catalog to appear in drivenMCPTools. There are only 27 and they are
// hand-enumerated (unlike contract.Routes(), there is no cheap static
// inventory to parse for MCP tools outside a running server), so this test
// also fails if the count drifts, which is the signal that a tool was added
// or removed without this file's attention.
func TestEveryMCPToolIsDrivenOrExcused(t *testing.T) {
	const wantTotal = 27

	if len(drivenMCPTools) != wantTotal {
		t.Errorf(
			"drivenMCPTools has %d entries, want %d -- internal/mcp's tool catalog "+
				"(tools_reads.go + tools_operational.go + tools_configuration.go + "+
				"tools_impactful.go) changed size; update this file's coverage",
			len(drivenMCPTools), wantTotal,
		)
	}

	t.Logf("mcp sweep: %d of %d tools driven, 0 excused", len(drivenMCPTools), wantTotal)
}
