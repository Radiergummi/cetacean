// Package cluster provides shared domain logic used by both the REST API
// and the MCP server: task enrichment, service state derivation, secret
// redaction, and cross-resource search. Keeping these as transport-neutral
// helpers prevents the two transports from drifting in what they expose to
// callers.
package cluster
