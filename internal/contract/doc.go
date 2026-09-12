// Package contract holds the invariants that cross Cetacean's surfaces:
// properties that must hold for every route, MCP tool and resource type.
//
// Every test loops over an inventory derived from a source of truth -- the
// router's source, the live MCP server, the OpenAPI spec -- never a hand-typed
// list, so a route added without coverage fails an inventory test. Assertions
// come from those inventories and the spec, never from what world.go's fixture
// happens to hold.
package contract
