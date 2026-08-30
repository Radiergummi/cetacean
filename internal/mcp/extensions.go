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
// server/discover.
//
// An extension goes in here only once the capability behind it is actually
// wired, because a host takes this list as a promise: advertising Tasks without
// mcpserver.WithTaskCapabilities means a host calls tasks/get and is told the
// method does not exist, and advertising UI without a ui:// resource means it
// looks for a widget that was never registered. Both are worse than saying
// nothing — a host that reads no extension simply uses the core protocol.
//
// The map is empty today. Tasks is added by the phase that enables task
// capabilities, and UI by the phase that registers the first widget resource;
// extensionsMatchWiring in the tests fails if either arrives without the other.
func serverExtensions() map[string]any {
	return map[string]any{}
}
