package mcp

import (
	"encoding/json"
	"io"
	"net/http"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// ProtocolVersion is the single MCP revision Cetacean implements. Older
// revisions are refused by requireModernProtocol.
const ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION

// maxIDSniffBytes bounds how much of a rejected request body we read to recover
// its JSON-RPC id. The id sits at the top of the envelope, so this is generous;
// a body larger than this is malformed for our purposes anyway, and the reply
// simply carries a null id.
const maxIDSniffBytes = 64 << 10

// requireModernProtocol rejects any request not on protocol 2026-07-28 or
// later. mcp-go would negotiate down, but the older eras are session-based
// while the notification path here is stateless: such a client subscribes
// successfully and then receives nothing. The header alone decides.
func (s *Server) requireModernProtocol(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version := r.Header.Get(mcplib.HeaderProtocolVersion)
		if mcplib.IsModernProtocol(version) && r.Header.Get(mcplib.HeaderSessionID) == "" {
			next.ServeHTTP(w, r)

			return
		}

		writeUnsupportedProtocolVersion(w, r, version)
	})
}

// writeUnsupportedProtocolVersion answers with a JSON-RPC error naming the one
// version this server implements, echoing the request id so a client can
// correlate the failure with the call it made.
func writeUnsupportedProtocolVersion(w http.ResponseWriter, r *http.Request, requested string) {
	response := mcplib.UnsupportedProtocolVersionError{
		Version:   requested,
		Supported: []string{mcplib.LATEST_PROTOCOL_VERSION},
	}.JSONRPCError()

	response.ID = mcplib.NewRequestId(requestIDFromBody(r))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(response)
}

// requestIDFromBody recovers the JSON-RPC id of the request being rejected, so
// the error can be matched to it. Returns nil for a body that carries none —
// a notification, or something unparseable — which renders as a null id.
func requestIDFromBody(r *http.Request) any {
	if r.Body == nil {
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxIDSniffBytes))
	if err != nil {
		return nil
	}

	var envelope struct {
		ID any `json:"id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}

	return envelope.ID
}
