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

// GET /history and get_events read one window through cluster.ParseTimeWindow,
// so for the same bounds they return the same entries, boundaries included.
func TestHistoryWindowAgreesAcrossTransports(t *testing.T) {
	w := NewWorld(t)

	// Long before the seed, so only these entries fall in any window below.
	base := time.Date(2001, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, name := range []string{"m0", "m1", "m2", "m3", "m4"} {
		w.Cache.History().Append(cache.HistoryEntry{
			Type:       cache.EventService,
			Action:     "update",
			ResourceID: name,
			Name:       name,
			Timestamp:  base.Add(time.Duration(i) * time.Minute),
		})
	}

	at := func(minute int) string {
		return base.Add(time.Duration(minute) * time.Minute).Format(time.RFC3339)
	}

	for _, window := range []struct {
		name         string
		since, until string
		want         []string
	}{
		{"on both boundaries", at(1), at(3), []string{"m3", "m2"}},
		{"since alone", at(3), "", []string{"m4"}},
		{"until alone", "", at(1), []string{"m1", "m0"}},
	} {
		t.Run(window.name, func(t *testing.T) {
			query := url.Values{"limit": {"200"}}
			args := map[string]any{"limit": 500}

			if window.since != "" {
				query.Set("since", window.since)
				args["since"] = window.since
			}
			if window.until != "" {
				query.Set("until", window.until)
				args["until"] = window.until
			}

			overREST := windowNames(t, restHistory(t, w, query))
			overMCP := windowNames(t, mcpEvents(t, w, args))

			if !slices.Equal(overREST, overMCP) {
				t.Fatalf("REST %v, MCP %v", overREST, overMCP)
			}
			if !slices.Equal(overREST, window.want) {
				t.Errorf("entries = %v, want %v", overREST, window.want)
			}
		})
	}
}

func TestMalformedHistoryWindowIsRefusedByBothTransports(t *testing.T) {
	w := NewWorld(t)

	resp := w.REST(t, "ops", http.MethodGet, "/history?since=yesterday")
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("REST status = %d, want 400", resp.StatusCode)
	}

	result, rpcErr := w.MCP(t, "ops", "tools/call", map[string]any{
		"name":      "get_events",
		"arguments": map[string]any{"since": "yesterday"},
	})

	if rpcErr != nil {
		return
	}

	var call struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &call); err != nil {
		t.Fatalf("decode get_events: %v", err)
	}
	if !call.IsError {
		t.Error("get_events accepted a since that is not RFC 3339")
	}
}

type windowEntry struct {
	Name string `json:"name"`
}

func restHistory(t *testing.T, w *World, query url.Values) []windowEntry {
	t.Helper()

	resp := w.REST(t, "ops", http.MethodGet, "/history?"+query.Encode())
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /history: status %d", resp.StatusCode)
	}

	var page struct {
		Items []windowEntry `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode /history: %v", err)
	}

	return page.Items
}

func mcpEvents(t *testing.T, w *World, args map[string]any) []windowEntry {
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
		Entries []windowEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(call.Content[0].Text), &events); err != nil {
		t.Fatalf("decode get_events result: %v", err)
	}

	return events.Entries
}

// windowNames keeps the entries this test appended, newest first, so the seed's
// own history never decides the comparison.
func windowNames(t *testing.T, entries []windowEntry) []string {
	t.Helper()

	var names []string
	for _, e := range entries {
		if len(e.Name) == 2 && e.Name[0] == 'm' {
			names = append(names, e.Name)
		}
	}

	slices.Sort(names)
	slices.Reverse(names)

	return names
}
