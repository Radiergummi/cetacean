package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// ProtocolVersion is the MCP revision Cetacean implements in full.
const ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION

// LegacyProtocolVersion is the one initialize-era revision served, and only its
// stateless subset: no session, no standalone stream, nothing pushed.
const LegacyProtocolVersion = mcplib.LATEST_LEGACY_PROTOCOL_VERSION

// SupportedProtocolVersions is every revision requireSupportedProtocol admits,
// newest first.
var SupportedProtocolVersions = []string{ProtocolVersion, LegacyProtocolVersion}

// maxIDSniffBytes bounds how much of a request body we read to recover its
// JSON-RPC method and id. Both sit at the top of the envelope, so this is
// generous; a body larger than this is malformed for our purposes anyway.
const maxIDSniffBytes = 64 << 10

// requireSupportedProtocol admits the revisions in SupportedProtocolVersions
// and nothing else. No session is ever minted, so a request naming one is
// refused whatever its version.
func (s *Server) requireSupportedProtocol(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := r.Header.Get(mcplib.HeaderProtocolVersion)

		switch {
		case r.Header.Get(mcplib.HeaderSessionID) != "":
			writeUnsupportedProtocolVersion(w, sniffEnvelope(r).ID, version)
		case version == ProtocolVersion:
			next.ServeHTTP(w, r)
		case version == LegacyProtocolVersion || version == "":
			serveLegacy(w, r, version, next)
		default:
			writeUnsupportedProtocolVersion(w, sniffEnvelope(r).ID, version)
		}
	})
}

// serveLegacy holds a legacy request to what a stateless server can honour. An
// initialize may omit the header, as the handshake has no version yet; nothing
// else may. tasks/list is refused because it would list every caller's tasks.
func serveLegacy(w http.ResponseWriter, r *http.Request, version string, next http.Handler) {
	if r.Method != http.MethodPost {
		if version == "" {
			writeUnsupportedProtocolVersion(w, nil, version)

			return
		}

		// Neither the standalone stream nor session termination is offered.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	envelope := sniffEnvelope(r)

	switch {
	case version == "" && envelope.Method != mcplib.MethodInitialize:
		writeUnsupportedProtocolVersion(w, envelope.ID, version)
	case envelope.Method == mcplib.MethodTasksList:
		writeJSONRPCError(w, envelope.ID, http.StatusOK, mcplib.NewJSONRPCErrorDetails(
			mcplib.METHOD_NOT_FOUND, "Method not found", nil))
	default:
		next.ServeHTTP(w, r)
	}
}

// installLegacyInitializeHooks answers every initialize with LegacyProtocolVersion
// and withdraws the capabilities that need a stream the legacy era never gets.
// The version hook relies on the handler receiving the hooked request.
func installLegacyInitializeHooks(h *mcpserver.Hooks) {
	h.AddBeforeInitialize(func(_ context.Context, _ any, msg *mcplib.InitializeRequest) {
		msg.Params.ProtocolVersion = LegacyProtocolVersion
	})
	h.AddAfterInitialize(
		func(_ context.Context, _ any, _ *mcplib.InitializeRequest, result *mcplib.InitializeResult) {
			if resources := result.Capabilities.Resources; resources != nil {
				resources.Subscribe = false
				resources.ListChanged = false
			}

			if tools := result.Capabilities.Tools; tools != nil {
				tools.ListChanged = false
			}

			if prompts := result.Capabilities.Prompts; prompts != nil {
				prompts.ListChanged = false
			}
		},
	)
}

// envelope is the part of a JSON-RPC message the gate decides on.
type envelope struct {
	ID     any              `json:"id"`
	Method mcplib.MCPMethod `json:"method"`
}

// sniffEnvelope reads the method and id off the head of the body and puts what
// it read back, so the handler behind still sees the whole message. A body it
// cannot parse yields the zero envelope, whose id renders as null.
func sniffEnvelope(r *http.Request) envelope {
	var parsed envelope

	if r.Body == nil {
		return parsed
	}

	head, err := io.ReadAll(io.LimitReader(r.Body, maxIDSniffBytes))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(head), r.Body))

	if err == nil {
		_ = json.Unmarshal(head, &parsed)
	}

	return parsed
}

// writeUnsupportedProtocolVersion answers with a JSON-RPC error naming the
// versions this server implements, echoing the request id so a client can
// correlate the failure with the call it made.
func writeUnsupportedProtocolVersion(w http.ResponseWriter, id any, requested string) {
	response := mcplib.UnsupportedProtocolVersionError{
		Version:   requested,
		Supported: SupportedProtocolVersions,
	}.JSONRPCError()

	writeJSONRPCError(w, id, http.StatusBadRequest, response.Error)
}

func writeJSONRPCError(
	w http.ResponseWriter,
	id any,
	status int,
	details mcplib.JSONRPCErrorDetails,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(mcplib.JSONRPCError{
		JSONRPC: mcplib.JSONRPC_VERSION,
		ID:      mcplib.NewRequestId(id),
		Error:   details,
	})
}
