//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// historyEntry is the subset of a /history row these cases compare on.
type historyEntry struct {
	Type       string `json:"type"`
	ResourceID string `json:"resourceId"`
	Name       string `json:"name"`
}

// historyFor reads GET /history for one identifier, which may be an ID or a
// name. resourceType is the `type` parameter; "" omits it.
func historyFor(
	t *testing.T,
	proc *sut.Process,
	resourceType, identifier string,
) []historyEntry {
	t.Helper()

	path := "/history?resourceId=" + identifier
	if resourceType != "" {
		path += "&type=" + resourceType
	}

	var body struct {
		Items []historyEntry `json:"items"`
	}

	resp := historyRequest(t, proc, path, &body)
	if resp != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", path, resp)
	}

	return body.Items
}

// historyRequest issues the GET and returns its status, decoding on 200 only.
// getJSON in api_test.go fatals on a decode failure, which is what a caller
// asserting on a non-200 status needs to avoid.
func historyRequest(t *testing.T, proc *sut.Process, path string, into any) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK && into != nil {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}

	return resp.StatusCode
}

// awaitHistory polls until an identifier has at least one entry. The ring is
// filled from the Docker event stream, so a resource created after the SUT
// started appears only once the watcher has processed its event.
func awaitHistory(
	t *testing.T,
	proc *sut.Process,
	resourceType, identifier string,
) []historyEntry {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if entries := historyFor(t, proc, resourceType, identifier); len(entries) > 0 {
			return entries
		}

		time.Sleep(time.Second)
	}

	t.Fatalf("no history for %q (type %q) within the deadline", identifier, resourceType)

	return nil
}

// resourceIDs reduces entries to the IDs they carry, which is what two
// timelines reached by different identifiers have to agree on. Comparing whole
// entries would compare timestamps the two reads are not guaranteed to share.
func resourceIDs(entries []historyEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ResourceID)
	}

	return ids
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// liveTaskOf returns a running task of the named service, and the name the
// cluster renders it under. The name is rebuilt from what /tasks reports
// rather than taken from a Cetacean response, so the case fails if the
// convention the resolver splits on ever parts ways with Docker's.
func liveTaskOf(t *testing.T, proc *sut.Process, service string) (id, name string) {
	t.Helper()

	var body struct {
		Items []struct {
			ID           string `json:"ID"`
			Slot         int    `json:"Slot"`
			ServiceName  string `json:"ServiceName"`
			DesiredState string `json:"DesiredState"`
		} `json:"items"`
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp := getJSON(t, proc, "/tasks?limit=200", &body) //nolint:bodyclose // closed in getJSON
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /tasks: status = %d, want 200", resp.StatusCode)
		}

		for _, item := range body.Items {
			if item.ServiceName == service && item.DesiredState == "running" {
				return item.ID, service + "." + strconv.Itoa(item.Slot)
			}
		}

		time.Sleep(time.Second)
	}

	t.Fatalf("no running task of %s within the deadline", service)

	return "", ""
}

