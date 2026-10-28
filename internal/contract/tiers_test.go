package contract

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// exposure is how each transport carries out one shared write: the MCP tool and
// the REST routes, the latter with their path values filled from the fixture.
type exposure struct {
	tool   string
	routes []string
}

var operationExposures = map[cluster.Operation]exposure{
	cluster.OpScaleService:       {"scale_service", []string{"PUT /services/{id}/scale"}},
	cluster.OpUpdateServiceImage: {"update_service_image", []string{"PUT /services/{id}/image"}},
	cluster.OpRollbackService:    {"rollback_service", []string{"POST /services/{id}/rollback"}},
	cluster.OpRestartService:     {"restart_service", []string{"POST /services/{id}/restart"}},
	cluster.OpUpdateService: {"update_service", []string{
		"PATCH /services/{id}/env",
		"PATCH /services/{id}/labels",
		"PATCH /services/{id}/resources",
		"PUT /services/{id}/placement",
		"PATCH /services/{id}/ports",
		"PATCH /services/{id}/update-policy",
		"PATCH /services/{id}/rollback-policy",
		"PATCH /services/{id}/log-driver",
		"PUT /services/{id}/healthcheck",
		"PATCH /services/{id}/healthcheck",
		"PATCH /services/{id}/container-config",
	}},
	cluster.OpUpdateServiceSecrets: {
		"update_service_secrets",
		[]string{"PATCH /services/{id}/secrets"},
	},
	cluster.OpUpdateServiceConfigs: {
		"update_service_configs",
		[]string{"PATCH /services/{id}/configs"},
	},
	cluster.OpUpdateServiceMounts: {
		"update_service_mounts",
		[]string{"PATCH /services/{id}/mounts"},
	},
	cluster.OpRemoveService:    {"remove_service", []string{"DELETE /services/{id}"}},
	cluster.OpUpdateNodeLabels: {"update_node_labels", []string{"PATCH /nodes/{id}/labels"}},
	cluster.OpUpdateNode: {"update_node", []string{
		"PUT /nodes/{id}/availability",
		"PUT /nodes/{id}/role",
	}},
	cluster.OpRemoveTask:    {"remove_task", []string{"DELETE /tasks/{id}"}},
	cluster.OpCreateConfig:  {"create_config", []string{"POST /configs"}},
	cluster.OpRemoveConfig:  {"remove_config", []string{"DELETE /configs/{id}"}},
	cluster.OpCreateSecret:  {"create_secret", []string{"POST /secrets"}},
	cluster.OpRemoveSecret:  {"remove_secret", []string{"DELETE /secrets/{id}"}},
	cluster.OpRemoveNetwork: {"remove_network", []string{"DELETE /networks/{id}"}},
	cluster.OpRemoveVolume:  {"remove_volume", []string{"DELETE /volumes/{name}"}},
}

// Every shared write is refused below its tier and admitted at it, by both
// transports alike: the REST route answers OPS001 exactly when tools/list
// leaves the tool out.
func TestWriteTiersAgreeAcrossTransports(t *testing.T) {
	for _, op := range cluster.Operations() {
		if _, ok := operationExposures[op]; !ok {
			t.Errorf("%s has no operationExposures row", op)
		}
	}

	for _, level := range []config.OperationsLevel{
		config.OpsReadOnly, config.OpsOperational, config.OpsConfiguration, config.OpsImpactful,
	} {
		w := NewWorldAt(t, level)
		listed := listedTools(t, w)

		for op, exposed := range operationExposures {
			admitted := level >= op.Tier()

			if slices.Contains(listed, exposed.tool) != admitted {
				t.Errorf("level %d: %s listed = %t, want %t",
					level, exposed.tool, !admitted, admitted)
			}

			for _, route := range exposed.routes {
				if refused := refusedForTier(t, w, route); refused == admitted {
					t.Errorf("level %d: %s refused for its tier = %t, want %t",
						level, route, refused, !admitted)
				}
			}
		}
	}
}

func listedTools(t *testing.T, w *World) []string {
	t.Helper()

	result, rpcErr := w.MCP(t, "ops", "tools/list", nil)
	if rpcErr != nil {
		t.Fatalf("tools/list: %s", rpcErr.Message)
	}

	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &catalog); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}

	names := make([]string, 0, len(catalog.Tools))
	for _, tool := range catalog.Tools {
		names = append(names, tool.Name)
	}

	return names
}

// refusedForTier reports whether route answers OPS001. A mismatched If-Match
// and a malformed body stop an admitted request before it reaches the absent
// Docker client.
func refusedForTier(t *testing.T, w *World, route string) bool {
	t.Helper()

	method, path, _ := strings.Cut(route, " ")
	path = strings.NewReplacer(
		"/services/{id}", "/services/"+SeededServiceID,
		"/nodes/{id}", "/nodes/"+SeededNodeID,
		"/tasks/{id}", "/tasks/"+SeededTaskID,
		"/configs/{id}", "/configs/"+SeededConfigID,
		"/secrets/{id}", "/secrets/"+SeededSecretID,
		"/networks/{id}", "/networks/"+SeededNetworkID,
		"/volumes/{name}", "/volumes/"+SeededVolumeName,
	).Replace(path)

	req, err := http.NewRequestWithContext(
		t.Context(), method, w.Server.URL+path, strings.NewReader("{"),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"mismatch"`)
	setPersonaHeaders(t, req, "ops")

	resp, err := w.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s: %v", route, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusInternalServerError {
		t.Fatalf("%s = %d; the probe reached a handler it should not have", route,
			resp.StatusCode)
	}

	var problem struct {
		Type string `json:"type"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&problem)

	return strings.HasSuffix(problem.Type, "/OPS001")
}
