package mcp

import (
	"encoding/json"
	"testing"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestCacheTTLsAreConservative(t *testing.T) {
	// Lists are cache-backed and cheap to rebuild; a long TTL would leave an
	// agent acting on a stale cluster. Reads are per-resource and change more
	// often still.
	if cacheTTLList > time.Minute {
		t.Errorf("list TTL %v too long for live cluster state", cacheTTLList)
	}

	if cacheTTLRead > cacheTTLList {
		t.Errorf("read TTL %v should not exceed list TTL %v", cacheTTLRead, cacheTTLList)
	}
}

// readCacheHints decodes just the cacheable envelope of a result. The concrete
// result types cannot be unmarshalled — ReadResourceResult.Contents is an
// interface — but CacheableResult carries exactly the SEP-2549 fields and
// nothing that resists decoding.
func readCacheHints(t *testing.T, raw json.RawMessage, method string) mcplib.CacheableResult {
	t.Helper()

	var hints mcplib.CacheableResult
	if err := json.Unmarshal(raw, &hints); err != nil {
		t.Fatalf("decode %s result: %v", method, err)
	}

	if _, ok := hints.TTL(); !ok {
		t.Fatalf("%s result carries no ttlMs; SEP-2549 requires it", method)
	}

	// Cetacean filters every response by the caller's ACL, so a shared
	// intermediary must never reuse one identity's view for another.
	if hints.CacheScope != mcplib.CacheScopePrivate {
		t.Fatalf("%s cacheScope = %q, want %q", method, hints.CacheScope, mcplib.CacheScopePrivate)
	}

	return hints
}

// TestCacheHintsOnModernResults drives the real dispatch path for every method
// SEP-2549 covers. A unit test on the constants alone would not catch an
// unwired server option.
func TestCacheHintsOnModernResults(t *testing.T) {
	handler := newTestServer(t).Handler()

	for _, tc := range []struct {
		method string
		params string
		wantMs int64
	}{
		{"tools/list", `{}`, cacheTTLList.Milliseconds()},
		{"resources/list", `{}`, cacheTTLList.Milliseconds()},
		{"resources/templates/list", `{}`, cacheTTLList.Milliseconds()},
		{"resources/read", `{"uri":"cetacean://cluster"}`, cacheTTLRead.Milliseconds()},
	} {
		t.Run(tc.method, func(t *testing.T) {
			_, env := mcpModern(t, handler, 2, tc.method, tc.params)
			if env.Error != nil {
				t.Fatalf("%s error: %+v", tc.method, env.Error)
			}

			hints := readCacheHints(t, env.Result, tc.method)
			if ttl, _ := hints.TTL(); ttl != tc.wantMs {
				t.Fatalf("%s ttlMs = %d, want %d", tc.method, ttl, tc.wantMs)
			}
		})
	}
}
