// Package contract holds the invariants that cross Cetacean's surfaces:
// properties that must hold for every route, every MCP tool and every resource
// type rather than at one endpoint.
//
// Every test here is a loop over an inventory derived from a source of truth —
// the router's own source, the live MCP server, the OpenAPI spec — never a
// hand-typed list. That is what makes the coverage self-enforcing: a route or
// tool added without coverage fails an inventory test instead of silently going
// untested.
//
// Assertions are derived from those inventories and from the spec, never from
// what the fixture in world.go happens to hold. A test that asserts fixture
// contents passes for as long as nobody touches the fixture, and stops meaning
// anything the moment somebody does.
package contract
