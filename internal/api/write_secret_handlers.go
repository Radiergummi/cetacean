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

func (h *Handlers) HandleRemoveSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", "secret not found")
		return
	}

	slog.Info("removing secret", "secret", id)

	err := h.writeClient.RemoveSecret(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "SEC001", err.Error())
			return
		}
		writeDockerError(w, r, err, "secret")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleCreateSecret(w http.ResponseWriter, r *http.Request) {
	var req createResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeErrorCode(w, r, "SEC004", "name is required")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeErrorCode(w, r, "SEC004", "data must be valid base64")
		return
	}

	slog.Info("creating secret", "name", req.Name)

	id, err := h.writeClient.CreateSecret(r.Context(), swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: req.Name},
		Data:        data,
	})
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, "SEC003", err.Error())
			return
		}
		writeDockerError(w, r, err, "secret")
		return
	}

	sec, ok := h.cache.GetSecret(id)
	if !ok {
		w.Header().Set("Location", absPath(r.Context(), "/secrets/"+id))
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", map[string]any{
			"secret": swarm.Secret{
				ID:   id,
				Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: req.Name}},
			},
			"services": []cache.ServiceRef{},
		}))
		return
	}

	sec.Spec.Data = nil
	w.Header().Set("Location", absPath(r.Context(), "/secrets/"+id))
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", map[string]any{
		"secret":   sec,
		"services": h.cache.ServicesUsingSecret(id),
	}))
}

func (h *Handlers) HandleGetSecretLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", "secret not found")
		return
	}
	labels := sec.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse(r.Context(), "/secrets/"+id+"/labels", "SecretLabels", map[string]any{
			"labels": labels,
		}),
	)
}

func (h *Handlers) HandlePatchSecretLabels(w http.ResponseWriter, r *http.Request) {
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

	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", "secret not found")
		return
	}

	current := sec.Spec.Labels
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

	slog.Info("patching secret labels", "secret", id)

	result, err := h.writeClient.UpdateSecretLabels(r.Context(), id, updated)
	if err != nil {
		writeSecretError(w, r, err)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSON(w, labels)
}
