package api

import (
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
)

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

	writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.ScaleService(r.Context(), id, *req.Replicas)
	})
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

	writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.UpdateServiceMode(r.Context(), id, mode)
	})
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

	writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.UpdateServiceEndpointMode(r.Context(), id, mode)
	})
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

	writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.UpdateServiceImage(r.Context(), id, req.Image)
	})
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

	writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.RollbackService(r.Context(), id)
	})
}

func (h *Handlers) HandleRemoveService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	slog.Info("removing service", "service", id)

	err := h.serviceLifecycle.RemoveService(r.Context(), id)
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
		if err := h.serviceLifecycle.RemoveService(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "service", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Services++
	}

	for _, id := range stack.Networks {
		if err := h.resourceRemover.RemoveNetwork(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "network", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Networks++
	}

	for _, id := range stack.Secrets {
		if err := h.secretWriter.RemoveSecret(ctx, id); err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, removeError{Type: "secret", ID: id, Error: err.Error()})
			continue
		}
		resp.Removed.Secrets++
	}

	for _, id := range stack.Configs {
		if err := h.configWriter.RemoveConfig(ctx, id); err != nil {
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

	writeMutationResponse(w, r, resp)
}

func (h *Handlers) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	_, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	slog.Info("restarting service", "service", id)

	writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.RestartService(r.Context(), id)
	})
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
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/env", "ServiceEnv", EnvResponse{
			Env: envSliceToMap(env),
		}),
	)
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

	result, err := h.serviceSpec.UpdateServiceEnv(r.Context(), id, updated)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	var resultEnv []string
	if result.Spec.TaskTemplate.ContainerSpec != nil {
		resultEnv = result.Spec.TaskTemplate.ContainerSpec.Env
	}
	writeMutationResponse(w, r, envSliceToMap(resultEnv))
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
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/labels", "ServiceLabels", LabelsResponse{
			Labels: labels,
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

	result, err := h.serviceSpec.UpdateServiceLabels(r.Context(), id, updated)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	labels := result.Spec.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	writeMutationResponse(w, r, labels)
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
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/services/"+id+"/resources",
			"ServiceResources",
			map[string]any{
				"resources": resources,
			},
		),
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

	var result swarm.ResourceRequirements
	if !applyStructMergePatch(w, r, current, &result, "SVC011", "invalid resource specification") {
		return
	}

	slog.Info("updating service resources", "service", id)
	updated, err := h.serviceSpec.UpdateServiceResources(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/resources",
		"ServiceResources",
		map[string]any{
			"resources": updated.Spec.TaskTemplate.Resources,
		},
	))
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
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/ports", "ServicePorts", map[string]any{
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

	updated, err := h.serviceSpec.UpdateServicePorts(r.Context(), id, patch.Ports)
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
	writeMutationResponse(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/ports", "ServicePorts", map[string]any{
			"ports": resultPorts,
		}),
	)
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

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/services/"+id+"/healthcheck",
			"ServiceHealthcheck",
			map[string]any{
				"healthcheck": hc,
			},
		),
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

	updated, err := h.serviceSpec.UpdateServiceHealthcheck(r.Context(), id, &hc)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	var resultHC *container.HealthConfig
	if updated.Spec.TaskTemplate.ContainerSpec != nil {
		resultHC = updated.Spec.TaskTemplate.ContainerSpec.Healthcheck
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/healthcheck",
		"ServiceHealthcheck",
		map[string]any{
			"healthcheck": resultHC,
		},
	))
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

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/services/"+id+"/placement",
			"ServicePlacement",
			map[string]any{
				"placement": placement,
			},
		),
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

	updated, err := h.serviceSpec.UpdateServicePlacement(r.Context(), id, &placement)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	resultPlacement := updated.Spec.TaskTemplate.Placement
	if resultPlacement == nil {
		resultPlacement = &swarm.Placement{}
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/placement",
		"ServicePlacement",
		map[string]any{
			"placement": resultPlacement,
		},
	))
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
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/services/"+id+"/update-policy",
			"ServiceUpdatePolicy",
			map[string]any{
				"updatePolicy": policy,
			},
		),
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

	var result swarm.UpdateConfig
	if !applyStructMergePatch(
		w,
		r,
		current,
		&result,
		"SVC012",
		"invalid update policy specification",
	) {
		return
	}

	slog.Info("updating service update policy", "service", id)

	updated, err := h.serviceSpec.UpdateServiceUpdatePolicy(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	resultPolicy := updated.Spec.UpdateConfig
	if resultPolicy == nil {
		resultPolicy = &swarm.UpdateConfig{}
	}
	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/update-policy",
		"ServiceUpdatePolicy",
		map[string]any{
			"updatePolicy": resultPolicy,
		},
	))
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
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(),
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

	var result swarm.UpdateConfig
	if !applyStructMergePatch(
		w,
		r,
		current,
		&result,
		"SVC013",
		"invalid rollback policy specification",
	) {
		return
	}

	slog.Info("updating service rollback policy", "service", id)

	updated, err := h.serviceSpec.UpdateServiceRollbackPolicy(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	resultPolicy := updated.Spec.RollbackConfig
	if resultPolicy == nil {
		resultPolicy = &swarm.UpdateConfig{}
	}
	writeMutationResponse(w, r, NewDetailResponse(r.Context(),
		"/services/"+id+"/rollback-policy",
		"ServiceRollbackPolicy",
		map[string]any{
			"rollbackPolicy": resultPolicy,
		},
	))
}

