package api

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/cache"
)

// writeDockerError handles Docker API errors that don't have a domain-specific
// error code. Handlers should check for IsConflict/IsFailedPrecondition
// themselves and call writeErrorCode with the appropriate code before falling
// through to this function.
var notFoundCodes = map[string]string{
	"service": "SVC003",
	"node":    "NOD003",
	"task":    "TSK002",
	"volume":  "VOL002",
	"network": "NET002",
	"config":  "CFG002",
	"secret":  "SEC002",
	"plugin":  "PLG004",
}

func writeDockerError(w http.ResponseWriter, r *http.Request, err error, resource string) {
	if cerrdefs.IsNotFound(err) {
		if code, ok := notFoundCodes[resource]; ok {
			writeErrorCode(w, r, code, resource+" not found")
		} else {
			writeProblem(w, r, http.StatusNotFound, resource+" not found")
		}
		return
	}
	if cerrdefs.IsInvalidArgument(err) {
		writeErrorCode(w, r, "ENG003", err.Error())
		return
	}
	if cerrdefs.IsUnavailable(err) {
		writeErrorCode(w, r, "ENG001", err.Error())
		return
	}
	slog.Error("failed to update "+resource, "error", err)
	writeErrorCode(w, r, "ENG004", "failed to update "+resource)
}

// writeServiceError handles Docker API errors for service mutations,
// mapping version conflicts to SVC001.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "SVC001", err.Error())
		return
	}
	writeDockerError(w, r, err, "service")
}

// writeNodeError handles Docker API errors for node mutations,
// mapping version conflicts to NOD002.
func writeNodeError(w http.ResponseWriter, r *http.Request, err error) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "NOD002", err.Error())
		return
	}
	writeDockerError(w, r, err, "node")
}

// writePatchError maps JSON Patch application errors to error codes.
func writePatchError(w http.ResponseWriter, r *http.Request, err error) {
	var tfe *testFailedError
	if errors.As(err, &tfe) {
		writeErrorCode(w, r, "API010", err.Error())
		return
	}
	writeErrorCode(w, r, "API011", err.Error())
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

type serviceConfigRef struct {
	ConfigID   string `json:"configID"`
	ConfigName string `json:"configName"`
	FileName   string `json:"fileName"`
}

type serviceSecretRef struct {
	SecretID   string `json:"secretID"`
	SecretName string `json:"secretName"`
	FileName   string `json:"fileName"`
}

type serviceNetworkRef struct {
	Target  string   `json:"target"`
	Aliases []string `json:"aliases,omitempty"`
}

func (h *Handlers) HandleScaleService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit

	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}
	if req.Replicas == nil {
		writeErrorCode(w, r, "SVC004", "replicas is required")
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		writeErrorCode(w, r, "SVC005", "cannot scale a global-mode service")
		return
	}

	slog.Info("scaling service", "service", id, "replicas", *req.Replicas)

	updated, err := h.writeClient.ScaleService(r.Context(), id, *req.Replicas)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	var mode swarm.ServiceMode
	switch req.Mode {
	case "replicated":
		if req.Replicas == nil {
			writeErrorCode(
				w,
				r,
				"SVC009",
				"replicas is required when switching to replicated mode",
			)
			return
		}
		mode.Replicated = &swarm.ReplicatedService{Replicas: req.Replicas}
	case "global":
		mode.Global = &swarm.GlobalService{}
	default:
		writeErrorCode(w, r, "SVC008", "mode must be one of: replicated, global")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	slog.Info("updating service mode", "service", id, "mode", req.Mode)

	updated, err := h.writeClient.UpdateServiceMode(r.Context(), id, mode)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	var mode swarm.ResolutionMode
	switch req.Mode {
	case "vip":
		mode = swarm.ResolutionModeVIP
	case "dnsrr":
		mode = swarm.ResolutionModeDNSRR
	default:
		writeErrorCode(w, r, "SVC010", "mode must be one of: vip, dnsrr")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	slog.Info("updating service endpoint mode", "service", id, "mode", req.Mode)

	updated, err := h.writeClient.UpdateServiceEndpointMode(r.Context(), id, mode)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}
	if req.Image == "" {
		writeErrorCode(w, r, "SVC006", "image is required")
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	slog.Info("updating service image", "service", id, "image", req.Image)

	updated, err := h.writeClient.UpdateServiceImage(r.Context(), id, req.Image)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}
	if svc.PreviousSpec == nil {
		writeErrorCode(w, r, "SVC007", "service has no previous spec to rollback to")
		return
	}

	slog.Info("rolling back service", "service", id)

	updated, err := h.writeClient.RollbackService(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "API006", "invalid request body")
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
		writeErrorCode(
			w,
			r,
			"NOD004",
			"availability must be one of: active, drain, pause",
		)
		return
	}

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", "node not found")
		return
	}

	slog.Info("updating node availability", "node", id, "availability", req.Availability)

	updated, err := h.writeClient.UpdateNodeAvailability(r.Context(), id, availability)
	if err != nil {
		writeNodeError(w, r, err)
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
		writeErrorCode(w, r, "TSK002", "task not found")
		return
	}

	slog.Info("removing task", "task", id)

	err := h.writeClient.RemoveTask(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "TSK001", err.Error())
			return
		}
		writeDockerError(w, r, err, "task")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemoveService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	slog.Info("removing service", "service", id)

	err := h.writeClient.RemoveService(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "SVC002", err.Error())
			return
		}
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
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	var role swarm.NodeRole
	switch req.Role {
	case "worker":
		role = swarm.NodeRoleWorker
	case "manager":
		role = swarm.NodeRoleManager
	default:
		writeErrorCode(w, r, "NOD005", "role must be one of: worker, manager")
		return
	}

	_, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", "node not found")
		return
	}

	slog.Info("updating node role", "node", id, "role", req.Role)

	updated, err := h.writeClient.UpdateNodeRole(r.Context(), id, role)
	if err != nil {
		writeNodeError(w, r, err)
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
		writeErrorCode(w, r, "NOD003", "node not found")
		return
	}

	force := r.URL.Query().Get("force") == "true"

	slog.Info("removing node", "node", id, "force", force)

	err := h.writeClient.RemoveNode(r.Context(), id, force)
	if err != nil {
		if !force && (cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err)) {
			writeErrorCode(w, r, "NOD001", err.Error())
			return
		}
		writeDockerError(w, r, err, "node")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleGetNodeRole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", "node not found")
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
		writeErrorCode(w, r, "STK001", "stack not found")
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

