package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"
)

// writeDockerError maps Docker API errors to appropriate HTTP status codes.
func writeDockerError(w http.ResponseWriter, r *http.Request, err error, resource string) {
	if cerrdefs.IsNotFound(err) {
		writeProblem(w, r, http.StatusNotFound, resource+" not found")
		return
	}
	if cerrdefs.IsConflict(err) {
		writeProblem(
			w,
			r,
			http.StatusConflict,
			resource+" was modified by another client, please retry",
		)
		return
	}
	if cerrdefs.IsInvalidArgument(err) {
		writeProblem(w, r, http.StatusBadRequest, err.Error())
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

type updateModeRequest struct {
	Mode     string  `json:"mode"`
	Replicas *uint64 `json:"replicas,omitempty"`
}

type updateImageRequest struct {
	Image string `json:"image"`
}

type scaleRequest struct {
	Replicas *uint64 `json:"replicas"`
}

func (h *Handlers) HandleScaleService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

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

func (h *Handlers) HandleUpdateServiceMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	var req updateModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	var mode swarm.ServiceMode
	switch req.Mode {
	case "replicated":
		if req.Replicas == nil {
			writeProblem(
				w,
				r,
				http.StatusBadRequest,
				"replicas is required when switching to replicated mode",
			)
			return
		}
		mode.Replicated = &swarm.ReplicatedService{Replicas: req.Replicas}
	case "global":
		mode.Global = &swarm.GlobalService{}
	default:
		writeProblem(w, r, http.StatusBadRequest, "mode must be one of: replicated, global")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("updating service mode", "service", id, "mode", req.Mode)

	updated, err := h.writeClient.UpdateServiceMode(r.Context(), id, mode)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

type updateEndpointModeRequest struct {
	Mode string `json:"mode"`
}

func (h *Handlers) HandleUpdateServiceEndpointMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req updateEndpointModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	var mode swarm.ResolutionMode
	switch req.Mode {
	case "vip":
		mode = swarm.ResolutionModeVIP
	case "dnsrr":
		mode = swarm.ResolutionModeDNSRR
	default:
		writeProblem(w, r, http.StatusBadRequest, "mode must be one of: vip, dnsrr")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("updating service endpoint mode", "service", id, "mode", req.Mode)

	updated, err := h.writeClient.UpdateServiceEndpointMode(r.Context(), id, mode)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id, "Service", map[string]any{
		"service": updated,
	}))
}

func (h *Handlers) HandleUpdateServiceImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

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
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

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
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"availability must be one of: active, drain, pause",
		)
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

func (h *Handlers) HandleRemoveService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	slog.Info("removing service", "service", id)

	err := h.writeClient.RemoveService(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type updateRoleRequest struct {
	Role string `json:"role"`
}

func (h *Handlers) HandleUpdateNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	var role swarm.NodeRole
	switch req.Role {
	case "worker":
		role = swarm.NodeRoleWorker
	case "manager":
		role = swarm.NodeRoleManager
	default:
		writeProblem(w, r, http.StatusBadRequest, "role must be one of: worker, manager")
		return
	}

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	slog.Info("updating node role", "node", id, "role", req.Role)

	updated, err := h.writeClient.UpdateNodeRole(r.Context(), id, role)
	if err != nil {
		writeDockerError(w, r, err, "node")
		return
	}

	writeJSON(w, NewDetailResponse("/nodes/"+id, "Node", map[string]any{
		"node": updated,
	}))
}

func (h *Handlers) HandleRemoveNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	slog.Info("removing node", "node", id)

	err := h.writeClient.RemoveNode(r.Context(), id)
	if err != nil {
		writeDockerError(w, r, err, "node")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleGetNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "node not found")
		return
	}

	managerCount := 0
	for _, n := range h.cache.ListNodes() {
		if n.Spec.Role == swarm.NodeRoleManager {
			managerCount++
		}
	}

	writeJSONWithETag(w, r, map[string]any{
		"role":         string(node.Spec.Role),
		"isLeader":     node.ManagerStatus != nil && node.ManagerStatus.Leader,
		"managerCount": managerCount,
	})
}

type removeError struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Error string `json:"error"`
}

