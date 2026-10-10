package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/spec"
)

// legacyHeaders are what a 2025-11-25 client sends after its handshake.
var legacyHeaders = map[string]string{mcplib.HeaderProtocolVersion: LegacyProtocolVersion}

func legacyInitialize(
	t *testing.T,
	handler http.Handler,
	version string,
) (int, http.Header, jsonrpcEnvelope) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{
		"jsonrpc":"2.0","id":1,"method":"initialize",
		"params":{"protocolVersion":"`+version+`","capabilities":{},"clientInfo":{"name":"old","version":"1"}}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	result, env := sendMCP(t, handler, req)

	return result.StatusCode, result.Header, env
}

func TestLegacyInitializeNegotiatesTheLegacyRevision(t *testing.T) {
	handler := newTestServer(t).Handler()

	t.Run("a supported version is echoed", func(t *testing.T) {
		spec.Satisfies(t, "mcp/legacy-2025-11-25/initialize-echoes-a-supported-version")

		assertNegotiated(t, handler, LegacyProtocolVersion)
	})

	for _, requested := range []string{"2025-06-18", "2024-11-05", "2026-07-28", "1999-01-01"} {
		t.Run("asking for "+requested, func(t *testing.T) {
			spec.Satisfies(t,
				"mcp/legacy-2025-11-25/initialize-answers-a-supported-version",
				"mcp/legacy-2025-11-25/initialize-answers-the-latest-version",
			)

			assertNegotiated(t, handler, requested)
		})
	}
}

func assertNegotiated(t *testing.T, handler http.Handler, requested string) {
	t.Helper()

	status, header, env := legacyInitialize(t, handler, requested)
	if status != http.StatusOK || env.Error != nil {
		t.Fatalf("initialize refused: status %d, error %+v", status, env.Error)
	}

	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}

	if result.ProtocolVersion != LegacyProtocolVersion {
		t.Errorf("negotiated %q, want %q", result.ProtocolVersion, LegacyProtocolVersion)
	}

	if id := header.Get(mcplib.HeaderSessionID); id != "" {
		t.Errorf("initialize minted session %q; the legacy subset is sessionless", id)
	}
}

// Nothing reaches a legacy client unasked, so a capability promising a push
// would leave it waiting forever.
func TestLegacyInitializeWithdrawsPushCapabilities(t *testing.T) {
	_, _, env := legacyInitialize(t, newTestServer(t).Handler(), LegacyProtocolVersion)

	var result struct {
		Capabilities map[string]map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}

	for _, capability := range []string{"resources", "tools", "prompts"} {
		for _, flag := range []string{"subscribe", "listChanged"} {
			if result.Capabilities[capability][flag] == true {
				t.Errorf("legacy initialize advertises %s.%s", capability, flag)
			}
		}
	}

	if _, ok := result.Capabilities["tools"]; !ok {
		t.Error("legacy initialize lost the tools capability itself")
	}
}

// After the handshake a client calls with the header and no session.
func TestLegacyRequestIsServed(t *testing.T) {
	handler := newTestServer(t).Handler()

	status, env := postRaw(t, handler,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, legacyHeaders)
	if status != http.StatusOK || env.Error != nil {
		t.Fatalf("legacy tools/list refused: status %d, error %+v", status, env.Error)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil || len(result.Tools) == 0 {
		t.Fatalf("legacy tools/list returned no tools: %s (%v)", env.Result, err)
	}

	notification := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	notification.Header.Set("Content-Type", "application/json")
	notification.Header.Set("Accept", "application/json, text/event-stream")
	notification.Header.Set(mcplib.HeaderProtocolVersion, LegacyProtocolVersion)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, notification)

	if rec.Code != http.StatusAccepted {
		t.Errorf("notifications/initialized: status %d, want 202", rec.Code)
	}
}

