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
		writeProblem(w, r, http.StatusInternalServerError, "plugin list failed")
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
}

func (h *Handlers) HandlePluginPrivileges(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Remote == "" {
		writeProblem(w, r, http.StatusBadRequest, "remote is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	privileges, err := h.pluginClient.PluginPrivileges(ctx, req.Remote)
	if err != nil {
		slog.Error("plugin privileges check failed", "remote", req.Remote, "error", err)
		writeProblem(w, r, http.StatusInternalServerError, "failed to check plugin privileges")
		return
	}

	writeJSON(w, privileges)
}

func (h *Handlers) HandleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Remote == "" {
		writeProblem(w, r, http.StatusBadRequest, "remote is required")
		return
	}

	slog.Info("installing plugin", "remote", req.Remote)

	plugin, err := h.pluginClient.PluginInstall(r.Context(), req.Remote)
	if err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	writeJSON(w, NewDetailResponse("/plugins/"+plugin.Name, "Plugin", map[string]any{
		"plugin": plugin,
	}))
}

func (h *Handlers) HandleUpgradePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Remote == "" {
		writeProblem(w, r, http.StatusBadRequest, "remote is required")
		return
	}

	slog.Info("upgrading plugin", "plugin", name, "remote", req.Remote)

	if err := h.pluginClient.PluginUpgrade(r.Context(), name, req.Remote); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleConfigurePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req pluginConfigureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	slog.Info("configuring plugin", "plugin", name, "args", req.Args)

	if err := h.pluginClient.PluginConfigure(r.Context(), name, req.Args); err != nil {
		writeDockerError(w, r, err, "plugin")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