type createResourceRequest struct {
	Name string `json:"name"`
	Data string `json:"data"`
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
		w.Header().Set("Location", "/secrets/"+id)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, NewDetailResponse("/secrets/"+id, "Secret", map[string]any{
			"secret": swarm.Secret{
				ID:   id,
				Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: req.Name}},
			},
			"services": []cache.ServiceRef{},
		}))
		return
	}

	sec.Spec.Data = nil
	w.Header().Set("Location", "/secrets/"+id)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse("/secrets/"+id, "Secret", map[string]any{
		"secret":   sec,
		"services": h.cache.ServicesUsingSecret(id),
	}))
}

func (h *Handlers) HandleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetNetwork(id)
	if !ok {
		writeErrorCode(w, r, "NET002", "network not found")
		return
	}

	slog.Info("removing network", "network", id)

	err := h.writeClient.RemoveNetwork(r.Context(), id)
	if err != nil {
		if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
			writeErrorCode(w, r, "NET001", err.Error())
			return
		}
		writeDockerError(w, r, err, "network")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRemoveVolume(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	_, ok := h.cache.GetVolume(name)
	if !ok {
		writeErrorCode(w, r, "VOL002", "volume not found")
		return
	}

	force := r.URL.Query().Get("force") == "true"

	slog.Info("removing volume", "volume", name, "force", force)

	err := h.writeClient.RemoveVolume(r.Context(), name, force)
	if err != nil {
		if !force && (cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err)) {
			writeErrorCode(w, r, "VOL001", err.Error())
			return
		}
		writeDockerError(w, r, err, "volume")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	slog.Info("restarting service", "service", id)

	updated, err := h.writeClient.RestartService(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
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

	slog.Info("patching service env", "service", id)

	result, err := h.writeClient.UpdateServiceEnv(r.Context(), id, updated)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "NOD003", "node not found")
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

	node, ok := h.cache.GetNode(id)
	if !ok {
		writeErrorCode(w, r, "NOD003", "node not found")
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

	slog.Info("patching node labels", "node", id)

	result, err := h.writeClient.UpdateNodeLabels(r.Context(), id, updated)
	if err != nil {
		writeNodeError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
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

	slog.Info("patching service labels", "service", id)

	result, err := h.writeClient.UpdateServiceLabels(r.Context(), id, updated)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	current := svc.Spec.TaskTemplate.Resources
	if current == nil {
		current = &swarm.ResourceRequirements{}
	}

	// Marshal current state to JSON, then to a generic map
	base, err := json.Marshal(current)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal current resources")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeErrorCode(w, r, "API009", "failed to unmarshal current resources")
		return
	}

	// Read the patch
	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	// Apply RFC 7396 merge: null deletes, non-null overwrites
	mergePatch(baseMap, patchMap)

	// Marshal back to struct
	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal merged resources")
		return
	}
	var result swarm.ResourceRequirements
	if err := json.Unmarshal(merged, &result); err != nil {
		writeErrorCode(w, r, "SVC011", "invalid resource specification")
		return
	}

	slog.Info("updating service resources", "service", id)
	updated, err := h.writeClient.UpdateServiceResources(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var patch struct {
		Ports []swarm.PortConfig `json:"ports"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	slog.Info("updating service ports", "service", id)

	updated, err := h.writeClient.UpdateServicePorts(r.Context(), id, patch.Ports)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var hc container.HealthConfig
	if err := json.NewDecoder(r.Body).Decode(&hc); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	slog.Info("updating service healthcheck", "service", id)

	updated, err := h.writeClient.UpdateServiceHealthcheck(r.Context(), id, &hc)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var placement swarm.Placement
	if err := json.NewDecoder(r.Body).Decode(&placement); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	slog.Info("updating service placement", "service", id)

	updated, err := h.writeClient.UpdateServicePlacement(r.Context(), id, &placement)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	current := svc.Spec.UpdateConfig
	if current == nil {
		current = &swarm.UpdateConfig{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeErrorCode(
			w,
			r,
			"API009",
			"failed to marshal current update policy",
		)
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeErrorCode(
			w,
			r,
			"API009",
			"failed to unmarshal current update policy",
		)
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal merged update policy")
		return
	}
	var result swarm.UpdateConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeErrorCode(w, r, "SVC012", "invalid update policy specification")
		return
	}

	slog.Info("updating service update policy", "service", id)

	updated, err := h.writeClient.UpdateServiceUpdatePolicy(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	current := svc.Spec.RollbackConfig
	if current == nil {
		current = &swarm.UpdateConfig{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeErrorCode(
			w,
			r,
			"API009",
			"failed to marshal current rollback policy",
		)
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeErrorCode(
			w,
			r,
			"API009",
			"failed to unmarshal current rollback policy",
		)
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeErrorCode(
			w,
			r,
			"API009",
			"failed to marshal merged rollback policy",
		)
		return
	}
	var result swarm.UpdateConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeErrorCode(w, r, "SVC013", "invalid rollback policy specification")
		return
	}

	slog.Info("updating service rollback policy", "service", id)

	updated, err := h.writeClient.UpdateServiceRollbackPolicy(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
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
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	current := svc.Spec.TaskTemplate.LogDriver
	if current == nil {
		current = &swarm.Driver{}
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal current log driver")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeErrorCode(w, r, "API009", "failed to unmarshal current log driver")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal merged log driver")
		return
	}
	var result swarm.Driver
	if err := json.Unmarshal(merged, &result); err != nil {
		writeErrorCode(w, r, "SVC019", "invalid log driver specification")
		return
	}

	slog.Info("updating service log driver", "service", id)

	updated, err := h.writeClient.UpdateServiceLogDriver(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	current := &container.HealthConfig{}
	if svc.Spec.TaskTemplate.ContainerSpec != nil &&
		svc.Spec.TaskTemplate.ContainerSpec.Healthcheck != nil {
		current = svc.Spec.TaskTemplate.ContainerSpec.Healthcheck
	}

	base, err := json.Marshal(current)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal current healthcheck")
		return
	}

	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeErrorCode(
			w,
			r,
			"API009",
			"failed to unmarshal current healthcheck",
		)
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}

	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal merged healthcheck")
		return
	}

	var result container.HealthConfig
	if err := json.Unmarshal(merged, &result); err != nil {
		writeErrorCode(w, r, "SVC014", "invalid healthcheck specification")
		return
	}

	slog.Info("updating service healthcheck", "service", id)

	updated, err := h.writeClient.UpdateServiceHealthcheck(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
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
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	resp := containerConfigFromSpec(svc.Spec.TaskTemplate.ContainerSpec)
	writeJSON(w, resp)
}

func (h *Handlers) HandlePatchServiceContainerConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	current := containerConfigFromSpec(svc.Spec.TaskTemplate.ContainerSpec)
	baseBytes, err := json.Marshal(current)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal current config")
		return
	}
	var baseMap map[string]any
	if err := json.Unmarshal(baseBytes, &baseMap); err != nil {
		writeErrorCode(w, r, "API009", "failed to unmarshal current config")
		return
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	mergePatch(baseMap, patchMap)

	mergedBytes, err := json.Marshal(baseMap)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal merged config")
		return
	}
	var merged containerConfigResponse
	if err := json.Unmarshal(mergedBytes, &merged); err != nil {
		writeErrorCode(w, r, "SVC018", "invalid patch result")
		return
	}

	slog.Info("updating service container config", "service", id)

	updated, err := h.writeClient.UpdateServiceContainerConfig(
		r.Context(),
		id,
		func(cs *swarm.ContainerSpec) {
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
		},
	)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	result := containerConfigFromSpec(updated.Spec.TaskTemplate.ContainerSpec)
	writeJSON(w, result)
}

func extractConfigRefs(cs *swarm.ContainerSpec) []serviceConfigRef {
	if cs == nil {
		return []serviceConfigRef{}
	}
	refs := make([]serviceConfigRef, 0, len(cs.Configs))
	for _, cfg := range cs.Configs {
		var fileName string
		if cfg.File != nil {
			fileName = cfg.File.Name
		}
		refs = append(refs, serviceConfigRef{
			ConfigID:   cfg.ConfigID,
			ConfigName: cfg.ConfigName,
			FileName:   fileName,
		})
	}
	return refs
}

func extractSecretRefs(cs *swarm.ContainerSpec) []serviceSecretRef {
	if cs == nil {
		return []serviceSecretRef{}
	}
	refs := make([]serviceSecretRef, 0, len(cs.Secrets))
	for _, sec := range cs.Secrets {
		var fileName string
		if sec.File != nil {
			fileName = sec.File.Name
		}
		refs = append(refs, serviceSecretRef{
			SecretID:   sec.SecretID,
			SecretName: sec.SecretName,
			FileName:   fileName,
		})
	}
	return refs
}

func extractNetworkRefs(networks []swarm.NetworkAttachmentConfig) []serviceNetworkRef {
	refs := make([]serviceNetworkRef, 0, len(networks))
	for _, net := range networks {
		refs = append(refs, serviceNetworkRef{
			Target:  net.Target,
			Aliases: net.Aliases,
		})
	}
	return refs
}

func (h *Handlers) HandleGetServiceConfigs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/configs", "ServiceConfigs", map[string]any{
			"configs": extractConfigRefs(svc.Spec.TaskTemplate.ContainerSpec),
		}),
	)
}

func (h *Handlers) HandlePatchServiceConfigs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var patch struct {
		Configs []serviceConfigRef `json:"configs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	configs := make([]*swarm.ConfigReference, len(patch.Configs))
	for i, ref := range patch.Configs {
		if ref.ConfigID == "" || ref.ConfigName == "" {
			writeErrorCode(
				w,
				r,
				"SVC015",
				"each config must have configID and configName",
			)
			return
		}
		fileName := ref.FileName
		if fileName == "" {
			fileName = "/" + ref.ConfigName
		}
		configs[i] = &swarm.ConfigReference{
			ConfigID:   ref.ConfigID,
			ConfigName: ref.ConfigName,
			File: &swarm.ConfigReferenceFileTarget{
				Name: fileName,
				UID:  "0",
				GID:  "0",
				Mode: 0444,
			},
		}
	}

	slog.Info("updating service configs", "service", id)

	updated, err := h.writeClient.UpdateServiceConfigs(r.Context(), id, configs)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/configs", "ServiceConfigs", map[string]any{
		"configs": extractConfigRefs(updated.Spec.TaskTemplate.ContainerSpec),
	}))
}

