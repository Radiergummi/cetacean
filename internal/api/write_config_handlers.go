package api

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
)

func (h *Handlers) HandleRemoveConfig(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Config]{
		resource:     "config",
		pathKey:      "id",
		getter:       h.cache.GetConfig,
		remove:       h.configWriter.RemoveConfig,
		conflictCode: "CFG001",
	})
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
	handleGetLabels(w, r, h.acl, getLabelsSpec[swarm.Config]{
		resource:    "config",
		pathKey:     "id",
		typeName:    "ConfigLabels",
		getter:      h.cache.GetConfig,
		aclResource: func(c swarm.Config) string { return "config:" + c.Spec.Name },
		getLabels:   func(c swarm.Config) map[string]string { return c.Spec.Labels },
	})
}

func (h *Handlers) HandlePatchConfigLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, patchLabelsSpec[swarm.Config]{
		resource:     "config",
		pathKey:      "id",
		getter:       h.cache.GetConfig,
		getLabels:    func(c swarm.Config) map[string]string { return c.Spec.Labels },
		update:       h.configWriter.UpdateConfigLabels,
		conflictCode: "CFG005",
	})
}
