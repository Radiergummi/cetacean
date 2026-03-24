package api

import (
	"context"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"
	"log/slog"
)

func (h *Handlers) HandlePlugins(w http.ResponseWriter, r *http.Request) {
	if h.pluginClient == nil {
		writeProblem(w, r, http.StatusNotImplemented, "plugin list not available")
		return
	}

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
