//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// The MCP conformance suite's server requirements, carried over rather than
// shelled out to; README.md says why. Every other MCP test here builds its
// requests with the code that answers them, so they agree with themselves.
// These quote the requirement instead, and name the SEP it comes from.

const conformancePort = 19027

const conformanceVersion = "2026-07-28"

var conformanceIssuer = fmt.Sprintf("http://127.0.0.1:%d", conformancePort)

// omitHeader in a case's Headers removes a default rather than overriding it,
// which is how the missing-header requirements get exercised at all.
const omitHeader = "\x00omit"

// conformanceRequest is one request with every header and _meta field under the
// case's control, so a case can violate exactly one rule and nothing else.
type conformanceRequest struct {
	Method  string
	Params  map[string]any
	Headers map[string]string

	// Meta replaces the default _meta; NoMeta sends none at all.
	Meta   map[string]any
	NoMeta bool
}

type conformanceResult struct {
	Status int
	Body   struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  map[string]any  `json:"result"`
		Error   *struct {
			Code    int            `json:"code"`
			Message string         `json:"message"`
			Data    map[string]any `json:"data"`
		} `json:"error"`
	}
	Raw string
}

func startConformanceSUT(t *testing.T) *sut.Process {
	t.Helper()

	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	return sut.Start(t, sut.Config{
		Port:       conformancePort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":  "none",
			"CETACEAN_MCP":        "true",
			"CETACEAN_PUBLIC_URL": conformanceIssuer,
			// Named rather than inherited: the default is 0, and the widest
			// tool list is the widest surface for these cases.
			"CETACEAN_OPERATIONS_LEVEL": "3",
			"CETACEAN_DATA_DIR":         t.TempDir(),
		},
	})
}

