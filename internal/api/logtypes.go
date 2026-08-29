package api

import "github.com/radiergummi/cetacean/internal/logs"

// LogLine is the wire shape for a single log line. It is an alias rather than
// a distinct type so the JSON produced here and by the MCP transport cannot
// drift, and so the OpenAPI schema keeps describing one thing.
type LogLine = logs.LogLine
