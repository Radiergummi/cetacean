package contract

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// scopeBase is long before the seed, so a window around it holds only the
// entries a test appends.
var scopeBase = time.Date(2001, 1, 1, 12, 0, 0, 0, time.UTC)

// seedScopeHistory records a change to the seeded service, one to its task
// and one to the node, so a scope has something to keep and something to drop.
func seedScopeHistory(w *World) {
	for i, e := range []cache.HistoryEntry{
		{Type: cache.EventService, ResourceID: SeededServiceID, Name: SeededServiceName},
		{Type: cache.EventTask, ResourceID: SeededTaskID, Name: SeededTaskID, ServiceID: SeededServiceID},
		{Type: cache.EventNode, ResourceID: SeededNodeID, Name: SeededNodeName},
	} {
		e.Kind, e.Action = cache.KindChange, "update"
		e.Timestamp = scopeBase.Add(time.Duration(i) * time.Minute)
		w.Cache.History().Append(e)
	}
}

// GET /history and get_events build their query through one scope, so the
// same scope asked of either returns the same rows.
func TestTimelineScopeAgreesAcrossTransports(t *testing.T) {
	w := NewWorld(t)
	seedScopeHistory(w)

	for _, tc := range []struct {
		name string
		rest url.Values
		mcp  map[string]any
		want []string
	}{
		{
			"a stack holds its services' changes",
			url.Values{"type": {"stack"}, "resourceId": {SeededStack}},
			map[string]any{"types": []string{"stack"}, "resource": SeededStack},
			[]string{"service:" + SeededServiceID},
		},
		{
			"a service holds its tasks'",
			url.Values{"type": {"service"}, "resourceId": {SeededServiceName}},
			map[string]any{"types": []string{"service", "task"}, "resource": SeededServiceName},
			[]string{"service:" + SeededServiceID, "task:" + SeededTaskID},
		},
		{
			"a service without its tasks",
			url.Values{
				"type": {"service"}, "resourceId": {SeededServiceName}, "tasks": {"false"},
			},
			map[string]any{"types": []string{"service"}, "resource": SeededServiceName},
			[]string{"service:" + SeededServiceID},
		},
		{
			"an empty tag or author is no filter",
			url.Values{"tag": {""}, "author": {""}, "kind": {""}},
			map[string]any{"tags": []string{""}, "authors": []string{""}, "kinds": []string{""}},
			[]string{
				"node:" + SeededNodeID, "service:" + SeededServiceID, "task:" + SeededTaskID,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			since := scopeBase.Add(-time.Hour).Format(time.RFC3339)
			until := scopeBase.Add(time.Hour).Format(time.RFC3339)

			tc.rest.Set("since", since)
			tc.rest.Set("until", until)
			tc.mcp["since"], tc.mcp["until"] = since, until

			overREST := scopeRows(restScopeHistory(t, w, tc.rest))
			overMCP := scopeRows(mcpScopeEvents(t, w, tc.mcp))

			if !slices.Equal(overREST, overMCP) {
				t.Fatalf("REST %v, MCP %v", overREST, overMCP)
			}
			if !slices.Equal(overREST, tc.want) {
				t.Errorf("rows = %v, want %v", overREST, tc.want)
			}
		})
	}
}

// Several types, one of them stack, still read the stack's services.
func TestStackAmongSeveralTypesHoldsItsServices(t *testing.T) {
	w := NewWorld(t)
	seedScopeHistory(w)

	rows := scopeRows(mcpScopeEvents(t, w, map[string]any{
		"types":    []string{"stack", "node"},
		"resource": SeededStack,
		"since":    scopeBase.Add(-time.Hour).Format(time.RFC3339),
		"until":    scopeBase.Add(time.Hour).Format(time.RFC3339),
	}))

	if !slices.Equal(rows, []string{"service:" + SeededServiceID}) {
		t.Errorf("rows = %v, want the stack's service", rows)
	}
}

// A caller holding no grant reads no timeline over either transport, gaps
// and trackingSince included.
func TestTimelineNeedsAnyGrantOverBothTransports(t *testing.T) {
	w := NewWorld(t)
	w.Cache.History().Append(cache.HistoryEntry{
		Kind: cache.KindGap, Timestamp: scopeBase, Until: scopeBase.Add(time.Minute),
	})

	resp := w.REST(t, AnonymousPersona, http.MethodGet, "/history")
	resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("REST status = %d, want 403", resp.StatusCode)
	}

	result, rpcErr := w.MCP(t, AnonymousPersona, "tools/call", map[string]any{
		"name": "get_events", "arguments": map[string]any{},
	})
	if rpcErr == nil {
		var call struct {
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal(result, &call); err != nil {
			t.Fatalf("decode get_events: %v", err)
		}
		if !call.IsError {
			t.Error("get_events answered a caller with no grant")
		}
	}

	if _, rpcErr := w.MCP(t, AnonymousPersona, "resources/read", map[string]any{
		"uri": "cetacean://history",
	}); rpcErr == nil {
		t.Error("cetacean://history answered a caller with no grant")
	}
}

type scopeEntry struct {
	Kind       string `json:"kind"`
	Type       string `json:"type"`
	ResourceID string `json:"resourceId"`
}

func restScopeHistory(t *testing.T, w *World, query url.Values) []scopeEntry {
	t.Helper()

	resp := w.REST(t, "ops", http.MethodGet, "/history?"+query.Encode())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /history?%s: status %d", query.Encode(), resp.StatusCode)
	}

	var page struct {
		Items []scopeEntry `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode /history: %v", err)
	}

	return page.Items
}

func mcpScopeEvents(t *testing.T, w *World, args map[string]any) []scopeEntry {
	t.Helper()

	result, rpcErr := w.MCP(t, "ops", "tools/call", map[string]any{
		"name":      "get_events",
		"arguments": args,
	})
	if rpcErr != nil {
		t.Fatalf("get_events: %s", rpcErr.Message)
	}

	var call struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &call); err != nil {
		t.Fatalf("decode get_events: %v", err)
	}
	if call.IsError || len(call.Content) == 0 {
		t.Fatalf("get_events refused: %+v", call)
	}

	var events struct {
		Entries []scopeEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(call.Content[0].Text), &events); err != nil {
		t.Fatalf("decode get_events result: %v", err)
	}

	return events.Entries
}

// scopeRows names each change as type:resourceId, sorted, since the two
// transports order the same rows differently.
func scopeRows(entries []scopeEntry) []string {
	rows := []string{}
	for _, e := range entries {
		if e.Kind == cache.KindChange {
			rows = append(rows, e.Type+":"+e.ResourceID)
		}
	}

	slices.Sort(rows)

	return rows
}
