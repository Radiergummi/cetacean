package api

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
)

func (h *Handlers) HandleRemoveConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", "config not found")
		return
	}

	slog.Info("removing config", "config", id)

	err := h.writeClient.RemoveConfig(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "CFG001", err.Error())
			return
		}
		writeDockerError(w, r, err, "config")
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

	id, err := h.writeClient.CreateConfig(r.Context(), swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: req.Name},
		Data:        data,
	})
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, "CFG003", err.Error())
			return
		}
		writeDockerError(w, r, err, "config")
		return
	}

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		w.Header().Set("Location", "/configs/"+id)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse("/configs/"+id, "Config", map[string]any{
			"config": swarm.Config{
				ID:   id,
				Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: req.Name}},
			},
			"services": []cache.ServiceRef{},
		}))
		return
	}

	w.Header().Set("Location", "/configs/"+id)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse("/configs/"+id, "Config", map[string]any{
		"config":   cfg,
		"services": h.cache.ServicesUsingConfig(id),
	}))
}

func (h *Handlers) HandleGetConfigLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", "config not found")
		return
	}
	labels := cfg.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/configs/"+id+"/labels", "ConfigLabels", map[string]any{
			"labels": labels,
		}),
	)
}

func (h *Handlers) HandlePatchConfigLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	isJSONPatch := strings.HasPrefix(ct, "application/json-patch+json")
	isMergePatch := strings.HasPrefix(ct, "application/merge-patch+json")

	if !isJSONPatch && !isMergePatch {
		writeErrorCode(
			w,
			r,
			"API004",
			"Content-Type must be application/json-patch+json or application/merge-patch+json",
		)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		writeErrorCode(w, r, "CFG002", "config not found")
		return
	}

	current := cfg.Spec.Labels
	if current == nil {
		current = map[string]string{}
	}

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeErrorCode(w, r, "API006", "invalid request body")
			return
		}
		updated, err = applyJSONPatch(current, ops)
	} else {
		updated, err = applyMergePatchStringMap(current, body)
	}

	if err != nil {
		writePatchError(w, r, err)
		return
	}

	slog.Info("patching config labels", "config", id)

	result, err := h.writeClient.UpdateConfigLabels(r.Context(), id, updated)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSON(w, labels)
}
