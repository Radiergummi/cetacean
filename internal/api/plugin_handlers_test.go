package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/errdefs"
	json "github.com/goccy/go-json"
)

type mockPluginClient struct {
	pluginListFn       func(ctx context.Context) (types.PluginsListResponse, error)
	pluginInspectFn    func(ctx context.Context, name string) (*types.Plugin, error)
	pluginEnableFn     func(ctx context.Context, name string) error
	pluginDisableFn    func(ctx context.Context, name string) error
	pluginRemoveFn     func(ctx context.Context, name string, force bool) error
	pluginInstallFn    func(ctx context.Context, remote string) (*types.Plugin, error)
	pluginUpgradeFn    func(ctx context.Context, name string, remote string) error
	pluginPrivilegesFn func(ctx context.Context, remote string) (types.PluginPrivileges, error)
	pluginConfigureFn  func(ctx context.Context, name string, args []string) error
}

var _ DockerPluginClient = (*mockPluginClient)(nil)

func (m *mockPluginClient) PluginList(ctx context.Context) (types.PluginsListResponse, error) {
	if m.pluginListFn != nil {
		return m.pluginListFn(ctx)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginInspect(ctx context.Context, name string) (*types.Plugin, error) {
	if m.pluginInspectFn != nil {
		return m.pluginInspectFn(ctx, name)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginEnable(ctx context.Context, name string) error {
	if m.pluginEnableFn != nil {
		return m.pluginEnableFn(ctx, name)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginDisable(ctx context.Context, name string) error {
	if m.pluginDisableFn != nil {
		return m.pluginDisableFn(ctx, name)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginRemove(ctx context.Context, name string, force bool) error {
	if m.pluginRemoveFn != nil {
		return m.pluginRemoveFn(ctx, name, force)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginInstall(
	ctx context.Context,
	remote string,
) (*types.Plugin, error) {
	if m.pluginInstallFn != nil {
		return m.pluginInstallFn(ctx, remote)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginUpgrade(ctx context.Context, name string, remote string) error {
	if m.pluginUpgradeFn != nil {
		return m.pluginUpgradeFn(ctx, name, remote)
	}
	return fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginPrivileges(
	ctx context.Context,
	remote string,
) (types.PluginPrivileges, error) {
	if m.pluginPrivilegesFn != nil {
		return m.pluginPrivilegesFn(ctx, remote)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginConfigure(ctx context.Context, name string, args []string) error {
	if m.pluginConfigureFn != nil {
		return m.pluginConfigureFn(ctx, name, args)
	}
	return fmt.Errorf("not implemented")
}

func newPluginHandlers(t testing.TB, pc *mockPluginClient) *Handlers {
	t.Helper()
	return newTestHandlers(t, withPluginClient(pc))
}

func TestHandleListPlugins_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginListFn: func(_ context.Context) (types.PluginsListResponse, error) {
			return types.PluginsListResponse{
				{Name: "plugin-a", Enabled: true},
				{Name: "plugin-b", Enabled: false},
			}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest(http.MethodGet, "/plugins", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	h.HandleListPlugins(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	total, _ := resp["total"].(float64)
	if total != 2 {
		t.Errorf("expected total 2, got %v", resp["total"])
	}
}

func TestHandleListPlugins_Empty(t *testing.T) {
	pc := &mockPluginClient{
		pluginListFn: func(_ context.Context) (types.PluginsListResponse, error) {
			return nil, nil
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest(http.MethodGet, "/plugins", nil)
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	h.HandleListPlugins(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetPlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			return &types.Plugin{Name: name, Enabled: true}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest("GET", "/plugins/my-plugin", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleGetPlugin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Plugin" {
		t.Errorf("@type=%v, want Plugin", resp["@type"])
	}
}

func TestHandleGetPlugin_NotFound(t *testing.T) {
	pc := &mockPluginClient{
		pluginInspectFn: func(_ context.Context, _ string) (*types.Plugin, error) {
			return nil, errdefs.NotFound(fmt.Errorf("plugin not found"))
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest("GET", "/plugins/missing", nil)
	req.SetPathValue("name", "missing")
	w := httptest.NewRecorder()
	h.HandleGetPlugin(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", w.Code)
	}
}

func TestHandleEnablePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginEnableFn: func(_ context.Context, _ string) error {
			return nil
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest("POST", "/plugins/my-plugin/enable", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleEnablePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleDisablePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginDisableFn: func(_ context.Context, _ string) error {
			return nil
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest("POST", "/plugins/my-plugin/disable", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleDisablePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRemovePlugin_OK(t *testing.T) {
	var removedForce bool
	pc := &mockPluginClient{
		pluginRemoveFn: func(_ context.Context, _ string, force bool) error {
			removedForce = force
			return nil
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest("DELETE", "/plugins/my-plugin", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleRemovePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
	if removedForce {
		t.Error("expected force=false")
	}
}

func TestHandleRemovePlugin_Force(t *testing.T) {
	var removedForce bool
	pc := &mockPluginClient{
		pluginRemoveFn: func(_ context.Context, _ string, force bool) error {
			removedForce = force
			return nil
		},
	}
	h := newPluginHandlers(t, pc)

	req := httptest.NewRequest("DELETE", "/plugins/my-plugin?force=true", nil)
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleRemovePlugin(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204; body: %s", w.Code, w.Body.String())
	}
	if !removedForce {
		t.Error("expected force=true")
	}
}

func TestHandlePluginPrivileges_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginPrivilegesFn: func(_ context.Context, _ string) (types.PluginPrivileges, error) {
			return types.PluginPrivileges{{Name: "network", Value: []string{"host"}}}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	body := `{"remote":"example/plugin:latest"}`
	req := httptest.NewRequest("POST", "/plugins/privileges", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandlePluginPrivileges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "PluginPrivileges" {
		t.Errorf("@type=%v, want PluginPrivileges", resp["@type"])
	}
	if id, ok := resp["@id"].(string); !ok || !strings.HasSuffix(id, "/plugins/privileges") {
		t.Errorf("expected @id ending in /plugins/privileges, got %v", resp["@id"])
	}
	if ctx, ok := resp["@context"].(string); !ok || !strings.HasSuffix(ctx, "/api/context.jsonld") {
		t.Errorf("expected @context ending in /api/context.jsonld, got %v", resp["@context"])
	}
}

func TestHandlePluginPrivileges_MissingRemote(t *testing.T) {
	pc := &mockPluginClient{}
	h := newPluginHandlers(t, pc)

	body := `{"remote":""}`
	req := httptest.NewRequest("POST", "/plugins/privileges", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandlePluginPrivileges(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", w.Code)
	}
}

func TestHandleInstallPlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginInstallFn: func(_ context.Context, remote string) (*types.Plugin, error) {
			return &types.Plugin{Name: "example/plugin:latest", Enabled: false}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	body := `{"remote":"example/plugin:latest"}`
	req := httptest.NewRequest("POST", "/plugins", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleInstallPlugin(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Plugin" {
		t.Errorf("@type=%v, want Plugin", resp["@type"])
	}
}

func TestHandleUpgradePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginUpgradeFn: func(_ context.Context, _ string, _ string) error {
			return nil
		},
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			return &types.Plugin{Name: name, Enabled: true}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	body := `{"remote":"example/plugin:v2"}`
	req := httptest.NewRequest("POST", "/plugins/my-plugin/upgrade", strings.NewReader(body))
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleUpgradePlugin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Plugin" {
		t.Errorf("@type=%v, want Plugin", resp["@type"])
	}
}

func TestHandleConfigurePlugin_OK(t *testing.T) {
	pc := &mockPluginClient{
		pluginConfigureFn: func(_ context.Context, _ string, _ []string) error {
			return nil
		},
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			return &types.Plugin{Name: name, Enabled: true}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	body := `{"args":["DEBUG=1","LOG_LEVEL=info"]}`
	req := httptest.NewRequest("PATCH", "/plugins/my-plugin/settings", strings.NewReader(body))
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleConfigurePlugin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Plugin" {
		t.Errorf("@type=%v, want Plugin", resp["@type"])
	}
}
