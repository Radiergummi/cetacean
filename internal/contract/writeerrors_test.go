package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api"
	"github.com/radiergummi/cetacean/internal/mcp"
)

type refusingRESTWriter struct {
	api.DockerWriteClient
	err error
}

func (f refusingRESTWriter) ScaleService(context.Context, string, uint64) (swarm.Service, error) {
	return swarm.Service{}, f.err
}

type refusingMCPWriter struct {
	mcp.DockerWriteClient
	err error
}

func (f refusingMCPWriter) ScaleService(context.Context, string, uint64) (swarm.Service, error) {
	return swarm.Service{}, f.err
}

// Both transports show the engine's text for the same classes of refusal and
// withhold it for the rest, as cluster.ClassifyWrite decides for both.
func TestEngineTextIsWithheldAlikeAcrossTransports(t *testing.T) {
	const internals = "/var/run/docker.sock on node-7"

	for _, tc := range []struct {
		name  string
		class error
		shown bool
	}{
		{"internal", cerrdefs.ErrInternal, false},
		{"not found", cerrdefs.ErrNotFound, false},
		{"failed precondition", cerrdefs.ErrFailedPrecondition, false},
		{"invalid", cerrdefs.ErrInvalidArgument, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("%w: %s", tc.class, internals)
			w := NewWorldWithWriters(t, refusingRESTWriter{err: err}, refusingMCPWriter{err: err})

			overREST := scaleRefusalOverREST(t, w)
			overMCP := scaleRefusalOverMCP(t, w)

			if got := strings.Contains(overREST, internals); got != tc.shown {
				t.Errorf("REST shows the engine's text = %t, want %t: %s", got, tc.shown, overREST)
			}
			if got := strings.Contains(overMCP, internals); got != tc.shown {
				t.Errorf("MCP shows the engine's text = %t, want %t: %s", got, tc.shown, overMCP)
			}
		})
	}
}

func scaleRefusalOverREST(t *testing.T, w *World) string {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPut, w.Server.URL+"/services/"+SeededServiceID+"/scale",
		strings.NewReader(`{"replicas":3}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	setPersonaHeaders(t, req, "ops")

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT scale: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 400 {
		t.Fatalf("PUT scale = %d, want a refusal: %s", resp.StatusCode, body)
	}

	return string(body)
}

func scaleRefusalOverMCP(t *testing.T, w *World) string {
	t.Helper()

	result, rpcErr := w.MCP(t, "ops", "tools/call", map[string]any{
		"name":      "scale_service",
		"arguments": map[string]any{"id": SeededServiceID, "replicas": 3},
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
