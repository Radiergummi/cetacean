//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// startNone brings up the shared environment with a no-auth binary on the
// `none` lane's reserved port. opsLevel sets CETACEAN_OPERATIONS_LEVEL; pass
// "" to leave it unset and take the binary's default (operational, tier 1).
func startNone(t *testing.T, opsLevel string) (*harness.Env, *sut.Process) {
	t.Helper()

	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	envVars := map[string]string{"CETACEAN_AUTH_MODE": "none"}
	if opsLevel != "" {
		envVars["CETACEAN_OPERATIONS_LEVEL"] = opsLevel
	}

	proc := sut.Start(t, sut.Config{
		Port:       19001,
		DockerHost: env.DockerHost,
		Env:        envVars,
	})

	return env, proc
}

// serviceID resolves a service's ID from /services by name. GET /services/{id}
// only accepts the Docker service ID (cache.GetService is a direct map
// lookup, not a name resolver), so tests addressing a service by its fixture
// name must look the ID up first.
func serviceID(t *testing.T, proc *sut.Process, name string) string {
	t.Helper()

	var body struct {
		Items []struct {
			ID   string `json:"ID"`
			Spec struct {
				Name string `json:"Name"`
			} `json:"Spec"`
		} `json:"items"`
	}

	resp := getJSON(t, proc, "/services", &body) //nolint:bodyclose // closed in getJSON
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /services: status = %d, want 200", resp.StatusCode)
	}

	for _, item := range body.Items {
		if item.Spec.Name == name {
			return item.ID
		}
	}

	t.Fatalf("service %q not found in /services", name)

	return ""
}

func getJSON(t *testing.T, proc *sut.Process, path string, into any) *http.Response {
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

	if into != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}

	return resp
}

func TestServicesListReportsTheBaseline(t *testing.T) {
	_, proc := startNone(t, "")

	var body struct {
		Total int `json:"total"`
		Items []struct {
			Spec struct {
				Name   string            `json:"Name"`
				Labels map[string]string `json:"Labels"`
			} `json:"Spec"`
		} `json:"items"`
	}

	resp := getJSON(t, proc, "/services", &body) //nolint:bodyclose // closed in getJSON

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// shop_web, shop_lonely, shop_flaky, platform_agent. shop_flaky never
	// converges, but it is still a listed service, so it counts here too.
	if body.Total < 4 {
		t.Errorf("total = %d, want at least 4", body.Total)
	}

	found := false
	for _, item := range body.Items {
		if item.Spec.Name == "shop_web" {
			found = true
		}
	}

	if !found {
		t.Errorf("shop_web missing from /services")
	}
}

func TestETagRoundTripReturns304(t *testing.T) {
	_, proc := startNone(t, "")

	first := getJSON(t, proc, "/services", nil) //nolint:bodyclose // closed in getJSON

	etag := first.Header.Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on /services")
	}

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		proc.BaseURL+"/services",
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("If-None-Match", etag)

	second, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("conditional GET: %v", err)
	}
	defer second.Body.Close()

	if second.StatusCode != http.StatusNotModified {
		t.Errorf("status = %d, want 304", second.StatusCode)
	}
}

// TestTopologyRenderingsAgree checks the three renderings of /topology against
// each other -- the case unit tests cannot express. The JSON response holds
// two JGF graphs, "network" and "placement", but DOT and GraphML render only
// the network graph, so only that graph's node URNs are compared.
func TestTopologyRenderingsAgree(t *testing.T) {
	_, proc := startNone(t, "")

	var doc struct {
		Graphs []struct {
			ID    string                     `json:"id"`
			Nodes map[string]json.RawMessage `json:"nodes"`
		} `json:"graphs"`
	}

	getJSON(t, proc, "/topology", &doc) //nolint:bodyclose // closed in getJSON

	var networkNodes map[string]json.RawMessage
	for _, g := range doc.Graphs {
		if g.ID == "network" {
			networkNodes = g.Nodes
		}
	}

	if len(networkNodes) == 0 {
		t.Fatal("JGF topology has no nodes in the network graph")
	}

	for _, accept := range []string{"text/vnd.graphviz", "application/graphml+xml"} {
		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			proc.BaseURL+"/topology",
			nil,
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		req.Header.Set("Accept", accept)

		resp, err := proc.Client().Do(req)
		if err != nil {
			t.Fatalf("GET /topology as %s: %v", accept, err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", accept, resp.StatusCode)

			continue
		}

		// Every network-graph node id must appear in the alternate rendering.
		for id := range networkNodes {
			if !bytes.Contains(body, []byte(id)) {
				t.Errorf("%s rendering omits node %q", accept, id)
			}
		}
	}
}

// Checks the Allow header on a service *detail* endpoint: setAllowList adds POST
// only for config/secret/plugin, so /services answers "GET, HEAD" at every level
// and cannot tell tier gating from a broken one. Both halves are asserted, since
// tier 0 having no PUT/POST is unfalsifiable on its own.
func TestAllowHeaderReflectsOperationsLevel(t *testing.T) {
	t.Run("ops level 0 denies writes", func(t *testing.T) {
		_, proc := startNone(t, "0")
		id := serviceID(t, proc, "shop_web")

		resp := getJSON(t, proc, "/services/"+id, nil) //nolint:bodyclose // closed in getJSON
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /services/%s: status = %d, want 200", id, resp.StatusCode)
		}

		allow := resp.Header.Get("Allow")
		if allow == "" {
			t.Fatal("no Allow header on /services/{id}")
		}

		if strings.Contains(allow, "PUT") || strings.Contains(allow, "POST") {
			t.Errorf("Allow = %q at ops level 0; want no PUT/POST", allow)
		}
	})

	t.Run("ops level 1 allows writes", func(t *testing.T) {
		_, proc := startNone(t, "1")
		id := serviceID(t, proc, "shop_web")

		resp := getJSON(t, proc, "/services/"+id, nil) //nolint:bodyclose // closed in getJSON
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /services/%s: status = %d, want 200", id, resp.StatusCode)
		}

		allow := resp.Header.Get("Allow")
		if !strings.Contains(allow, "PUT") || !strings.Contains(allow, "POST") {
			t.Errorf("Allow = %q at ops level 1; want PUT and POST present", allow)
		}
	})
}

// TestMetricsReport503WhenPrometheusIsUnconfigured is the spec's one
// phase-one metrics case: with no Prometheus configured, the nil-receiver
// paths must say so (MTR001, 503) rather than serve an empty chart.
func TestMetricsReport503WhenPrometheusIsUnconfigured(t *testing.T) {
	_, proc := startNone(t, "")

	resp := getJSON(t, proc, "/metrics", nil) //nolint:bodyclose // closed in getJSON

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 with no CETACEAN_PROMETHEUS_URL", resp.StatusCode)
	}
}