// TestIdentifierResolutionAgainstALiveCluster drives the name resolution the
// canonical redirect cannot reach — a query parameter and an MCP argument —
// against services this SUT watched being created. The unit tests pin the
// semantics against a hand-built cache; what only a real cluster can show is
// that the names being split and compared are the ones Swarm actually renders.
func TestIdentifierResolutionAgainstALiveCluster(t *testing.T) {
	env, proc := startMCP(t)

	// Deployed after the SUT is up on purpose: the initial sync is not
	// recorded, so a stack that predates the process has no timeline to read
	// and every comparison below would hold vacuously.
	stack := fixtures.DeployStack(t, env, "resolve", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
		{Name: "agent", Global: true, Command: []string{"sleep infinity"}},
	})

	service := stack + "_app"
	global := stack + "_agent"

	id := serviceID(t, proc, service)

	t.Run("a service name reads the timeline its ID does", func(t *testing.T) {
		byName := awaitHistory(t, proc, "service", service)
		byID := historyFor(t, proc, "service", id)

		if !equalIDs(resourceIDs(byName), resourceIDs(byID)) {
			t.Errorf(
				"?resourceId=%s gave %v, ?resourceId=%s gave %v",
				service, resourceIDs(byName), id, resourceIDs(byID),
			)
		}

		for _, entry := range byName {
			if entry.ResourceID != id {
				t.Errorf("entry resourceId = %q, want the service ID %q", entry.ResourceID, id)
			}
		}
	})

	t.Run("a service name resolves without a type", func(t *testing.T) {
		// No type means the name has to be unambiguous across every type that
		// resolves one, which is the path an agent takes when the user named
		// a resource without saying what it is.
		entries := awaitHistory(t, proc, "", service)

		for _, entry := range entries {
			if entry.ResourceID != id {
				t.Errorf("entry resourceId = %q, want the service ID %q", entry.ResourceID, id)
			}
		}
	})

	t.Run("a task name rendered by the cluster resolves", func(t *testing.T) {
		taskID, taskName := liveTaskOf(t, proc, service)

		byName := awaitHistory(t, proc, "task", taskName)
		byID := historyFor(t, proc, "task", taskID)

		if !equalIDs(resourceIDs(byName), resourceIDs(byID)) {
			t.Errorf(
				"?resourceId=%s gave %v, ?resourceId=%s gave %v",
				taskName, resourceIDs(byName), taskID, resourceIDs(byID),
			)
		}
	})

	t.Run("get_events agrees with the REST timeline", func(t *testing.T) {
		rest := awaitHistory(t, proc, "service", id)

		var events struct {
			StructuredContent struct {
				Entries []struct {
					Type       string `json:"type"`
					ResourceID string `json:"resourceId"`
				} `json:"entries"`
			} `json:"structuredContent"`
		}

		raw := mcpCall(t, proc, "tools/call", map[string]any{
			"name": "get_events",
			"arguments": map[string]any{
				"types":    []string{"service"},
				"resource": service,
				"limit":    200,
			},
		})

		if err := json.Unmarshal(raw, &events); err != nil {
			t.Fatalf("decode get_events: %v", err)
		}

		ids := make([]string, 0, len(events.StructuredContent.Entries))
		for _, entry := range events.StructuredContent.Entries {
			ids = append(ids, entry.ResourceID)
		}

		if len(ids) == 0 {
			t.Fatalf("get_events(resource: %q) returned nothing; REST gave %d entries",
				service, len(rest))
		}

		for _, got := range ids {
			if got != id {
				t.Errorf("get_events resourceId = %q, want the service ID %q", got, id)
			}
		}

		if len(ids) != len(rest) {
			t.Errorf("get_events returned %d entries, /history returned %d", len(ids), len(rest))
		}
	})

	t.Run("a global service name resolves past its own task", func(t *testing.T) {
		// A global service's task renders as "<service>.<node>" once assigned,
		// so the name belongs to the service alone. This pins the converged
		// state; the window before Swarm assigns the task, where TaskName
		// renders the bare service name, is not reachable from here because
		// DeployStack has already waited for convergence.
		var events struct {
			StructuredContent struct {
				Entries []struct {
					Type       string `json:"type"`
					ResourceID string `json:"resourceId"`
				} `json:"entries"`
			} `json:"structuredContent"`
		}

		raw := mcpCall(t, proc, "tools/call", map[string]any{
			"name":      "get_events",
			"arguments": map[string]any{"resource": global, "limit": 200},
		})

		if err := json.Unmarshal(raw, &events); err != nil {
			t.Fatalf("decode get_events: %v", err)
		}

		entries := events.StructuredContent.Entries
		if len(entries) == 0 {
			t.Fatalf("get_events(resource: %q) returned nothing", global)
		}

		globalID := serviceID(t, proc, global)
		for _, entry := range entries {
			if entry.ResourceID != globalID {
				t.Errorf(
					"get_events resourceId = %q, want the service ID %q",
					entry.ResourceID, globalID,
				)
			}
		}
	})
}
