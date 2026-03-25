package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"
	json "github.com/goccy/go-json"
)

func (h *Handlers) HandlePlugins(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	plugins, err := h.pluginClient.PluginList(ctx)
	if err != nil {
		slog.Error("plugin list failed", "error", err)
		writeErrorCode(w, r, "PLG001", "plugin list failed")
		return
	}
	if plugins == nil {
		plugins = types.PluginsListResponse{}
	}

	writeJSONWithETag(w, r, NewCollectionResponse(plugins, len(plugins), len(plugins), 0))
}

func (h *Handlers) HandlePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	plugin, err := h.pluginClient.PluginInspect(ctx, name)
	if err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	writeJSONWithETag(w, r, NewDetailResponse("/plugins/"+name, "Plugin", map[string]any{
		"plugin": plugin,
	}))
}

func (h *Handlers) HandleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("enabling plugin", "plugin", name)

	if err := h.pluginClient.PluginEnable(r.Context(), name); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("disabling plugin", "plugin", name)

	if err := h.pluginClient.PluginDisable(r.Context(), name); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	force := r.URL.Query().Get("force") == "true"
	slog.Info("removing plugin", "plugin", name, "force", force)

	if err := h.pluginClient.PluginRemove(r.Context(), name, force); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type pluginRemoteRequest struct {
	Remote string `json:"remote"`
}

type pluginConfigureRequest struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
}

func (h *Handlers) HandlePluginPrivileges(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}
	if req.Remote == "" {
		writeErrorCode(w, r, "PLG002", "remote is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	privileges, err := h.pluginClient.PluginPrivileges(ctx, req.Remote)
	if err != nil {
		slog.Error("plugin privileges check failed", "remote", req.Remote, "error", err)
		writeErrorCode(w, r, "PLG003", "failed to check plugin privileges")
		return
	}

	writeJSON(w, privileges)
}

func (h *Handlers) HandleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}
	if req.Remote == "" {
		writeErrorCode(w, r, "PLG002", "remote is required")
		return
	}

	slog.Info("installing plugin", "remote", req.Remote)

	plugin, err := h.pluginClient.PluginInstall(r.Context(), req.Remote)
	if err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	writeJSONStatus(
		w,
		http.StatusCreated,
		NewDetailResponse("/plugins/"+plugin.Name, "Plugin", map[string]any{
			"plugin": plugin,
		}),
	)
}

func (h *Handlers) HandleUpgradePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}
	if req.Remote == "" {
		writeErrorCode(w, r, "PLG002", "remote is required")
		return
	}

	slog.Info("upgrading plugin", "plugin", name, "remote", req.Remote)

	if err := h.pluginClient.PluginUpgrade(r.Context(), name, req.Remote); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	plugin, err := h.pluginClient.PluginInspect(r.Context(), name)
	if err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	writeJSON(w, NewDetailResponse("/plugins/"+name, "Plugin", map[string]any{
		"plugin": plugin,
	}))
}

func (h *Handlers) HandleConfigurePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginConfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	settings := make([]string, 0, len(req.Args)+len(req.Env))
	settings = append(settings, req.Args...)
	settings = append(settings, req.Env...)
	slog.Info("configuring plugin", "plugin", name, "settings", settings)

	if err := h.pluginClient.PluginConfigure(r.Context(), name, settings); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	plugin, err := h.pluginClient.PluginInspect(r.Context(), name)
	if err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	writeJSON(w, NewDetailResponse("/plugins/"+name, "Plugin", map[string]any{
		"plugin": plugin,
	}))
}
