package api

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

func (h *Handlers) HandleRemoveSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", fmt.Sprintf("secret %q not found", id))
		return
	}

	slog.Info("removing secret", "secret", id)

	err := h.secretWriter.RemoveSecret(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "SEC001", err.Error())
			return
		}
		writeDockerError(w, r, err, "secret", id)
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

	id, err := h.secretWriter.CreateSecret(r.Context(), swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: req.Name},
		Data:        data,
	})
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, "SEC003", err.Error())
			return
		}
		writeDockerError(w, r, err, "secret", req.Name)
		return
	}

	w.Header().Set("Location", absPath(r.Context(), "/secrets/"+id))

	if preferMinimal(r) {
		writePreferCreated(w)
		return
	}

	sec, ok := h.cache.GetSecret(id)
	if !ok {
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", SecretResponse{
			Secret: swarm.Secret{
				ID:   id,
				Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: req.Name}},
			},
			Services: []cache.ServiceRef{},
		}))
		return
	}

	sec.Spec.Data = nil
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", SecretResponse{
		Secret:   sec,
		Services: h.cache.ServicesUsingSecret(id),
	}))
}

func (h *Handlers) HandleGetSecretLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, ok := h.cache.GetSecret(id)
	if !ok {
		writeErrorCode(w, r, "SEC002", fmt.Sprintf("secret %q not found", id))
		return
	}
	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "secret:"+sec.Spec.Name) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}
	labels := sec.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/secrets/"+id+"/labels", "SecretLabels", LabelsResponse{
			Labels: labels,
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
		writeErrorCode(w, r, "SEC002", fmt.Sprintf("secret %q not found", id))
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

	result, err := h.secretWriter.UpdateSecretLabels(r.Context(), id, updated)
	if err != nil {
		writeSecretError(w, r, err, id)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeMutationResponse(w, r, labels)
}
