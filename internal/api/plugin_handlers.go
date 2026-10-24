package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/docker/docker/api/types"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/docker"
)

func (h *Handlers) HandleListPlugins(w http.ResponseWriter, r *http.Request) {
	h.setAllowList(w, r, "plugin")
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

	plugins = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		plugins,
		func(p *types.Plugin) string {
			return "plugin:" + p.Name
		},
	)

	writeLinkTemplate(w, r, "/plugins/{name}")
	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(
				r.Context(),
				plugins,
				"Plugin",
				func(p *types.Plugin) string { return pluginPath(p.Name) },
			),
			len(plugins),
			len(plugins),
			0,
		),
	)
}

func (h *Handlers) HandleGetPlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "plugin:"+name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	plugin, err := h.pluginClient.PluginInspect(ctx, name)
	if err != nil {
		writeDockerError(w, r, err, "plugin", name)
		return
	}

	h.setAllow(w, r, "plugin", name)
	writeCachedJSON(w, r, pluginDetail(r, name, *plugin))
}

func (h *Handlers) HandleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("enabling plugin", "plugin", name)

	if err := h.pluginClient.PluginEnable(r.Context(), name); err != nil {
		writeDockerError(w, r, err, "plugin", name)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	slog.Info("disabling plugin", "plugin", name)

	if err := h.pluginClient.PluginDisable(r.Context(), name); err != nil {
		writeDockerError(w, r, err, "plugin", name)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemovePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	force := r.URL.Query().Get("force") == "true"

	// Every other remove reads the cache and answers 404, which is what the
	// precondition passed the request through to get. This one reaches the
	// daemon, where a plugin installed since would make it a 204 instead.
	if subjectAbsent(r.Context()) {
		writeErrorCode(w, r, "PLG004", fmt.Sprintf("plugin %q not found", name))
		return
	}

	slog.Info("removing plugin", "plugin", name, "force", force)

	if err := h.pluginClient.PluginRemove(r.Context(), name, force); err != nil {
		writeDockerError(w, r, err, "plugin", name)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type pluginRemoteRequest struct {
	Remote string `json:"remote"`

	// Privileges are the ones the caller reviewed and grants. Nil, not empty,
	// when absent: a plugin needing none is granted with an empty list.
	Privileges *types.PluginPrivileges `json:"privileges"`
}

// decodePluginRemote decodes a request naming a remote, refusing one without.
func decodePluginRemote(w http.ResponseWriter, r *http.Request) (pluginRemoteRequest, bool) {
	req, ok := decodeJSON[pluginRemoteRequest](w, r)
	if !ok {
		return req, false
	}
	if req.Remote == "" {
		writeErrorCode(w, r, "PLG002", "remote is required")
		return req, false
	}

	return req, true
}

// grantedPrivileges refuses an install or upgrade that names no privileges,
// and answers a plugin whose privileges changed since they were reviewed.
func grantedPrivileges(w http.ResponseWriter, r *http.Request, req pluginRemoteRequest) bool {
	if req.Privileges == nil {
		writeErrorCode(w, r, "PLG005",
			"privileges is required: send what POST /plugins/privileges returned for this remote")

		return false
	}

	return true
}

// writePluginWriteError is writeDockerError with the refusal for privileges
// that no longer match the review.
func writePluginWriteError(w http.ResponseWriter, r *http.Request, err error, name string) {
	if errors.Is(err, docker.ErrPluginPrivilegesChanged) {
		writeErrorCode(
			w,
			r,
			"PLG006",
			"the plugin's required privileges changed; review them again",
		)
		return
	}

	writeDockerError(w, r, err, "plugin", name)
}

type pluginConfigureRequest struct {
	Args []string `json:"args"`
	Env  []string `json:"env"`
}

func (h *Handlers) HandlePluginPrivileges(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePluginRemote(w, r)
	if !ok {
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

	writeJSON(
		w,
		NewDetailResponse(
			r.Context(),
			"/plugins/privileges",
			"PluginPrivileges",
			map[string]any{"privileges": privileges},
		),
	)
}

func (h *Handlers) HandleInstallPlugin(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePluginRemote(w, r)
	if !ok {
		return
	}

	if !grantedPrivileges(w, r, req) {
		return
	}

	slog.Info("installing plugin", "remote", req.Remote)

	plugin, err := h.pluginClient.PluginInstall(r.Context(), req.Remote, *req.Privileges)
	if err != nil {
		writePluginWriteError(w, r, err, req.Remote)
		return
	}

	w.Header().Set("Location", absPath(r.Context(), pluginPath(plugin.Name)))

	writeJSONStatus(
		w,
		http.StatusCreated,
		pluginDetail(r, plugin.Name, *plugin),
	)
}

func (h *Handlers) HandleUpgradePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	req, ok := decodePluginRemote(w, r)
	if !ok {
		return
	}

	if !grantedPrivileges(w, r, req) {
		return
	}

	slog.Info("upgrading plugin", "plugin", name, "remote", req.Remote)

	if err := h.pluginClient.PluginUpgrade(
		r.Context(),
		name,
		req.Remote,
		*req.Privileges,
	); err != nil {
		writePluginWriteError(w, r, err, name)
		return
	}

	h.writePluginAfterWrite(w, r, name)
}

func (h *Handlers) HandleConfigurePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	req, ok := decodeJSON[pluginConfigureRequest](w, r)
	if !ok {
		return
	}

	settings := make([]string, 0, len(req.Args)+len(req.Env))
	settings = append(settings, req.Args...)
	settings = append(settings, req.Env...)
	slog.Info("configuring plugin", "plugin", name, "settings", settings)

	if err := h.pluginClient.PluginConfigure(r.Context(), name, settings); err != nil {
		writeDockerError(w, r, err, "plugin", name)
		return
	}

	h.writePluginAfterWrite(w, r, name)
}

// writePluginAfterWrite answers a plugin write with the plugin as it now is,
// unless the client asked for a minimal response.
func (h *Handlers) writePluginAfterWrite(w http.ResponseWriter, r *http.Request, name string) {
	if preferMinimal(r) {
		writePreferMinimal(w)
		return
	}

	plugin, err := h.pluginClient.PluginInspect(r.Context(), name)
	if err != nil {
		writeDockerError(w, r, err, "plugin", name)
		return
	}

	writeJSON(w, pluginDetail(r, name, *plugin))
}
