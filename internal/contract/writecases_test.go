package contract

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
)

const engineAsked = "the engine was asked"

func refusingWorld(t *testing.T) *World {
	t.Helper()

	err := errors.New(engineAsked)

	return NewWorldWithWriters(t, refusingRESTWriter{err: err}, refusingMCPWriter{err: err})
}

func scaleOverREST(t *testing.T, w *World, persona, id string) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPut, w.Server.URL+"/services/"+id+"/scale",
		strings.NewReader(`{"replicas":3}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	setPersonaHeaders(t, req, persona)

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT scale: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(body)
}

func scaleOverMCP(t *testing.T, w *World, persona, id string) string {
	t.Helper()

	result, rpcErr := w.MCP(t, persona, "tools/call", map[string]any{
		"name":      "scale_service",
		"arguments": map[string]any{"id": id, "replicas": 3},
	})
	if rpcErr != nil {
		return rpcErr.Message
	}

	var call struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &call); err != nil {
		t.Fatalf("decode tools/call scale_service: %v", err)
	}
	if !call.IsError || len(call.Content) == 0 {
		t.Fatalf("scale_service succeeded: %s", result)
	}

	return call.Content[0].Text
}

// Both transports refuse scaling a global service from the cache, before
// Docker is asked, under the same rule.
func TestScalingAGlobalServiceIsRefusedAlike(t *testing.T) {
	w := refusingWorld(t)
	w.Cache.SetService(swarm.Service{
		ID: "svc-agent",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "agent"},
			Mode:        swarm.ServiceMode{Global: &swarm.GlobalService{}},
		},
	})

	status, overREST := scaleOverREST(t, w, "ops", "svc-agent")
	overMCP := scaleOverMCP(t, w, "ops", "svc-agent")

	if status != http.StatusBadRequest || !strings.Contains(overREST, "SVC005") {
		t.Errorf("REST = %d %s, want 400 SVC005", status, overREST)
	}
	if !strings.Contains(overMCP, "global") {
		t.Errorf("MCP = %q, want the global-mode refusal", overMCP)
	}
	for transport, answer := range map[string]string{"REST": overREST, "MCP": overMCP} {
		if strings.Contains(answer, engineAsked) {
			t.Errorf("%s asked Docker to scale a global service: %s", transport, answer)
		}
	}
}

// A write to a service the caller cannot read is answered as not found by
// both, so neither confirms that it exists.
func TestAWriteToAnUnreadableServiceIsNotFoundAlike(t *testing.T) {
	w := refusingWorld(t)

	policy, err := acl.ParsePolicy([]byte(`
grants:
  - resources: ["stack:frontend-*"]
    audience: ["group:frontend"]
    permissions: ["read", "write"]
`))
	if err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	w.ACL.SetPolicy(policy)

	status, overREST := scaleOverREST(t, w, "frontend", SeededServiceID)
	overMCP := scaleOverMCP(t, w, "frontend", SeededServiceID)

	if status != http.StatusNotFound || !strings.Contains(overREST, "SVC003") {
		t.Errorf("REST = %d %s, want 404 SVC003", status, overREST)
	}
	if !strings.Contains(overMCP, "not found") {
		t.Errorf("MCP = %q, want not found", overMCP)
	}
}

// A route keyed by {name} hides a record the caller cannot read as one keyed
// by {id} does.
func TestAWriteToAnUnreadableVolumeIsNotFound(t *testing.T) {
	w := refusingWorld(t)

	policy, err := acl.ParsePolicy([]byte(`
grants:
  - resources: ["stack:frontend-*"]
    audience: ["group:frontend"]
    permissions: ["read", "write"]
`))
	if err != nil {
		t.Fatalf("parse policy: %v", err)
	}
	w.ACL.SetPolicy(policy)

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodDelete, w.Server.URL+"/volumes/"+SeededVolumeName, nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	setPersonaHeaders(t, req, "frontend")

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE volume: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "VOL002") {
		t.Errorf(
			"DELETE /volumes/%s = %d %s, want 404 VOL002",
			SeededVolumeName,
			resp.StatusCode,
			body,
		)
	}
}