func (h *Handlers) HandleGetServiceSecrets(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/secrets", "ServiceSecrets", map[string]any{
			"secrets": extractSecretRefs(svc.Spec.TaskTemplate.ContainerSpec),
		}),
	)
}

func (h *Handlers) HandlePatchServiceSecrets(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var patch struct {
		Secrets []serviceSecretRef `json:"secrets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	secrets := make([]*swarm.SecretReference, len(patch.Secrets))
	for i, ref := range patch.Secrets {
		if ref.SecretID == "" || ref.SecretName == "" {
			writeErrorCode(
				w,
				r,
				"SVC016",
				"each secret must have secretID and secretName",
			)
			return
		}
		fileName := ref.FileName
		if fileName == "" {
			fileName = "/run/secrets/" + ref.SecretName
		}
		secrets[i] = &swarm.SecretReference{
			SecretID:   ref.SecretID,
			SecretName: ref.SecretName,
			File: &swarm.SecretReferenceFileTarget{
				Name: fileName,
				UID:  "0",
				GID:  "0",
				Mode: 0444,
			},
		}
	}

	slog.Info("updating service secrets", "service", id)

	updated, err := h.writeClient.UpdateServiceSecrets(r.Context(), id, secrets)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/secrets", "ServiceSecrets", map[string]any{
		"secrets": extractSecretRefs(updated.Spec.TaskTemplate.ContainerSpec),
	}))
}

func (h *Handlers) HandleGetServiceNetworks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/networks", "ServiceNetworks", map[string]any{
			"networks": extractNetworkRefs(svc.Spec.TaskTemplate.Networks),
		}),
	)
}

func (h *Handlers) HandlePatchServiceNetworks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(
			w,
			r,
			"API004",
			"expected Content-Type: application/merge-patch+json",
		)
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var patch struct {
		Networks []serviceNetworkRef `json:"networks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return
	}

	networks := make([]swarm.NetworkAttachmentConfig, len(patch.Networks))
	for i, ref := range patch.Networks {
		if ref.Target == "" {
			writeErrorCode(w, r, "SVC017", "each network must have a target")
			return
		}
		networks[i] = swarm.NetworkAttachmentConfig{
			Target:  ref.Target,
			Aliases: ref.Aliases,
		}
	}

	slog.Info("updating service networks", "service", id)

	updated, err := h.writeClient.UpdateServiceNetworks(r.Context(), id, networks)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/networks", "ServiceNetworks", map[string]any{
		"networks": extractNetworkRefs(updated.Spec.TaskTemplate.Networks),
	}))
}