// The 2025-11-25 schema types a schema's properties as objects, and the stock
// SDK refuses a whole tools/list over one boolean subschema.
func TestToolSchemaPropertiesAreObjects(t *testing.T) {
	srv := newToolTestServer(t, cache.New(nil), &fakeWriteClient{}, config.OpsImpactful)

	_, env := postRaw(t, srv.Handler(),
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, legacyHeaders)

	var result struct {
		Tools []struct {
			Name         string                              `json:"name"`
			InputSchema  struct{ Properties map[string]any } `json:"inputSchema"`
			OutputSchema struct{ Properties map[string]any } `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	for _, tool := range result.Tools {
		for kind, properties := range map[string]map[string]any{
			"input":  tool.InputSchema.Properties,
			"output": tool.OutputSchema.Properties,
		} {
			for name, schema := range properties {
				if _, ok := schema.(map[string]any); !ok {
					t.Errorf(
						"%s: %s property %q is %v, not a schema object",
						tool.Name,
						kind,
						name,
						schema,
					)
				}
			}
		}
	}
}

// tasks/list would answer with every caller's tasks: stateless, it has no
// session to scope them to. The capability is not advertised either.
func TestLegacyTasksListIsNotFound(t *testing.T) {
	_, env := postRaw(t, newTestServer(t).Handler(),
		`{"jsonrpc":"2.0","id":3,"method":"tasks/list","params":{}}`, legacyHeaders)

	if env.Error == nil || env.Error.Code != mcplib.METHOD_NOT_FOUND {
		t.Fatalf(
			"legacy tasks/list: got error %+v, want code %d",
			env.Error,
			mcplib.METHOD_NOT_FOUND,
		)
	}
}

func TestUnsupportedLegacyVersionIsRefused(t *testing.T) {
	handler := newTestServer(t).Handler()

	t.Run("an older revision", func(t *testing.T) {
		spec.Satisfies(t, "mcp/legacy-2025-11-25/unsupported-version-header-is-400")

		assertRefusedWithSupported(t, handler,
			map[string]string{mcplib.HeaderProtocolVersion: "2025-06-18"})
	})

	t.Run("no header outside the handshake", func(t *testing.T) {
		spec.Satisfies(t, "mcp/legacy-2025-11-25/missing-header-assumes-2025-03-26")

		assertRefusedWithSupported(t, handler, nil)
	})
}

func assertRefusedWithSupported(t *testing.T, handler http.Handler, headers map[string]string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var envelope struct {
		Error *struct {
			Code int `json:"code"`
			Data struct {
				Supported []string `json:"supported"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode rejection: %v (body %q)", err, rec.Body.String())
	}

	if rec.Code != http.StatusBadRequest || envelope.Error == nil {
		t.Fatalf("status %d, error %+v; want 400 with an error", rec.Code, envelope.Error)
	}

	if envelope.Error.Code != mcplib.UNSUPPORTED_PROTOCOL_VERSION {
		t.Errorf("code = %d, want %d", envelope.Error.Code, mcplib.UNSUPPORTED_PROTOCOL_VERSION)
	}

	if !slices.Equal(envelope.Error.Data.Supported, SupportedProtocolVersions) {
		t.Errorf(
			"supported = %v, want %v",
			envelope.Error.Data.Supported,
			SupportedProtocolVersions,
		)
	}
}

func TestLegacyStreamAndSessionTerminationAre405(t *testing.T) {
	handler := newTestServer(t).Handler()

	t.Run("GET", func(t *testing.T) {
		spec.Satisfies(t, "mcp/legacy-2025-11-25/get-is-a-stream-or-405")

		assertLegacy405(t, handler, http.MethodGet)
	})

	t.Run("DELETE", func(t *testing.T) {
		spec.Satisfies(t, "mcp/legacy-2025-11-25/delete-may-be-405")

		assertLegacy405(t, handler, http.MethodDelete)
	})
}

func assertLegacy405(t *testing.T, handler http.Handler, method string) {
	t.Helper()

	req := httptest.NewRequest(method, "/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set(mcplib.HeaderProtocolVersion, LegacyProtocolVersion)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", rec.Code)
	}

	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Errorf("Allow = %q, want POST", allow)
	}
}