type removeStackResponse struct {
	Removed struct {
		Services int `json:"services"`
		Networks int `json:"networks"`
		Configs  int `json:"configs"`
		Secrets  int `json:"secrets"`
	} `json:"removed"`
	Errors []removeError `json:"errors,omitempty"`
}

func (h *Handlers) HandleRemoveStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	stack, ok := h.cache.GetStack(name)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "stack not found")
		return
	}

	slog.Info("removing stack", "stack", name,
		"services", len(stack.Services),
		"networks", len(stack.Networks),
		"configs", len(stack.Configs),
		"secrets", len(stack.Secrets),
	)

	ctx := r.Context()
	var resp removeStackResponse
	var errs []removeError

	for _, id := range stack.Services {
		if err := h.writeClient.RemoveService(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "service", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Services++
	}

	for _, id := range stack.Networks {
		if err := h.writeClient.RemoveNetwork(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "network", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Networks++
	}

	for _, id := range stack.Secrets {
		if err := h.writeClient.RemoveSecret(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "secret", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Secrets++
	}

	for _, id := range stack.Configs {
		if err := h.writeClient.RemoveConfig(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "config", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Configs++
	}

	if len(errs) > 0 {
		resp.Errors = errs
	}

	writeJSON(w, resp)
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
// Bare-key entries (KEY without =) are preserved with an empty string value.
func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			m[k] = v
		} else {
			m[e] = ""
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
	writeJSONWithETag(w, r, NewDetailResponse("/services/"+id+"/env", "ServiceEnv", map[string]any{
		"env": envSliceToMap(env),
	}))
}

func (h *Handlers) HandlePatchServiceEnv(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	ct := r.Header.Get("Content-Type")
	isJSONPatch := strings.HasPrefix(ct, "application/json-patch+json")
	isMergePatch := strings.HasPrefix(ct, "application/merge-patch+json")

	if !isJSONPatch && !isMergePatch {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"Content-Type must be application/json-patch+json or application/merge-patch+json",
		)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
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

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid request body")
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
	writeJSONWithETag(w, r, NewDetailResponse("/nodes/"+id+"/labels", "NodeLabels", map[string]any{
		"labels": labels,
	}))
}

func (h *Handlers) HandlePatchNodeLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	ct := r.Header.Get("Content-Type")
	isJSONPatch := strings.HasPrefix(ct, "application/json-patch+json")
	isMergePatch := strings.HasPrefix(ct, "application/merge-patch+json")

	if !isJSONPatch && !isMergePatch {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"Content-Type must be application/json-patch+json or application/merge-patch+json",
		)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
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

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid request body")
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

func (h *Handlers) HandleGetServiceLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	labels := svc.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/labels", "ServiceLabels", map[string]any{
			"labels": labels,
		}),
	)
}

func (h *Handlers) HandlePatchServiceLabels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	ct := r.Header.Get("Content-Type")
	isJSONPatch := strings.HasPrefix(ct, "application/json-patch+json")
	isMergePatch := strings.HasPrefix(ct, "application/merge-patch+json")

	if !isJSONPatch && !isMergePatch {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"Content-Type must be application/json-patch+json or application/merge-patch+json",
		)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.Labels
	if current == nil {
		current = map[string]string{}
	}

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid request body")
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

	slog.Info("patching service labels", "service", id)

	result, err := h.writeClient.UpdateServiceLabels(r.Context(), id, updated)
	if err != nil {
		writeDockerError(w, r, err, "service")
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
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/resources", "ServiceResources", map[string]any{
			"resources": resources,
		}),
	)
}

func (h *Handlers) HandlePatchServiceResources(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"expected Content-Type: application/merge-patch+json",
		)
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

	// Marshal current state to JSON, then to a generic map
	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current resources")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current resources")
		return
	}

	// Read the patch
	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Apply RFC 7396 merge: null deletes, non-null overwrites
	mergePatch(baseMap, patchMap)

	// Marshal back to struct
	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged resources")
		return
	}
	var result swarm.ResourceRequirements
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid resource specification")
		return
	}

	slog.Info("updating service resources", "service", id)
	updated, err := h.writeClient.UpdateServiceResources(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}
	writeJSON(w, NewDetailResponse("/services/"+id+"/resources", "ServiceResources", map[string]any{
		"resources": updated.Spec.TaskTemplate.Resources,
	}))
}