func (h *Handlers) HandleGetServiceLogDriver(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/services/"+id+"/log-driver",
			"ServiceLogDriver",
			map[string]any{
				"logDriver": svc.Spec.TaskTemplate.LogDriver,
			},
		),
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

	var result swarm.Driver
	if !applyStructMergePatch(
		w,
		r,
		current,
		&result,
		"SVC019",
		"invalid log driver specification",
	) {
		return
	}

	slog.Info("updating service log driver", "service", id)

	updated, err := h.serviceSpec.UpdateServiceLogDriver(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/log-driver",
		"ServiceLogDriver",
		map[string]any{
			"logDriver": updated.Spec.TaskTemplate.LogDriver,
		},
	))
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

	var result container.HealthConfig
	if !applyStructMergePatch(
		w,
		r,
		current,
		&result,
		"SVC014",
		"invalid healthcheck specification",
	) {
		return
	}

	slog.Info("updating service healthcheck", "service", id)

	updated, err := h.serviceSpec.UpdateServiceHealthcheck(r.Context(), id, &result)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	var resultHC *container.HealthConfig
	if updated.Spec.TaskTemplate.ContainerSpec != nil {
		resultHC = updated.Spec.TaskTemplate.ContainerSpec.Healthcheck
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/healthcheck",
		"ServiceHealthcheck",
		map[string]any{
			"healthcheck": resultHC,
		},
	))
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

	var merged containerConfigResponse
	if !applyStructMergePatch(w, r, current, &merged, "SVC018", "invalid patch result") {
		return
	}

	slog.Info("updating service container config", "service", id)

	updated, err := h.serviceAttachment.UpdateServiceContainerConfig(
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
	writeMutationResponse(w, r, result)
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

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/configs", "ServiceConfigs", map[string]any{
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

	updated, err := h.serviceAttachment.UpdateServiceConfigs(r.Context(), id, configs)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeMutationResponse(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/configs", "ServiceConfigs", map[string]any{
			"configs": extractConfigRefs(updated.Spec.TaskTemplate.ContainerSpec),
		}),
	)
}

func (h *Handlers) HandleGetServiceSecrets(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/secrets", "ServiceSecrets", map[string]any{
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

	updated, err := h.serviceAttachment.UpdateServiceSecrets(r.Context(), id, secrets)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeMutationResponse(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/secrets", "ServiceSecrets", map[string]any{
			"secrets": extractSecretRefs(updated.Spec.TaskTemplate.ContainerSpec),
		}),
	)
}

func (h *Handlers) HandleGetServiceNetworks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	svc, ok := h.cache.GetService(id)
	if !ok {
		writeErrorCode(w, r, "SVC003", "service not found")
		return
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/services/"+id+"/networks",
			"ServiceNetworks",
			map[string]any{
				"networks": extractNetworkRefs(svc.Spec.TaskTemplate.Networks),
			},
		),
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

	updated, err := h.serviceAttachment.UpdateServiceNetworks(r.Context(), id, networks)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/networks",
		"ServiceNetworks",
		map[string]any{
			"networks": extractNetworkRefs(updated.Spec.TaskTemplate.Networks),
		},
	))
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

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/mounts", "ServiceMounts", map[string]any{
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

	updated, err := h.serviceAttachment.UpdateServiceMounts(r.Context(), id, req.Mounts)
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

	writeMutationResponse(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id+"/mounts", "ServiceMounts", map[string]any{
			"mounts": mounts,
		}),
	)
}
