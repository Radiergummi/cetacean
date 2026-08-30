package mcp

import "time"

// Cache TTLs advertised on cacheable results (SEP-2549). They are freshness
// hints, not correctness guarantees: a client may serve a cached response for
// this long before re-fetching. Both are deliberately short — Cetacean mirrors
// live cluster state, and an agent acting on a minute-old service list can
// scale the wrong thing. listChanged notifications remain the primary
// invalidation signal; these hints only bound how long a client that missed one
// stays stale.
//
// mcp-go applies them to tools/list, resources/list, resources/templates/list,
// resources/read and server/discover, and omits them for clients on protocol
// versions older than 2026-07-28, where the fields do not exist.
const (
	cacheTTLList = 30 * time.Second
	cacheTTLRead = 10 * time.Second
)