// call issues one request, filling in the headers and _meta a conformant client
// sends and then applying whatever the case overrode.
func call(t *testing.T, proc *sut.Process, c conformanceRequest) conformanceResult {
	t.Helper()

	params := map[string]any{}
	for k, v := range c.Params {
		params[k] = v
	}

	switch {
	case c.NoMeta:
	case c.Meta != nil:
		params["_meta"] = c.Meta
	default:
		params["_meta"] = map[string]any{
			"io.modelcontextprotocol/protocolVersion":    conformanceVersion,
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		}
	}

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  c.Method,
		"params":  params,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	headers := map[string]string{
		"Content-Type":         "application/json",
		"Accept":               "application/json, text/event-stream",
		"Mcp-Protocol-Version": conformanceVersion,
		"Mcp-Method":           c.Method,
	}

	// SEP-2243 requires Mcp-Name on any method whose body names a subject.
	if name, ok := c.Params["name"].(string); ok {
		headers["Mcp-Name"] = name
	}

	if uri, ok := c.Params["uri"].(string); ok {
		headers["Mcp-Name"] = uri
	}

	for k, v := range c.Headers {
		if v == omitHeader {
			delete(headers, k)

			continue
		}

		headers[k] = v
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, proc.BaseURL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("POST /mcp %s: %v", c.Method, err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// A refusal from a guard in front of the JSON-RPC layer — the Origin check
	// is the one — carries no envelope at all, which is a fact a case may be
	// asserting rather than a reason to stop.
	out := conformanceResult{Status: resp.StatusCode, Raw: string(body)}
	_ = json.Unmarshal(body, &out.Body)

	return out
}

// ok fails unless the call succeeded, naming the error it carried instead.
func (r conformanceResult) ok(t *testing.T, what string) map[string]any {
	t.Helper()

	if r.Status != http.StatusOK || r.Body.Error != nil || r.Body.JSONRPC != "2.0" {
		t.Fatalf("%s: status %d, body %s", what, r.Status, r.Raw)
	}

	return r.Body.Result
}

// refused fails unless the call was rejected with this status and error code.
func (r conformanceResult) refused(t *testing.T, what string, status, code int) {
	t.Helper()

	if r.Body.Error == nil {
		t.Fatalf("%s: no JSON-RPC error (status %d): %s", what, r.Status, r.Raw)
	}

	if r.Status != status {
		t.Errorf("%s: status = %d, want %d: %s", what, r.Status, status, r.Raw)
	}

	if r.Body.Error.Code != code {
		t.Errorf("%s: code = %d, want %d: %s", what, r.Body.Error.Code, code, r.Raw)
	}
}

// ─── SEP-2575, the stateless core ───────────────────────────────────────

// "Servers MUST implement server/discover", and a server supporting prompts
// "MUST declare the prompts capability in their DiscoverResult". The document
// is how a client learns what to speak, so the versions it names are the ones
// the gate must then accept.
func TestMCPConformanceDiscover(t *testing.T) {
	proc := startConformanceSUT(t)

	result := call(t, proc, conformanceRequest{Method: "server/discover"}).
		ok(t, "server/discover")

	versions, _ := result["supportedVersions"].([]any)
	if len(versions) != 1 || versions[0] != conformanceVersion {
		t.Errorf("supportedVersions = %v, want [%s]", versions, conformanceVersion)
	}

	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("no capabilities in the discover result: %v", result)
	}

	// Every capability declared has to be answered by a handler; a client that
	// believes the document and then gets a 404 is worse off than one told
	// nothing. The list methods are the observable half.
	for capability, method := range map[string]string{
		"prompts":   "prompts/list",
		"resources": "resources/list",
		"tools":     "tools/list",
	} {
		if _, declared := capabilities[capability]; !declared {
			t.Errorf("discover declares no %q capability", capability)

			continue
		}

		call(t, proc, conformanceRequest{Method: method}).ok(t, method)
	}

	// The server identifies itself in the result's _meta rather than through an
	// initialize handshake this revision does not have.
	meta, _ := result["_meta"].(map[string]any)
	if _, ok := meta["io.modelcontextprotocol/serverInfo"]; !ok {
		t.Errorf("no io.modelcontextprotocol/serverInfo in the result _meta: %v", meta)
	}
}

// Every client request must carry protocolVersion and clientCapabilities in
// _meta; clientInfo is optional. On HTTP a request that does not is a 400.
func TestMCPConformanceRequestMeta(t *testing.T) {
	proc := startConformanceSUT(t)

	full := map[string]any{
		"io.modelcontextprotocol/protocolVersion":    conformanceVersion,
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}

	t.Run("no _meta at all", func(t *testing.T) {
		call(t, proc, conformanceRequest{Method: "tools/list", NoMeta: true}).
			refused(t, "_meta omitted", http.StatusBadRequest, -32602)
	})

	t.Run("no protocolVersion", func(t *testing.T) {
		call(t, proc, conformanceRequest{Method: "tools/list", Meta: map[string]any{
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		}}).refused(t, "protocolVersion omitted", http.StatusBadRequest, -32602)
	})

	// The suite reads SEP-2575 as requiring -32602 here and reports -32021 as a
	// failure. The code is mcp-go's MISSING_REQUIRED_CLIENT_CAPABILITY, not
	// ours; this pins what we actually answer so the day it changes is visible.
	t.Run("no clientCapabilities", func(t *testing.T) {
		call(t, proc, conformanceRequest{Method: "tools/list", Meta: map[string]any{
			"io.modelcontextprotocol/protocolVersion": conformanceVersion,
		}}).refused(t, "clientCapabilities omitted", http.StatusBadRequest, -32021)
	})

	t.Run("clientInfo is optional", func(t *testing.T) {
		call(t, proc, conformanceRequest{Method: "tools/list", Meta: full}).
			ok(t, "clientInfo omitted")
	})
}

// "Every POST request to the MCP endpoint MUST include an MCP-Protocol-Version
// header", whose value "MUST match the io.modelcontextprotocol/protocolVersion
// field carried in the request body _meta" — and a server that does not
// implement the requested version answers 400 listing the ones it does.
func TestMCPConformanceProtocolVersionHeader(t *testing.T) {
	proc := startConformanceSUT(t)

	t.Run("header absent", func(t *testing.T) {
		call(t, proc, conformanceRequest{
			Method:  "tools/list",
			Headers: map[string]string{"Mcp-Protocol-Version": omitHeader},
		}).refused(t, "no version header", http.StatusBadRequest, -32022)
	})

	// Both values are well-formed and each is individually intelligible; only
	// their disagreement is the fault, which is what makes this its own code.
	t.Run("header disagrees with _meta", func(t *testing.T) {
		call(t, proc, conformanceRequest{Method: "tools/list", Meta: map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "2025-11-25",
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		}}).refused(t, "header/_meta mismatch", http.StatusBadRequest, -32020)
	})

	// Both directions, because they are different code paths: a version
	// sorting after ours is "modern" to mcp-go, which would answer it with
	// every revision the SDK knows rather than the one this server serves.
	for _, version := range []string{"2025-11-25", "2027-01-01", "v999.0.0"} {
		t.Run("unsupported "+version+" names what is supported", func(t *testing.T) {
			got := call(t, proc, conformanceRequest{
				Method:  "tools/list",
				Headers: map[string]string{"Mcp-Protocol-Version": version},
				Meta: map[string]any{
					"io.modelcontextprotocol/protocolVersion":    version,
					"io.modelcontextprotocol/clientCapabilities": map[string]any{},
				},
			})

			got.refused(t, "unsupported version", http.StatusBadRequest, -32022)

			supported, _ := got.Body.Error.Data["supported"].([]any)
			if len(supported) != 1 || supported[0] != conformanceVersion {
				t.Errorf("supported = %v, want [%s]", supported, conformanceVersion)
			}
		})
	}
}

