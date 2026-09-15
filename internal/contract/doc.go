// Package contract holds the invariants that cross Cetacean's surfaces:
// properties that must hold for every route, MCP tool and resource type. Every
// test loops over an inventory derived from a source of truth — the router's
// source, the live MCP server, the OpenAPI spec — never a hand-typed list.
package contract
