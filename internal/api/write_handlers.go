package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/errdefs"
	json "github.com/goccy/go-json"
)

// writeDockerError maps Docker API errors to appropriate HTTP status codes.
func writeDockerError(w http.ResponseWriter, r *http.Request, err error, resource string) {
	if errdefs.IsNotFound(err) {
		writeProblem(w, r, http.StatusNotFound, resource+" not found")
		return
	}
	if errdefs.IsConflict(err) {
		writeProblem(w, r, http.StatusConflict, resource+" was modified by another client, please retry")
		return
	}
	slog.Error("failed to update "+resource, "error", err)
	writeProblem(w, r, http.StatusInternalServerError, "failed to update "+resource)
}

// writePatchError maps JSON Patch application errors to HTTP status codes.
func writePatchError(w http.ResponseWriter, r *http.Request, err error) {
	var tfe *testFailedError
	if errors.As(err, &tfe) {
		writeProblem(w, r, http.StatusConflict, err.Error())
		return
	}
	writeProblem(w, r, http.StatusBadRequest, err.Error())
}

type updateImageRequest struct {
	Image string `json:"image"`
}

type scaleRequest struct {
	Replicas *uint64 `json:"replicas"`
}

func (h *Handlers) HandleScaleService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Replicas == nil {
		writeProblem(w, r, http.StatusBadRequest, "replicas is required")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		writeProblem(w, r, http.StatusBadRequest, "cannot scale a global-mode service")
		return
	}

	slog.Info("scaling service", "service", id, "replicas", *req.Replicas)

	updated, err := h.writeClient.ScaleService(r.Context(), id, *req.Replicas)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	// Use writeJSON (not writeJSONWithETag) for mutation responses:
	// ETag + If-None-Match → 304 is only valid for safe methods (GET/HEAD)
	// per RFC 9110 Section 13.1.1.
	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

func (h *Handlers) HandleUpdateServiceImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req updateImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Image == "" {
		writeProblem(w, r, http.StatusBadRequest, "image is required")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("updating service image", "service", id, "image", req.Image)

	updated, err := h.writeClient.UpdateServiceImage(r.Context(), id, req.Image)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

func (h *Handlers) HandleRollbackService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	if svc.PreviousSpec == nil {
		writeProblem(w, r, http.StatusBadRequest, "service has no previous spec to rollback to")
		return
	}

	slog.Info("rolling back service", "service", id)

	updated, err := h.writeClient.RollbackService(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

type updateAvailabilityRequest struct {
	Availability string `json:"availability"`
}

func (h *Handlers) HandleUpdateNodeAvailability(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req updateAvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	var availability swarm.NodeAvailability
	switch req.Availability {
	case "active":
		availability = swarm.NodeAvailabilityActive
	case "drain":
		availability = swarm.NodeAvailabilityDrain
	case "pause":
		availability = swarm.NodeAvailabilityPause
	default:
		writeProblem(w, r, http.StatusBadRequest, "availability must be one of: active, drain, pause")
		return
	}

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	slog.Info("updating node availability", "node", id, "availability", req.Availability)

	updated, err := h.writeClient.UpdateNodeAvailability(r.Context(), id, availability)
	if err != nil {
		writeDockerError(w, r, err, "node")
		return
	}

	writeJSON(w, NewDetailResponse("/nodes/"+id, "Node", map[string]any{
		"node": updated,
	}))
}

func (h *Handlers) HandleRemoveTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetTask(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "task not found")
		return
	}

	slog.Info("removing task", "task", id)

	err := h.writeClient.RemoveTask(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "task")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("restarting service", "service", id)

	updated, err := h.writeClient.RestartService(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

// envSliceToMap converts a slice of KEY=VALUE strings to a map.
func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		}
	}
	return m
}

func (h *Handlers) HandleGetServiceEnv(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	var env []string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		env = svc.Spec.TaskTemplate.ContainerSpec.Env
	}
	writeJSONWithETag(w, r, envSliceToMap(env))
}

func (h *Handlers) HandlePatchServiceEnv(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/json-patch+json")
		return
	}

	var ops []PatchOp
	if err := json.NewDecoder(r.Body).Decode(&ops); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var env []string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		env = svc.Spec.TaskTemplate.ContainerSpec.Env
	}
	current := envSliceToMap(env)

	updated, err := applyJSONPatch(current, ops)
	if err != nil {
		writePatchError(w, r, err)
		return
	}

	slog.Info("patching service env", "service", id)

	result, err := h.writeClient.UpdateServiceEnv(r.Context(), id, updated)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	var resultEnv []string
	if result.Spec.TaskTemplate.ContainerSpec != nil {
		resultEnv = result.Spec.TaskTemplate.ContainerSpec.Env
	}
	writeJSON(w, envSliceToMap(resultEnv))
}

func (h *Handlers) HandleGetNodeLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	node, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}
	labels := node.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSONWithETag(w, r, labels)
}

func (h *Handlers) HandlePatchNodeLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/json-patch+json")
		return
	}

	var ops []PatchOp
	if err := json.NewDecoder(r.Body).Decode(&ops); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	current := node.Spec.Labels
	if current == nil {
		current = map[string]string{}
	}

	updated, err := applyJSONPatch(current, ops)
	if err != nil {
		writePatchError(w, r, err)
		return
	}

	slog.Info("patching node labels", "node", id)

	result, err := h.writeClient.UpdateNodeLabels(r.Context(), id, updated)
	if err != nil {
		writeDockerError(w, r, err, "node")
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSON(w, labels)
}

func (h *Handlers) HandleGetServiceResources(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	resources := svc.Spec.TaskTemplate.Resources
	if resources == nil {
		resources = &swarm.ResourceRequirements{}
	}
	writeJSONWithETag(w, r, resources)
}

func (h *Handlers) HandlePatchServiceResources(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/merge-patch+json")
		return
	}

	patchBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.TaskTemplate.Resources
	if current == nil {
		current = &swarm.ResourceRequirements{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to process resources")
		return
	}
	var merged swarm.ResourceRequirements
	if err := json.Unmarshal(base, &merged); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to process resources")
		return
	}
	if err := json.Unmarshal(patchBytes, &merged); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid patch body")
		return
	}

	slog.Info("patching service resources", "service", id)

	result, err := h.writeClient.UpdateServiceResources(r.Context(), id, &merged)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	resources := result.Spec.TaskTemplate.Resources
	if resources == nil {
		resources = &swarm.ResourceRequirements{}
	}
	writeJSON(w, resources)
}
