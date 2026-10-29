package contract

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/api"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/mcp"
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

// Every record route answers a write the caller cannot read with the record's
// not-found, and one it can read but not write with ACL002.
func TestAWriteToAnUnreadableRecordIsNotFound(t *testing.T) {
	routes := []struct{ method, path, contentType, body, notFound string }{
		{
			http.MethodPut,
			"/services/" + SeededServiceID + "/image",
			"application/json",
			`{"image":"nginx:2"}`,
			"SVC003",
		},
		{http.MethodPost, "/services/" + SeededServiceID + "/rollback", "", "", "SVC003"},
		{http.MethodPost, "/services/" + SeededServiceID + "/restart", "", "", "SVC003"},
		{
			http.MethodPut,
			"/nodes/" + SeededNodeID + "/availability",
			"application/json",
			`{"availability":"drain"}`,
			"NOD003",
		},
		{
			http.MethodPatch,
			"/nodes/" + SeededNodeID + "/labels",
			"application/json-patch+json",
			`[{"op":"add","path":"/a","value":"b"}]`,
			"NOD003",
		},
		{
			http.MethodPut,
			"/nodes/" + SeededNodeID + "/role",
			"application/json",
			`{"role":"worker"}`,
			"NOD003",
		},
		{http.MethodDelete, "/nodes/" + SeededNodeID, "", "", "NOD003"},
		{http.MethodDelete, "/tasks/" + SeededTaskID, "", "", "TSK002"},
		{http.MethodDelete, "/plugins/sshfs", "", "", "PLG004"},
	}

	for _, grant := range []struct{ name, permissions string }{
		{"unreadable", ""},
		{"readable", `
  - resources: ["*"]
    audience: ["group:frontend"]
    permissions: ["read"]`},
	} {
		w := refusingWorld(t)
		policy, err := acl.ParsePolicy([]byte(`
grants:
  - resources: ["stack:frontend-*"]
    audience: ["group:frontend"]
    permissions: ["read", "write"]` + grant.permissions + "\n"))
		if err != nil {
			t.Fatalf("parse policy: %v", err)
		}
		w.ACL.SetPolicy(policy)

		for _, route := range routes {
			t.Run(grant.name+" "+route.method+" "+route.path, func(t *testing.T) {
				status, body := writeAs(
					t,
					w,
					"frontend",
					route.method,
					route.path,
					route.contentType,
					route.body,
				)

				want, code := http.StatusNotFound, route.notFound
				if grant.name == "readable" {
					want, code = http.StatusForbidden, "ACL002"
				}
				if status != want || !strings.Contains(body, code) {
					t.Errorf("= %d %s, want %d %s", status, body, want, code)
				}
			})
		}
	}
}

func writeAs(
	t *testing.T,
	w *World,
	persona, method, path, contentType, body string,
) (int, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(),
		method,
		w.Server.URL+path,
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	setPersonaHeaders(t, req, persona)

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	answer, _ := io.ReadAll(resp.Body)

	return resp.StatusCode, string(answer)
}

// liveSpecWriter holds the spec Docker has, which the cache may not show yet,
// and applies a section change to it as the real writer does.
type liveSpecWriter struct {
	live    swarm.ServiceSpec
	written *bool
}

func (f liveSpecWriter) UpdateServiceSpec(
	_ context.Context,
	id string,
	change func(*swarm.ServiceSpec) error,
) (swarm.Service, error) {
	encoded, _ := json.Marshal(f.live)
	var spec swarm.ServiceSpec
	_ = json.Unmarshal(encoded, &spec)
	if err := change(&spec); err != nil {
		return swarm.Service{}, err
	}
	*f.written = true

	return swarm.Service{ID: id, Spec: spec}, nil
}

type liveRESTWriter struct {
	api.DockerWriteClient
	liveSpecWriter
}

func (f liveRESTWriter) UpdateServiceSpec(
	ctx context.Context,
	id string,
	change func(*swarm.ServiceSpec) error,
) (swarm.Service, error) {
	return f.liveSpecWriter.UpdateServiceSpec(ctx, id, change)
}

type liveMCPWriter struct {
	mcp.DockerWriteClient
	liveSpecWriter
}

func (f liveMCPWriter) UpdateServiceSpec(
	ctx context.Context,
	id string,
	change func(*swarm.ServiceSpec) error,
) (swarm.Service, error) {
	return f.liveSpecWriter.UpdateServiceSpec(ctx, id, change)
}

// Both transports judge a change to a service holding host access against
// the spec Docker holds, not the cache's copy, which has not caught up here.
func TestHostAccessHeldIsJudgedAgainstTheLiveSpecAlike(t *testing.T) {
	cached, ok := NewWorld(t).Cache.GetService(SeededServiceID)
	if !ok {
		t.Fatal("the seeded service is not cached")
	}
	live := cached.Spec
	container := *live.TaskTemplate.ContainerSpec
	container.Mounts = []mount.Mount{{
		Type: mount.TypeBind, Source: "/var/run/docker.sock", Target: "/var/run/docker.sock",
	}}
	live.TaskTemplate.ContainerSpec = &container

	var restWrote, mcpWrote bool
	w := NewWorldWithWritersAt(
		t,
		config.OpsConfiguration,
		liveRESTWriter{live: live, written: &restWrote},
		liveMCPWriter{live: live, written: &mcpWrote},
	)

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPatch, w.Server.URL+"/services/"+SeededServiceID+"/env",
		strings.NewReader(`{"MODE":"debug"}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	setPersonaHeaders(t, req, "ops")

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("PATCH env: %v", err)
	}
	defer resp.Body.Close()
	overREST, _ := io.ReadAll(resp.Body)

	result, rpcErr := w.MCP(t, "ops", "tools/call", map[string]any{
		"name": "update_service",
		"arguments": map[string]any{
			"id": SeededServiceID, "section": "env", "value": map[string]any{"MODE": "debug"},
		},
	})
	if rpcErr != nil {
		t.Fatalf("update_service: %s", rpcErr.Message)
	}

	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(overREST), "OPS001") {
		t.Errorf("REST = %d %s, want 403 OPS001", resp.StatusCode, overREST)
	}
	if !strings.Contains(string(result), "requires operations level 3") {
		t.Errorf("MCP = %s, want the host-access refusal", result)
	}
	if restWrote || mcpWrote {
		t.Errorf("written: REST %t, MCP %t; want neither", restWrote, mcpWrote)
	}
}

// A removal names a record the cache holds, or it is not found: neither
// transport hands Docker a name nothing has checked a grant against.
func TestRemovingAVolumeCetaceanDoesNotHoldIsNotFoundAlike(t *testing.T) {
	w := refusingWorld(t)

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodDelete, w.Server.URL+"/volumes/nowhere", nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	setPersonaHeaders(t, req, "ops")

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE volume: %v", err)
	}
	defer resp.Body.Close()
	overREST, _ := io.ReadAll(resp.Body)

	result, rpcErr := w.MCP(t, "ops", "tools/call", map[string]any{
		"name": "remove_volume", "arguments": map[string]any{"name": "nowhere"},
	})
	if rpcErr != nil {
		t.Fatalf("remove_volume: %s", rpcErr.Message)
	}

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("REST = %d %s, want 404", resp.StatusCode, overREST)
	}
	if !strings.Contains(string(result), "not found") {
		t.Errorf("MCP = %s, want not found", result)
	}
	for transport, answer := range map[string]string{
		"REST": string(overREST), "MCP": string(result),
	} {
		if strings.Contains(answer, engineAsked) {
			t.Errorf("%s asked Docker to remove a volume it does not hold: %s", transport, answer)
		}
	}
}