func (h *Handlers) HandleGetServiceMounts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var mounts []mount.Mount
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		mounts = svc.Spec.TaskTemplate.ContainerSpec.Mounts
	}
	if mounts == nil {
		mounts = []mount.Mount{}
	}

	writeJSONWithETag(
		w,
		r,
		NewDetailResponse("/services/"+id+"/mounts", "ServiceMounts", map[string]any{
			"mounts": mounts,
		}),
	)
}

func (h *Handlers) HandlePatchServiceMounts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(
			w,
			r,
			"API004",
			"Content-Type must be application/merge-patch+json",
		)
		return
	}

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	var req struct {
		Mounts []mount.Mount `json:"mounts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	slog.Info("updating service mounts", "service", id, "count", len(req.Mounts))

	updated, err := h.writeClient.UpdateServiceMounts(r.Context(), id, req.Mounts)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	var mounts []mount.Mount
	if updated.Spec.TaskTemplate.ContainerSpec != nil {
		mounts = updated.Spec.TaskTemplate.ContainerSpec.Mounts
	}
	if mounts == nil {
		mounts = []mount.Mount{}
	}

	writeJSON(w, NewDetailResponse("/services/"+id+"/mounts", "ServiceMounts", map[string]any{
		"mounts": mounts,
	}))
}

func (h *Handlers) HandlePatchSwarmOrchestration(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(w, r, "API004",
			"Content-Type must be application/merge-patch+json")
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	var patch swarm.OrchestrationConfig
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	spec := current.Spec
	if patch.TaskHistoryRetentionLimit != nil {
		spec.Orchestration.TaskHistoryRetentionLimit = patch.TaskHistoryRetentionLimit
	}

	slog.Info("updating swarm orchestration config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"orchestration": spec.Orchestration})
}

func (h *Handlers) HandlePatchSwarmRaft(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(w, r, "API004",
			"Content-Type must be application/merge-patch+json")
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	var patch swarm.RaftConfig
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	spec := current.Spec
	if patch.SnapshotInterval != 0 {
		spec.Raft.SnapshotInterval = patch.SnapshotInterval
	}
	if patch.KeepOldSnapshots != nil {
		spec.Raft.KeepOldSnapshots = patch.KeepOldSnapshots
	}
	if patch.LogEntriesForSlowFollowers != 0 {
		spec.Raft.LogEntriesForSlowFollowers = patch.LogEntriesForSlowFollowers
	}

	slog.Info("updating swarm raft config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"raft": spec.Raft})
}

func (h *Handlers) HandlePatchSwarmDispatcher(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(w, r, "API004",
			"Content-Type must be application/merge-patch+json")
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	var patch swarm.DispatcherConfig
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	spec := current.Spec
	if patch.HeartbeatPeriod != 0 {
		spec.Dispatcher.HeartbeatPeriod = patch.HeartbeatPeriod
	}

	slog.Info("updating swarm dispatcher config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"dispatcher": spec.Dispatcher})
}

func (h *Handlers) HandlePatchSwarmCAConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(w, r, "API004",
			"Content-Type must be application/merge-patch+json")
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	var patch swarm.CAConfig
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	spec := current.Spec
	if patch.NodeCertExpiry != 0 {
		spec.CAConfig.NodeCertExpiry = patch.NodeCertExpiry
	}

	slog.Info("updating swarm CA config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"caConfig": spec.CAConfig})
}

func (h *Handlers) HandlePatchSwarmEncryption(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/merge-patch+json") {
		writeErrorCode(w, r, "API004",
			"Content-Type must be application/merge-patch+json")
		return
	}

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	var patch struct {
		AutoLockManagers *bool `json:"AutoLockManagers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	spec := current.Spec
	if patch.AutoLockManagers != nil {
		spec.EncryptionConfig.AutoLockManagers = *patch.AutoLockManagers
	}

	slog.Info("updating swarm encryption config")

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(w, r, "SWM003", "failed to update swarm: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"encryption": spec.EncryptionConfig})
}

func (h *Handlers) HandlePostRotateToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return
	}

	var flags swarm.UpdateFlags
	switch req.Target {
	case "worker":
		flags.RotateWorkerToken = true
	case "manager":
		flags.RotateManagerToken = true
	default:
		writeErrorCode(w, r, "SWM011", "target must be \"worker\" or \"manager\"")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	slog.Info("rotating swarm join token", "target", req.Target)

	if err := h.systemClient.UpdateSwarm(ctx, current.Spec, current.Version, flags); err != nil {
		writeErrorCode(w, r, "SWM006", "failed to rotate token: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandlePostRotateUnlockKey(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	slog.Info("rotating swarm unlock key")

	flags := swarm.UpdateFlags{RotateManagerUnlockKey: true}
	if err := h.systemClient.UpdateSwarm(ctx, current.Spec, current.Version, flags); err != nil {
		writeErrorCode(
			w,
			r,
			"SWM007",
			"failed to rotate unlock key: "+err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandlePostForceRotateCA(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	current, err := h.systemClient.SwarmInspect(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM002", "failed to inspect swarm")
		return
	}

	spec := current.Spec
	spec.CAConfig.ForceRotate++

	slog.Info("forcing CA certificate rotation", "forceRotate", spec.CAConfig.ForceRotate)

	if err := h.systemClient.UpdateSwarm(
		ctx,
		spec,
		current.Version,
		swarm.UpdateFlags{},
	); err != nil {
		writeErrorCode(
			w,
			r,
			"SWM003",
			"failed to force CA rotation: "+err.Error(),
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) HandleGetUnlockKey(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	key, err := h.systemClient.GetUnlockKey(ctx)
	if err != nil {
		writeErrorCode(w, r, "SWM009", "failed to get unlock key: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"unlockKey": key})
}

func (h *Handlers) HandlePostUnlockSwarm(w http.ResponseWriter, r *http.Request) {
	if h.systemClient == nil {
		writeErrorCode(w, r, "SWM001", "swarm API not available")
		return
	}

	var body struct {
		UnlockKey string `json:"unlockKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body: "+err.Error())
		return
	}

	if body.UnlockKey == "" {
		writeErrorCode(w, r, "SWM010", "unlockKey is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	slog.Info("unlocking swarm")

	if err := h.systemClient.UnlockSwarm(ctx, body.UnlockKey); err != nil {
		writeErrorCode(w, r, "SWM008", "failed to unlock swarm: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