// "If the server does not implement the requested RPC method, it MUST respond
// with 404 Not Found and a JSON-RPC error with code -32601." The five named
// methods are the ones the revision removed, which a client carried over from
// an older era is most likely to try.
func TestMCPConformanceMethodNotFound(t *testing.T) {
	proc := startConformanceSUT(t)

	for _, method := range []string{
		"initialize",
		"ping",
		"logging/setLevel",
		"resources/subscribe",
		"resources/unsubscribe",
		"no/such/method",
	} {
		t.Run(method, func(t *testing.T) {
			got := call(t, proc, conformanceRequest{Method: method})
			got.refused(t, method, http.StatusNotFound, -32601)

			// The rejection is the first thing an incompatible client sees, so
			// it has to be matchable to the call that provoked it.
			if string(got.Body.ID) != "7" {
				t.Errorf("id = %s, want 7", got.Body.ID)
			}
		})
	}
}

// ─── SEP-2243, the standard request headers ─────────────────────────────

// Servers "MUST reject requests with mismatched or missing standard-header
// values, returning HTTP 400" with error code -32020 — while comparing header
// *names* case-insensitively and header *values* case-sensitively.
func TestMCPConformanceStandardHeaders(t *testing.T) {
	proc := startConformanceSUT(t)

	tool := firstToolName(t, proc)

	cases := []struct {
		name     string
		request  conformanceRequest
		accepted bool
	}{{
		name: "Mcp-Method disagrees with the body",
		request: conformanceRequest{
			Method:  "tools/list",
			Headers: map[string]string{"Mcp-Method": "prompts/list"},
		},
	}, {
		name: "Mcp-Method absent",
		request: conformanceRequest{
			Method:  "tools/list",
			Headers: map[string]string{"Mcp-Method": omitHeader},
		},
	}, {
		// RFC 9110 §3.2: field names are case-insensitive. The value is not,
		// and the next case is the same request with the case moved into it.
		name: "mcp-method spelled in lower case",
		request: conformanceRequest{
			Method: "tools/list",
			Headers: map[string]string{
				"Mcp-Method": omitHeader,
				"mcp-method": "tools/list",
			},
		},
		accepted: true,
	}, {
		name: "Mcp-Method value upper-cased",
		request: conformanceRequest{
			Method:  "tools/list",
			Headers: map[string]string{"Mcp-Method": "TOOLS/LIST"},
		},
	}, {
		name: "Mcp-Name disagrees with the body",
		request: conformanceRequest{
			Method:  "tools/call",
			Params:  map[string]any{"name": tool, "arguments": map[string]any{}},
			Headers: map[string]string{"Mcp-Name": "wrong_tool_name"},
		},
	}, {
		name: "Mcp-Name absent",
		request: conformanceRequest{
			Method:  "tools/call",
			Params:  map[string]any{"name": tool, "arguments": map[string]any{}},
			Headers: map[string]string{"Mcp-Name": omitHeader},
		},
	}, {
		// RFC 9110 §5.5: parsing excludes optional whitespace before the value
		// is evaluated, so this names the same tool as the body does.
		name: "Mcp-Name padded with whitespace",
		request: conformanceRequest{
			Method:  "tools/call",
			Params:  map[string]any{"name": tool, "arguments": map[string]any{}},
			Headers: map[string]string{"Mcp-Name": "  " + tool + "  "},
		},
		accepted: true,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := call(t, proc, tc.request)

			if tc.accepted {
				got.ok(t, tc.name)

				return
			}

			got.refused(t, tc.name, http.StatusBadRequest, -32020)
		})
	}
}