// mergePatch applies RFC 7396 JSON Merge Patch semantics to a base map.
// null values in patch delete keys from base; non-null values overwrite.
// Nested objects are merged recursively.
func mergePatch(base, patch map[string]any) {
	for k, v := range patch {
		if v == nil {
			delete(base, k)
		} else if patchObj, ok := v.(map[string]any); ok {
			if baseObj, ok := base[k].(map[string]any); ok {
				mergePatch(baseObj, patchObj)
			} else {
				base[k] = v
			}
		} else {
			base[k] = v
		}
	}
}

func (h *Handlers) HandleGetServicePorts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	var ports []swarm.PortConfig
	if svc.Spec.EndpointSpec != nil {
		ports = svc.Spec.EndpointSpec.Ports
	}
	if ports == nil {
		ports = []swarm.PortConfig{}
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/ports", "ServicePorts", map[string]any{
			"ports": ports,
		}),
	)
}

func (h *Handlers) HandlePatchServicePorts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var patch struct {
		Ports []swarm.PortConfig `json:"ports"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	slog.Info("updating service ports", "service", id)

	updated, err := h.writeClient.UpdateServicePorts(r.Context(), id, patch.Ports)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	var resultPorts []swarm.PortConfig
	if updated.Spec.EndpointSpec != nil {
		resultPorts = updated.Spec.EndpointSpec.Ports
	}
	if resultPorts == nil {
		resultPorts = []swarm.PortConfig{}
	}
	writeJSON(w, NewDetailResponse("/services/"+id+"/ports", "ServicePorts", map[string]any{
		"ports": resultPorts,
	}))
}

func (h *Handlers) HandleGetServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var hc *container.HealthConfig
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		hc = svc.Spec.TaskTemplate.ContainerSpec.Healthcheck
	}

	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/healthcheck", "ServiceHealthcheck", map[string]any{
			"healthcheck": hc,
		}),
	)
}

func (h *Handlers) HandlePutServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var hc container.HealthConfig
	if err := json.NewDecoder(r.Body).Decode(&hc); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	slog.Info("updating service healthcheck", "service", id)

	updated, err := h.writeClient.UpdateServiceHealthcheck(r.Context(), id, &hc)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	var resultHC *container.HealthConfig
	if updated.Spec.TaskTemplate.ContainerSpec != nil {
		resultHC = updated.Spec.TaskTemplate.ContainerSpec.Healthcheck
	}

	writeJSON(
		w,
		NewDetailResponse("/services/"+id+"/healthcheck", "ServiceHealthcheck", map[string]any{
			"healthcheck": resultHC,
		}),
	)
}

func (h *Handlers) HandleGetServicePlacement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	placement := svc.Spec.TaskTemplate.Placement
	if placement == nil {
		placement = &swarm.Placement{}
	}

	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/placement", "ServicePlacement", map[string]any{
			"placement": placement,
		}),
	)
}

func (h *Handlers) HandlePutServicePlacement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	_, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	var placement swarm.Placement
	if err := json.NewDecoder(r.Body).Decode(&placement); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	slog.Info("updating service placement", "service", id)

	updated, err := h.writeClient.UpdateServicePlacement(r.Context(), id, &placement)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	resultPlacement := updated.Spec.TaskTemplate.Placement
	if resultPlacement == nil {
		resultPlacement = &swarm.Placement{}
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/placement", "ServicePlacement", map[string]any{
		"placement": resultPlacement,
	}))
}

func (h *Handlers) HandleGetServiceUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	policy := svc.Spec.UpdateConfig
	if policy == nil {
		policy = &swarm.UpdateConfig{}
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/update-policy", "ServiceUpdatePolicy", map[string]any{
			"updatePolicy": policy,
		}),
	)
}

func (h *Handlers) HandlePatchServiceUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.UpdateConfig
	if current == nil {
		current = &swarm.UpdateConfig{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(
			w,
			r,
			http.StatusInternalServerError,
			"failed to marshal current update policy",
		)
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(
			w,
			r,
			http.StatusInternalServerError,
			"failed to unmarshal current update policy",
		)
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged update policy")
		return
	}
	var result swarm.UpdateConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid update policy specification")
		return
	}

	slog.Info("updating service update policy", "service", id)

	updated, err := h.writeClient.UpdateServiceUpdatePolicy(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	resultPolicy := updated.Spec.UpdateConfig
	if resultPolicy == nil {
		resultPolicy = &swarm.UpdateConfig{}
	}
	writeJSON(
		w,
		NewDetailResponse("/services/"+id+"/update-policy", "ServiceUpdatePolicy", map[string]any{
			"updatePolicy": resultPolicy,
		}),
	)
}

func (h *Handlers) HandleGetServiceRollbackPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	policy := svc.Spec.RollbackConfig
	if policy == nil {
		policy = &swarm.UpdateConfig{}
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse(
			"/services/"+id+"/rollback-policy",
			"ServiceRollbackPolicy",
			map[string]any{
				"rollbackPolicy": policy,
			},
		),
	)
}

func (h *Handlers) HandlePatchServiceRollbackPolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.RollbackConfig
	if current == nil {
		current = &swarm.UpdateConfig{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(
			w,
			r,
			http.StatusInternalServerError,
			"failed to marshal current rollback policy",
		)
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(
			w,
			r,
			http.StatusInternalServerError,
			"failed to unmarshal current rollback policy",
		)
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(
			w,
			r,
			http.StatusInternalServerError,
			"failed to marshal merged rollback policy",
		)
		return
	}
	var result swarm.UpdateConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid rollback policy specification")
		return
	}

	slog.Info("updating service rollback policy", "service", id)

	updated, err := h.writeClient.UpdateServiceRollbackPolicy(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	resultPolicy := updated.Spec.RollbackConfig
	if resultPolicy == nil {
		resultPolicy = &swarm.UpdateConfig{}
	}
	writeJSON(
		w,
		NewDetailResponse(
			"/services/"+id+"/rollback-policy",
			"ServiceRollbackPolicy",
			map[string]any{
				"rollbackPolicy": resultPolicy,
			},
		),
	)
}

func (h *Handlers) HandleGetServiceLogDriver(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/log-driver", "ServiceLogDriver", map[string]any{
			"logDriver": svc.Spec.TaskTemplate.LogDriver,
		}),
	)
}

func (h *Handlers) HandlePatchServiceLogDriver(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := svc.Spec.TaskTemplate.LogDriver
	if current == nil {
		current = &swarm.Driver{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current log driver")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current log driver")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged log driver")
		return
	}
	var result swarm.Driver
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid log driver specification")
		return
	}

	slog.Info("updating service log driver", "service", id)

	updated, err := h.writeClient.UpdateServiceLogDriver(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	writeJSON(
		w,
		NewDetailResponse("/services/"+id+"/log-driver", "ServiceLogDriver", map[string]any{
			"logDriver": updated.Spec.TaskTemplate.LogDriver,
		}),
	)
}

func (h *Handlers) HandlePatchServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(
			w,
			r,
			http.StatusUnsupportedMediaType,
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := &container.HealthConfig{}
	if svc.Spec.TaskTemplate.ContainerSpec != nil &&
		svc.Spec.TaskTemplate.ContainerSpec.Healthcheck != nil {
		current = svc.Spec.TaskTemplate.ContainerSpec.Healthcheck
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current healthcheck")
		return
	}

	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeProblem(
			w,
			r,
			http.StatusInternalServerError,
			"failed to unmarshal current healthcheck",
		)
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}

	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged healthcheck")
		return
	}

	var result container.HealthConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid healthcheck specification")
		return
	}

	slog.Info("updating service healthcheck", "service", id)

	updated, err := h.writeClient.UpdateServiceHealthcheck(r.Context(), id, &result)
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	var resultHC *container.HealthConfig
	if updated.Spec.TaskTemplate.ContainerSpec != nil {
		resultHC = updated.Spec.TaskTemplate.ContainerSpec.Healthcheck
	}

	writeJSON(
		w,
		NewDetailResponse("/services/"+id+"/healthcheck", "ServiceHealthcheck", map[string]any{
			"healthcheck": resultHC,
		}),
	)
}

type containerConfigResponse struct {
	Command         []string       `json:"command"`
	Args            []string       `json:"args"`
	Dir             string         `json:"dir"`
	User            string         `json:"user"`
	Hostname        string         `json:"hostname"`
	Init            *bool          `json:"init"`
	TTY             bool           `json:"tty"`
	ReadOnly        bool           `json:"readOnly"`
	StopSignal      string         `json:"stopSignal"`
	StopGracePeriod *int64         `json:"stopGracePeriod"`
	CapabilityAdd   []string       `json:"capabilityAdd"`
	CapabilityDrop  []string       `json:"capabilityDrop"`
	Groups          []string       `json:"groups"`
	Hosts           []string       `json:"hosts"`
	DNSConfig       *dnsConfigJSON `json:"dnsConfig"`
}

type dnsConfigJSON struct {
	Nameservers []string `json:"nameservers"`
	Search      []string `json:"search"`
	Options     []string `json:"options"`
}

func containerConfigFromSpec(cs *swarm.ContainerSpec) containerConfigResponse {
	if cs == nil {
		return containerConfigResponse{}
	}
	resp := containerConfigResponse{
		Command:        cs.Command,
		Args:           cs.Args,
		Dir:            cs.Dir,
		User:           cs.User,
		Hostname:       cs.Hostname,
		Init:           cs.Init,
		TTY:            cs.TTY,
		ReadOnly:       cs.ReadOnly,
		StopSignal:     cs.StopSignal,
		CapabilityAdd:  cs.CapabilityAdd,
		CapabilityDrop: cs.CapabilityDrop,
		Groups:         cs.Groups,
		Hosts:          cs.Hosts,
	}
	if cs.StopGracePeriod != nil {
		ns := int64(*cs.StopGracePeriod)
		resp.StopGracePeriod = &ns
	}
	if cs.DNSConfig != nil {
		resp.DNSConfig = &dnsConfigJSON{
			Nameservers: cs.DNSConfig.Nameservers,
			Search:      cs.DNSConfig.Search,
			Options:     cs.DNSConfig.Options,
		}
	}
	return resp
}

func (h *Handlers) HandleGetServiceContainerConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	resp := containerConfigFromSpec(svc.Spec.TaskTemplate.ContainerSpec)
	writeJSON(w, resp)
}

func (h *Handlers) HandlePatchServiceContainerConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeProblem(w, r, http.StatusUnsupportedMediaType, "expected Content-Type: application/merge-patch+json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "service not found")
		return
	}

	current := containerConfigFromSpec(svc.Spec.TaskTemplate.ContainerSpec)
	baseBytes, err := json.Marshal(current)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal current config")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(baseBytes, &baseMap); err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to unmarshal current config")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid request body")
		return
	}

	mergePatch(baseMap, patchMap)

	mergedBytes, err := json.Marshal(baseMap)
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "failed to marshal merged config")
		return
	}
	var merged containerConfigResponse
	if err := json.Unmarshal(mergedBytes, &merged); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid patch result")
		return
	}

	slog.Info("updating service container config", "service", id)

	updated, err := h.writeClient.UpdateServiceContainerConfig(r.Context(), id, func(cs *swarm.ContainerSpec) {
		cs.Command = merged.Command
		cs.Args = merged.Args
		cs.Dir = merged.Dir
		cs.User = merged.User
		cs.Hostname = merged.Hostname
		cs.Init = merged.Init
		cs.TTY = merged.TTY
		cs.ReadOnly = merged.ReadOnly
		cs.StopSignal = merged.StopSignal
		cs.CapabilityAdd = merged.CapabilityAdd
		cs.CapabilityDrop = merged.CapabilityDrop
		cs.Groups = merged.Groups
		cs.Hosts = merged.Hosts
		if merged.StopGracePeriod != nil {
			d := time.Duration(*merged.StopGracePeriod)
			cs.StopGracePeriod = &d
		} else {
			cs.StopGracePeriod = nil
		}
		if merged.DNSConfig != nil {
			cs.DNSConfig = &swarm.DNSConfig{
				Nameservers: merged.DNSConfig.Nameservers,
				Search:      merged.DNSConfig.Search,
				Options:     merged.DNSConfig.Options,
			}
		} else {
			cs.DNSConfig = nil
		}
	})
	if err != nil {
		writeDockerError(w, r, err, "service")
		return
	}

	result := containerConfigFromSpec(updated.Spec.TaskTemplate.ContainerSpec)
	writeJSON(w, result)
}
