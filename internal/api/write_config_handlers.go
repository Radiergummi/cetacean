package api

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func (h *Handlers) HandleRemoveConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", fmt.Sprintf("config %q not found", id))
		return
	}

	slog.Info("removing config", "config", id)

	err := h.configWriter.RemoveConfig(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "CFG001", err.Error())
			return
		}
		writeDockerError(w, r, err, "config", id)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleCreateConfig(w http.ResponseWriter, r *http.Request) {
	var req createResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeErrorCode(w, r, "CFG004", "name is required")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeErrorCode(w, r, "CFG004", "data must be valid base64")
		return
	}

	slog.Info("creating config", "name", req.Name)

	id, err := h.configWriter.CreateConfig(r.Context(), swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: req.Name},
		Data:        data,
	})
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, "CFG003", err.Error())
			return
		}
		writeDockerError(w, r, err, "config", req.Name)
		return
	}

	w.Header().Set("Location", absPath(r.Context(), "/configs/"+id))

	if preferMinimal(r) {
		writePreferCreated(w)
		return
	}

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse(r.Context(), "/configs/"+id, "Config", ConfigResponse{
			Config: swarm.Config{
				ID:   id,
				Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: req.Name}},
			},
			Services: []cache.ServiceRef{},
		}))
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse(r.Context(), "/configs/"+id, "Config", ConfigResponse{
		Config:   cfg,
		Services: h.cache.ServicesUsingConfig(id),
	}))
}

func (h *Handlers) HandleGetConfigLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", fmt.Sprintf("config %q not found", id))
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "config:"+cfg.Spec.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	labels := cfg.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/configs/"+id+"/labels", "ConfigLabels", LabelsResponse{
			Labels: labels,
		}),
	)
}

func (h *Handlers) HandlePatchConfigLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", fmt.Sprintf("config %q not found", id))
		return
	}

	updated, ok := patchStringMap(w, r, cfg.Spec.Labels)
	if !ok {
		return
	}

	slog.Info("patching config labels", "config", id)

	result, err := h.configWriter.UpdateConfigLabels(r.Context(), id, updated)
	if err != nil {
		writeConfigError(w, r, err, id)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeMutationResponse(w, r, labels)
}