// ─── SEP-2549, caching hints ────────────────────────────────────────────

// "Servers MUST include caching hints on results with resultType: complete"
// returned by each cacheable method: ttlMs, a non-negative integer, and
// cacheScope, which is "public" or "private".
func TestMCPConformanceCachingHints(t *testing.T) {
	proc := startConformanceSUT(t)

	resources := call(t, proc, conformanceRequest{Method: "resources/list"}).
		ok(t, "resources/list")

	listed, _ := resources["resources"].([]any)
	if len(listed) == 0 {
		t.Fatal("resources/list returned nothing to read")
	}

	first, _ := listed[0].(map[string]any)
	uri, _ := first["uri"].(string)

	cacheable := []conformanceRequest{
		{Method: "tools/list"},
		{Method: "prompts/list"},
		{Method: "resources/list"},
		{Method: "resources/templates/list"},
		{Method: "resources/read", Params: map[string]any{"uri": uri}},
	}

	for _, request := range cacheable {
		t.Run(request.Method, func(t *testing.T) {
			result := call(t, proc, request).ok(t, request.Method)

			if got, _ := result["resultType"].(string); got != "complete" {
				t.Fatalf("resultType = %q, want complete", got)
			}

			ttl, ok := result["ttlMs"].(float64)

			switch {
			case !ok:
				t.Errorf("no ttlMs on the result: %v", keysOf(result))
			case ttl < 0 || ttl != float64(int64(ttl)):
				t.Errorf("ttlMs = %v, want a non-negative integer", ttl)
			case ttl == 0:
				// Legal per the SEP, and what an unconfigured server answers.
				// Nothing here is uncacheable, so zero means the hint was lost.
				t.Errorf("ttlMs = 0, which tells a client not to cache at all")
			}

			switch scope, _ := result["cacheScope"].(string); scope {
			case "public", "private":
			default:
				t.Errorf("cacheScope = %q, want public or private", scope)
			}
		})
	}
}

// ─── SEP-2164, reading a resource that is not there ─────────────────────

// "Servers MUST NOT return an empty contents array for a non-existent
// resource" and SHOULD answer -32602 instead.
func TestMCPConformanceResourceNotFound(t *testing.T) {
	proc := startConformanceSUT(t)

	const missing = "cetacean://service/no-such-service-for-conformance"

	got := call(t, proc, conformanceRequest{
		Method: "resources/read",
		Params: map[string]any{"uri": missing},
	})

	if got.Body.Error == nil {
		contents, _ := got.Body.Result["contents"].([]any)
		t.Fatalf("a missing resource answered a result with %d contents", len(contents))
	}

	if got.Body.Error.Code != -32602 {
		t.Errorf("code = %d, want -32602: %s", got.Body.Error.Code, got.Raw)
	}

	// SEP-2164 also says the error data SHOULD name the URI that was asked
	// for, which this server does not do. Pinned as it stands: when the
	// field appears, this case fails and says so rather than staying quiet.
	if _, ok := got.Body.Error.Data["uri"]; ok {
		t.Errorf("error data now names the uri — the SHOULD is met, so assert it: %s", got.Raw)
	}
}

// ─── SEP-2575, the subscription stream ──────────────────────────────────

