package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/errdefs"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/docker"
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
	_ types.PluginPrivileges,
) (*types.Plugin, error) {
	if m.pluginInstallFn != nil {
		return m.pluginInstallFn(ctx, remote)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockPluginClient) PluginUpgrade(
	ctx context.Context,
	name, remote string,
	_ types.PluginPrivileges,
) error {
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
	var inspected string
	pc := &mockPluginClient{
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			inspected = name
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
	if resp["@id"] != "/plugins/my-plugin" {
		t.Errorf("@id=%v, want /plugins/my-plugin", resp["@id"])
	}
	if inspected != "my-plugin" {
		t.Errorf("inspected %q, want my-plugin", inspected)
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
	var target string
	pc := &mockPluginClient{
		pluginEnableFn: func(_ context.Context, name string) error {
			target = name
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
	if target != "my-plugin" {
		t.Errorf("enabled %q, want my-plugin", target)
	}
}

func TestHandleDisablePlugin_OK(t *testing.T) {
	var target string
	pc := &mockPluginClient{
		pluginDisableFn: func(_ context.Context, name string) error {
			target = name
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
	if target != "my-plugin" {
		t.Errorf("disabled %q, want my-plugin", target)
	}
}

func TestHandleRemovePlugin_OK(t *testing.T) {
	var (
		removed      string
		removedForce bool
	)
	pc := &mockPluginClient{
		pluginRemoveFn: func(_ context.Context, name string, force bool) error {
			removed, removedForce = name, force
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
	if removed != "my-plugin" {
		t.Errorf("removed %q, want my-plugin", removed)
	}
}

func TestHandleRemovePlugin_Force(t *testing.T) {
	var (
		removed      string
		removedForce bool
	)
	pc := &mockPluginClient{
		pluginRemoveFn: func(_ context.Context, name string, force bool) error {
			removed, removedForce = name, force
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
	if removed != "my-plugin" {
		t.Errorf("removed %q, want my-plugin", removed)
	}
}

func TestHandlePluginPrivileges_OK(t *testing.T) {
	var asked string
	pc := &mockPluginClient{
		pluginPrivilegesFn: func(_ context.Context, remote string) (types.PluginPrivileges, error) {
			asked = remote
			return types.PluginPrivileges{{Name: "network", Value: []string{"host"}}}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	body := `{"remote":"example/plugin:latest","privileges":[]}`
	req := httptest.NewRequest("POST", "/plugins/privileges", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandlePluginPrivileges(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	if asked != "example/plugin:latest" {
		t.Errorf("asked for the privileges of %q, want example/plugin:latest", asked)
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
	var installed string
	pc := &mockPluginClient{
		pluginInstallFn: func(_ context.Context, remote string) (*types.Plugin, error) {
			installed = remote
			return &types.Plugin{Name: "example/plugin:latest", Enabled: false}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	body := `{"remote":"example/plugin:latest","privileges":[]}`
	req := httptest.NewRequest("POST", "/plugins", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.HandleInstallPlugin(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body: %s", w.Code, w.Body.String())
	}
	if installed != "example/plugin:latest" {
		t.Errorf("installed %q, want example/plugin:latest", installed)
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
	var upgraded, upgradedTo string
	pc := &mockPluginClient{
		pluginUpgradeFn: func(_ context.Context, name, remote string) error {
			upgraded, upgradedTo = name, remote
			return nil
		},
		pluginInspectFn: func(_ context.Context, name string) (*types.Plugin, error) {
			return &types.Plugin{Name: name, Enabled: true}, nil
		},
	}
	h := newPluginHandlers(t, pc)

	body := `{"remote":"example/plugin:v2","privileges":[]}`
	req := httptest.NewRequest("POST", "/plugins/my-plugin/upgrade", strings.NewReader(body))
	req.SetPathValue("name", "my-plugin")
	w := httptest.NewRecorder()
	h.HandleUpgradePlugin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body: %s", w.Code, w.Body.String())
	}
	if upgraded != "my-plugin" || upgradedTo != "example/plugin:v2" {
		t.Errorf("upgraded %q to %q, want my-plugin to example/plugin:v2", upgraded, upgradedTo)
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
	var (
		configured string
		args       []string
	)
	pc := &mockPluginClient{
		pluginConfigureFn: func(_ context.Context, name string, a []string) error {
			configured, args = name, a
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
	if configured != "my-plugin" || !slices.Equal(args, []string{"DEBUG=1", "LOG_LEVEL=info"}) {
		t.Errorf("configured %q with %v, want my-plugin with the request's args", configured, args)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["@type"] != "Plugin" {
		t.Errorf("@type=%v, want Plugin", resp["@type"])
	}
}

// An install that names no privileges grants nothing it has not reviewed, so
// it is refused rather than accepting whatever the registry now asks for.
func TestHandleInstallPlugin_RequiresThePrivilegesItGrants(t *testing.T) {
	installed := false
	h := newTestHandlers(t, withPluginClient(&mockPluginClient{
		pluginInstallFn: func(context.Context, string) (*types.Plugin, error) {
			installed = true
			return &types.Plugin{Name: "p"}, nil
		},
	}))

	req := httptest.NewRequest(
		"POST",
		"/plugins",
		strings.NewReader(`{"remote":"example/plugin:latest"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleInstallPlugin(w, req)

	if w.Code != http.StatusBadRequest || installed {
		t.Errorf("status=%d installed=%v, want 400 and no install", w.Code, installed)
	}
}

// A plugin whose privileges changed after the review is refused with 409, so
// the caller knows to look at them again rather than retry.
func TestHandleInstallPlugin_ChangedPrivilegesAreAConflict(t *testing.T) {
	h := newTestHandlers(t, withPluginClient(&mockPluginClient{
		pluginInstallFn: func(context.Context, string) (*types.Plugin, error) {
			return nil, docker.ErrPluginPrivilegesChanged
		},
	}))

	req := httptest.NewRequest("POST", "/plugins",
		strings.NewReader(`{"remote":"example/plugin:latest","privileges":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.HandleInstallPlugin(w, req)

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "PLG006") {
		t.Errorf("status=%d body=%s, want 409 PLG006", w.Code, w.Body.String())
	}
}

// A plugin name carries a slash, so every URI naming one escapes it as a
// single segment; followed back through the router, each reaches the plugin.
func TestEveryPluginURIResolvesToThePlugin(t *testing.T) {
	const name = "vieux/sshfs:latest"
	plugin := &types.Plugin{Name: name}

	router := newTestRouterWithConfig(t, nil, withPluginClient(&mockPluginClient{
		pluginListFn: func(context.Context) (types.PluginsListResponse, error) {
			return types.PluginsListResponse{plugin}, nil
		},
		pluginInspectFn: func(_ context.Context, got string) (*types.Plugin, error) {
			if got != name {
				return nil, errdefs.NotFound(fmt.Errorf("plugin %q not found", got))
			}
			return plugin, nil
		},
		pluginInstallFn: func(context.Context, string) (*types.Plugin, error) { return plugin, nil },
	}))

	serve := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		return w
	}

	var list struct {
		Items []struct {
			ID string `json:"@id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(
		serve(http.MethodGet, "/plugins", "").Body.Bytes(),
		&list,
	); err != nil ||
		len(list.Items) != 1 {
		t.Fatalf("list did not decode to one item: %v", err)
	}

	var detail struct {
		ID string `json:"@id"`
	}
	if err := json.Unmarshal(
		serve(http.MethodGet, list.Items[0].ID, "").Body.Bytes(),
		&detail,
	); err != nil {
		t.Fatalf("detail did not decode: %v", err)
	}

	installed := serve(
		http.MethodPost,
		"/plugins",
		`{"remote":"vieux/sshfs:latest","privileges":[]}`,
	)

	for label, uri := range map[string]string{
		"list item @id": list.Items[0].ID,
		"detail @id":    detail.ID,
		"Location":      installed.Header().Get("Location"),
	} {
		if uri == "" {
			t.Errorf("no %s", label)
			continue
		}
		if w := serve(http.MethodGet, uri, ""); w.Code != http.StatusOK {
			t.Errorf("%s %q answered %d", label, uri, w.Code)
		}
	}
}
