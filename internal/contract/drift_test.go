package contract

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// specKey is the route as api/openapi.yaml spells it: {$} anchors a ServeMux
// pattern to the exact path, and the spec names that path itself.
func specKey(route Route) string {
	return strings.TrimSuffix(route.String(), "{$}")
}

// compareInventories is the pure comparison at the heart of the drift check:
// given two inventories and the excuse maps, it reports every unexcused
// asymmetry and every excuse no longer needed. It touches no file, so it can be
// driven with synthetic inventories to prove the check can fail.
func compareInventories(
	routes []Route,
	operations []Operation,
	excusedUndoc map[string]string,
	excusedUnreg map[string]string,
) (undocumented, unregistered, staleUndoc, staleUnreg []string) {
	documented := make(map[string]bool, len(operations))
	for _, op := range operations {
		documented[op.String()] = true
	}

	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[specKey(route)] = true
	}

	for _, route := range routes {
		key := specKey(route)

		if documented[key] {
			if _, excused := excusedUndoc[key]; excused {
				staleUndoc = append(staleUndoc, fmt.Sprintf(
					"%s is documented but still listed in excusedUndocumented. "+
						"Remove the excuse: a stale excuse hides the next real drift.",
					key,
				))
			}

			continue
		}

		if reason, excused := excusedUndoc[key]; excused {
			if strings.TrimSpace(reason) == "" {
				undocumented = append(
					undocumented, fmt.Sprintf("%s is excused with an empty reason", key),
				)
			}

			continue
		}

		undocumented = append(undocumented, fmt.Sprintf(
			"%s is registered on the router but absent from api/openapi.yaml. "+
				"Document it, or add it to excusedUndocumented with a real reason.",
			key,
		))
	}

	for _, op := range operations {
		key := op.String()

		if registered[key] {
			if _, excused := excusedUnreg[key]; excused {
				staleUnreg = append(staleUnreg, fmt.Sprintf(
					"%s is registered but still listed in excusedUnregistered. "+
						"Remove the excuse.",
					key,
				))
			}

			continue
		}

		if reason, excused := excusedUnreg[key]; excused {
			if strings.TrimSpace(reason) == "" {
				unregistered = append(
					unregistered, fmt.Sprintf("%s is excused with an empty reason", key),
				)
			}

			continue
		}

		unregistered = append(unregistered, fmt.Sprintf(
			"%s is documented in api/openapi.yaml but not registered in "+
				"internal/api/router.go. Either the spec describes an endpoint "+
				"that does not exist, or it is registered elsewhere — in which "+
				"case add it to excusedUnregistered with that reason.",
			key,
		))
	}

	return undocumented, unregistered, staleUndoc, staleUnreg
}

// TestEveryRegisteredRouteIsDocumented walks the router → spec direction, which
// internal/api's spec → router check cannot see: a route registered and never
// documented is invisible to it. A legitimate asymmetry belongs in excused.go
// with a reason; drift fails.
func TestEveryRegisteredRouteIsDocumented(t *testing.T) {
	routes, err := Routes()
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	operations, err := SpecOperations()
	if err != nil {
		t.Fatalf("SpecOperations: %v", err)
	}

	undocumented, _, staleUndoc, _ := compareInventories(
		routes, operations, excusedUndocumented, excusedUnregistered,
	)

	for _, msg := range staleUndoc {
		t.Error(msg)
	}

	for _, msg := range undocumented {
		t.Error(msg)
	}
}

func TestEveryDocumentedOperationIsRegistered(t *testing.T) {
	routes, err := Routes()
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}

	operations, err := SpecOperations()
	if err != nil {
		t.Fatalf("SpecOperations: %v", err)
	}

	_, unregistered, _, staleUnreg := compareInventories(
		routes, operations, excusedUndocumented, excusedUnregistered,
	)

	for _, msg := range staleUnreg {
		t.Error(msg)
	}

	for _, msg := range unregistered {
		t.Error(msg)
	}
}

func TestSpecOperationsCoversTheDocumentedPaths(t *testing.T) {
	operations, err := SpecOperations()
	if err != nil {
		t.Fatalf("SpecOperations: %v", err)
	}

	if len(operations) < 90 {
		t.Errorf(
			"SpecOperations found %d operations; api/openapi.yaml documents 94 "+
				"paths and more operations than that. The parser is incomplete.",
			len(operations),
		)
	}

	if !slices.IsSortedFunc(operations, func(a, b Operation) int {
		return strings.Compare(a.String(), b.String())
	}) {
		t.Error("SpecOperations is not sorted; diffs against it would be unstable")
	}
}

// TestCompareInventoriesDetectsDrift drives compareInventories with synthetic
// inventories, proving the four failure shapes the drift check exists to catch —
// an undocumented route, an unregistered operation, and a stale excuse on each
// side — and that a properly excused asymmetry stays silent.
func TestCompareInventoriesDetectsDrift(t *testing.T) {
	routes := []Route{
		{Method: "GET", Pattern: "/documented"},
		{Method: "GET", Pattern: "/undocumented"},
		{Method: "GET", Pattern: "/excused-undoc"},
		{Method: "GET", Pattern: "/now-documented"},
		{Method: "GET", Pattern: "/now-registered"},
	}

	operations := []Operation{
		{Method: "GET", Path: "/documented"},
		{Method: "GET", Path: "/unregistered"},
		{Method: "GET", Path: "/excused-unreg"},
		{Method: "GET", Path: "/now-documented"},
		{Method: "GET", Path: "/now-registered"},
	}

	excusedUndoc := map[string]string{
		"GET /excused-undoc":  "deliberately undocumented, for the test",
		"GET /now-documented": "stale: the spec documents this now",
	}

	excusedUnreg := map[string]string{
		"GET /excused-unreg":  "deliberately unregistered, for the test",
		"GET /now-registered": "stale: the router registers this now",
	}

	undocumented, unregistered, staleUndoc, staleUnreg := compareInventories(
		routes, operations, excusedUndoc, excusedUnreg,
	)

	if !containsSubstring(undocumented, "GET /undocumented") {
		t.Errorf("expected undocumented to report GET /undocumented, got %v", undocumented)
	}

	if !containsSubstring(unregistered, "GET /unregistered") {
		t.Errorf("expected unregistered to report GET /unregistered, got %v", unregistered)
	}

	if !containsSubstring(staleUndoc, "GET /now-documented") {
		t.Errorf("expected staleUndoc to report GET /now-documented, got %v", staleUndoc)
	}

	if !containsSubstring(staleUnreg, "GET /now-registered") {
		t.Errorf("expected staleUnreg to report GET /now-registered, got %v", staleUnreg)
	}

	// The legitimately excused entries must not appear in any failure list.
	for _, list := range [][]string{undocumented, unregistered, staleUndoc, staleUnreg} {
		if containsSubstring(list, "GET /excused-undoc") {
			t.Errorf("GET /excused-undoc should be silently excused, got %v", list)
		}

		if containsSubstring(list, "GET /excused-unreg") {
			t.Errorf("GET /excused-unreg should be silently excused, got %v", list)
		}
	}
}

func containsSubstring(list []string, substr string) bool {
	for _, s := range list {
		if strings.Contains(s, substr) {
			return true
		}
	}

	return false
}