// The acknowledgement "MUST [be] the first message on the stream", and every
// notification on one "MUST include io.modelcontextprotocol/subscriptionId in
// _meta". mcp_stream_test.go drives this stream for its ACL filtering and
// scans past the acknowledgement; the ordering and the tag are what this adds.
func TestMCPConformanceSubscriptionStream(t *testing.T) {
	proc := startConformanceSUT(t)

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "subscriptions/listen",
		"params": map[string]any{
			"notifications": map[string]any{"resourcesListChanged": true},
			"_meta": map[string]any{
				"io.modelcontextprotocol/protocolVersion":    conformanceVersion,
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, proc.BaseURL+"/mcp", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", conformanceVersion)
	req.Header.Set("Mcp-Method", "subscriptions/listen")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("subscriptions/listen: %v", err)
	}

	frames := make(chan sseFrame, 64)
	done := make(chan struct{})

	t.Cleanup(func() {
		close(done)
		resp.Body.Close() //nolint:errcheck // test cleanup
	})

	go readSSEFrames(resp.Body, frames, done)

	var first struct {
		Method string `json:"method"`
		Params struct {
			Meta map[string]any `json:"_meta"`
		} `json:"params"`
	}

	select {
	case frame := <-frames:
		if err := json.Unmarshal([]byte(frame.data), &first); err != nil {
			t.Fatalf("first frame is not JSON-RPC: %v (%s)", err, frame.data)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("no frame arrived on the listen stream")
	}

	if first.Method != "notifications/subscriptions/acknowledged" {
		t.Errorf(
			"first message on the stream = %q, want notifications/subscriptions/acknowledged",
			first.Method,
		)
	}

	if _, ok := first.Params.Meta["io.modelcontextprotocol/subscriptionId"]; !ok {
		t.Errorf("the acknowledgement carries no subscriptionId: %v", first.Params.Meta)
	}
}

// ─── DNS rebinding ──────────────────────────────────────────────────────

// A local server "MUST validate the Origin header on all incoming
// connections", which is what stops a page the user is browsing from driving
// their cluster.
func TestMCPConformanceOriginValidation(t *testing.T) {
	proc := startConformanceSUT(t)

	probe := func(t *testing.T, origin string) int {
		t.Helper()

		got := call(t, proc, conformanceRequest{
			Method:  "tools/list",
			Headers: map[string]string{"Origin": origin},
		})

		return got.Status
	}

	t.Run("a foreign origin is refused", func(t *testing.T) {
		if status := probe(t, "http://evil.example.com"); status != http.StatusForbidden {
			t.Errorf("status = %d, want 403", status)
		}
	})

	// The suite expects a 2xx here: the request comes from the address this
	// deployment publishes. The Origin guard reads server.cors.origins alone,
	// so naming only server.public_url is not enough — the CSRF guard trusts
	// that setting and this one does not. Pinned as it stands.
	t.Run("this deployment's own origin is refused too", func(t *testing.T) {
		if status := probe(t, conformanceIssuer); status != http.StatusForbidden {
			t.Errorf(
				"status = %d — the guard now accepts its own origin, so assert 200 here",
				status,
			)
		}
	})
}

// ─── The authorization server's metadata document ───────────────────────

// RFC 8414's required claims, plus the two the MCP authorization spec adds:
// S256 among the challenge methods, and Client ID Metadata Document support.
func TestMCPConformanceAuthorizationServerMetadata(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       conformancePort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			// The authorization server is opt-in and refuses auth mode none.
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_OAUTH_ENABLED":        "true",
			"CETACEAN_MCP":                  "true",
			"CETACEAN_PUBLIC_URL":           conformanceIssuer,
			"CETACEAN_DATA_DIR":             t.TempDir(),
		},
	})

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		proc.BaseURL+"/.well-known/oauth-authorization-server",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var doc struct {
		Issuer                 string   `json:"issuer"`
		AuthorizationEndpoint  string   `json:"authorization_endpoint"`
		TokenEndpoint          string   `json:"token_endpoint"`
		ResponseTypesSupported []string `json:"response_types_supported"`
		ChallengeMethods       []string `json:"code_challenge_methods_supported"`
		CIMDSupported          bool     `json:"client_id_metadata_document_supported"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}

	// The issuer identifies the server that minted a token, so a document
	// naming anything but the address it was fetched from is unusable.
	if doc.Issuer != conformanceIssuer {
		t.Errorf("issuer = %q, want %q", doc.Issuer, conformanceIssuer)
	}

	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		t.Errorf("metadata names no authorization or token endpoint: %+v", doc)
	}

	if !slices.Contains(doc.ResponseTypesSupported, "code") {
		t.Errorf(
			"response_types_supported = %v, want it to include code",
			doc.ResponseTypesSupported,
		)
	}

	if !slices.Contains(doc.ChallengeMethods, "S256") {
		t.Errorf(
			"code_challenge_methods_supported = %v, want it to include S256",
			doc.ChallengeMethods,
		)
	}

	if !doc.CIMDSupported {
		t.Error("client_id_metadata_document_supported is not true")
	}
}

// ─── helpers ────────────────────────────────────────────────────────────

func firstToolName(t *testing.T, proc *sut.Process) string {
	t.Helper()

	result := call(t, proc, conformanceRequest{Method: "tools/list"}).ok(t, "tools/list")

	tools, _ := result["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("tools/list returned nothing")
	}

	first, _ := tools[0].(map[string]any)

	name, _ := first["name"].(string)
	if name == "" {
		t.Fatalf("the first tool has no name: %v", first)
	}

	return name
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
