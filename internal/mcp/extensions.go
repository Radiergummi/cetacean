package mcp

// Extension identifiers and media types from the 2026-07-28 extensions
// framework. Declared in ServerCapabilities.extensions so a host knows what we
// support before it calls anything.
const (
	// extensionTasks is the official Tasks extension: long-running tool calls
	// that return a handle the client polls with tasks/get.
	extensionTasks = "io.modelcontextprotocol/tasks"

	// extensionUI is the MCP Apps extension: interactive HTML widgets rendered
	// in a sandboxed iframe by the host.
	extensionUI = "io.modelcontextprotocol/ui"

	// uiMIMEType is the media type MCP Apps resources must declare.
	uiMIMEType = "text/html;profile=mcp-app"
)

// serverExtensions returns the extension capability map advertised on
// server/discover and on the legacy initialize result.
func serverExtensions() map[string]any {
	return map[string]any{
		extensionTasks: map[string]any{},
		extensionUI: map[string]any{
			"mimeTypes": []string{uiMIMEType},
		},
	}
}
