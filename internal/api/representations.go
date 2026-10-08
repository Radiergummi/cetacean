package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/integrations"
)

// errNoRepresentation reports that the resource has no current representation,
// as distinct from one that could not be determined. Only the first is a
// precondition failure; a backend that could not be reached leaves the
// condition unevaluable, and saying "your validator is stale" would be a lie.
var errNoRepresentation = errors.New("api: no current representation")

// representationFunc builds the value a GET at this URI would serialize, so a
// precondition compares against the exact ETag that GET emits. It returns
// errNoRepresentation when the resource is absent and must never write to the
// response, which is why a paired handler looks its resource up twice.
type representationFunc func(*http.Request) (any, error)

// writeServiceRepresentation is the tail every service sub-resource GET shares:
// build the representation, answer 404 if the service vanished since the ACL
// check, and write it with its ETag.
func (h *Handlers) writeServiceRepresentation(
	w http.ResponseWriter,
	r *http.Request,
	serviceID string,
	rep representationFunc,
) {
	value, ok := representationOr404(w, r, "service", serviceID, rep)
	if !ok {
		return
	}

	writeCachedJSON(w, r, value)
}

// representationOr404 evaluates a paired GET's representation builder and, when
// the resource is gone, answers with that resource's own not-found code. The
// builder cannot write the 404 itself without breaking the 412-vs-404 rule.
func representationOr404(
	w http.ResponseWriter,
	r *http.Request,
	resource, key string,
	rep representationFunc,
) (any, bool) {
	value, err := rep(r)
	switch {
	case errors.Is(err, errNoRepresentation):
		writeErrorCode(
			w,
			r,
			notFoundCodes[resource],
			fmt.Sprintf("%s %q not found", resource, key),
		)

		return nil, false
	case err != nil:
		writeDockerError(w, r, err, resource, key)

		return nil, false
	}

	return value, true
}

// serviceSubResource builds the representation of a service sub-resource:
// the shared lookup, the shared `/services/<id><suffix>` identity, and the
// JSON-LD wrapper the GET emits. body renders the part that differs.
func (h *Handlers) serviceSubResource(
	r *http.Request,
	suffix string,
	typeName string,
	body func(swarm.Service) any,
) (any, error) {
	svc, ok := h.cache.GetService(r.PathValue("id"))
	if !ok {
		return nil, errNoRepresentation
	}

	return NewDetailResponse(
		r.Context(),
		"/services/"+svc.ID+suffix,
		typeName,
		body(svc),
	), nil
}

// --- Service sub-resources ---

func (h *Handlers) serviceEnvRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/env", "ServiceEnv", func(svc swarm.Service) any {
		var env []string
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			env = svc.Spec.TaskTemplate.ContainerSpec.Env
		}

		return EnvResponse{Env: envSliceToMap(env)}
	})
}

func (h *Handlers) serviceResourcesRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/resources", "ServiceResources", func(svc swarm.Service) any {
		resources := svc.Spec.TaskTemplate.Resources
		if resources == nil {
			resources = &swarm.ResourceRequirements{}
		}

		return map[string]any{"resources": resources}
	})
}

func (h *Handlers) servicePortsRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/ports", "ServicePorts", func(svc swarm.Service) any {
		var ports []swarm.PortConfig
		if svc.Spec.EndpointSpec != nil {
			ports = svc.Spec.EndpointSpec.Ports
		}
		if ports == nil {
			ports = []swarm.PortConfig{}
		}

		return map[string]any{"ports": ports}
	})
}

func (h *Handlers) serviceHealthcheckRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(
		r,
		"/healthcheck",
		"ServiceHealthcheck",
		func(svc swarm.Service) any {
			var hc *container.HealthConfig
			if svc.Spec.TaskTemplate.ContainerSpec != nil {
				hc = svc.Spec.TaskTemplate.ContainerSpec.Healthcheck
			}

			return map[string]any{"healthcheck": hc}
		},
	)
}

func (h *Handlers) servicePlacementRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/placement", "ServicePlacement", func(svc swarm.Service) any {
		placement := svc.Spec.TaskTemplate.Placement
		if placement == nil {
			placement = &swarm.Placement{}
		}

		return map[string]any{"placement": placement}
	})
}

func (h *Handlers) serviceUpdatePolicyRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(
		r,
		"/update-policy",
		"ServiceUpdatePolicy",
		func(svc swarm.Service) any {
			policy := svc.Spec.UpdateConfig
			if policy == nil {
				policy = &swarm.UpdateConfig{}
			}

			return map[string]any{"updatePolicy": policy}
		},
	)
}

func (h *Handlers) serviceRollbackPolicyRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(
		r,
		"/rollback-policy",
		"ServiceRollbackPolicy",
		func(svc swarm.Service) any {
			policy := svc.Spec.RollbackConfig
			if policy == nil {
				policy = &swarm.UpdateConfig{}
			}

			return map[string]any{"rollbackPolicy": policy}
		},
	)
}

func (h *Handlers) serviceLogDriverRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/log-driver", "ServiceLogDriver", func(svc swarm.Service) any {
		return map[string]any{"logDriver": svc.Spec.TaskTemplate.LogDriver}
	})
}

func (h *Handlers) serviceContainerConfigRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(
		r,
		"/container-config",
		"ServiceContainerConfig",
		func(svc swarm.Service) any {
			return map[string]any{
				"containerConfig": containerConfigFromSpec(svc.Spec.TaskTemplate.ContainerSpec),
			}
		},
	)
}

func (h *Handlers) serviceConfigsRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/configs", "ServiceConfigs", func(svc swarm.Service) any {
		return map[string]any{
			"configs": extractConfigRefs(svc.Spec.TaskTemplate.ContainerSpec),
		}
	})
}

func (h *Handlers) serviceSecretsRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/secrets", "ServiceSecrets", func(svc swarm.Service) any {
		return map[string]any{
			"secrets": extractSecretRefs(svc.Spec.TaskTemplate.ContainerSpec),
		}
	})
}

func (h *Handlers) serviceNetworksRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/networks", "ServiceNetworks", func(svc swarm.Service) any {
		return map[string]any{
			"networks": extractNetworkRefs(svc.Spec.TaskTemplate.Networks),
		}
	})
}

func (h *Handlers) serviceMountsRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/mounts", "ServiceMounts", func(svc swarm.Service) any {
		var mounts []mount.Mount
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			mounts = svc.Spec.TaskTemplate.ContainerSpec.Mounts
		}
		if mounts == nil {
			mounts = []mount.Mount{}
		}

		return map[string]any{"mounts": mounts}
	})
}

func (h *Handlers) serviceModeRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(r, "/mode", "ServiceMode", func(svc swarm.Service) any {
		mode := "replicated"
		var replicas *uint64
		if svc.Spec.Mode.Global != nil {
			mode = "global"
		} else if svc.Spec.Mode.Replicated != nil {
			replicas = svc.Spec.Mode.Replicated.Replicas
		}

		return map[string]any{
			"mode":     mode,
			"replicas": replicas,
		}
	})
}

func (h *Handlers) serviceEndpointModeRepresentation(r *http.Request) (any, error) {
	return h.serviceSubResource(
		r,
		"/endpoint-mode",
		"ServiceEndpointMode",
		func(svc swarm.Service) any {
			endpointMode := ""
			if svc.Spec.EndpointSpec != nil {
				endpointMode = string(svc.Spec.EndpointSpec.Mode)
			}

			return map[string]any{"endpointMode": endpointMode}
		},
	)
}

// --- Node sub-resources ---

func (h *Handlers) nodeRoleRepresentation(r *http.Request) (any, error) {
	node, ok := h.cache.GetNode(r.PathValue("id"))
	if !ok {
		return nil, errNoRepresentation
	}

	return NewDetailResponse(
		r.Context(),
		r.URL.Path,
		"NodeRole",
		NodeRoleResponse{
			Role:         string(node.Spec.Role),
			IsLeader:     node.ManagerStatus != nil && node.ManagerStatus.Leader,
			ManagerCount: h.managerCount(),
		},
	), nil
}

// managerCount counts the manager nodes in the cluster, the peer context the
// node role representation reports alongside the role itself.
func (h *Handlers) managerCount() int {
	count := 0
	for _, n := range h.cache.ListNodes() {
		if n.Spec.Role == swarm.NodeRoleManager {
			count++
		}
	}

	return count
}

// --- Resource roots ---

func (h *Handlers) serviceRepresentation(r *http.Request) (any, error) {
	id := r.PathValue("id")

	svc, ok := h.cache.GetService(id)
	if !ok {
		return nil, errNoRepresentation
	}

	detail := ServiceResponse{Service: svc}
	if changes := DiffServiceSpecs(svc.PreviousSpec, &svc.Spec); len(changes) > 0 {
		detail.Changes = changes
	}
	if detected := integrations.Detect(svc.Spec.Labels); len(detected) > 0 {
		detail.Integrations = detected
	}

	return NewDetailResponse(r.Context(), "/services/"+id, "Service", detail), nil
}

func (h *Handlers) nodeRepresentation(r *http.Request) (any, error) {
	id := r.PathValue("id")

	node, ok := h.cache.GetNode(id)
	if !ok {
		return nil, errNoRepresentation
	}

	return NewDetailResponse(r.Context(), "/nodes/"+id, "Node", NodeResponse{
		Node: node,
	}), nil
}

func (h *Handlers) configRepresentation(r *http.Request) (any, error) {
	id := r.PathValue("id")

	cfg, ok := h.cache.GetConfig(id)
	if !ok {
		return nil, errNoRepresentation
	}

	return NewDetailResponse(r.Context(), "/configs/"+id, "Config", ConfigResponse{
		Config:   cfg,
		Services: h.filterServiceRefs(r, h.cache.ServicesUsingConfig(id)),
	}), nil
}

func (h *Handlers) secretRepresentation(r *http.Request) (any, error) {
	id := r.PathValue("id")

	sec, ok := h.cache.GetSecret(id)
	if !ok {
		return nil, errNoRepresentation
	}

	// Never expose secret data — clear it before responding.
	sec = cluster.RedactSecret(sec)

	return NewDetailResponse(r.Context(), "/secrets/"+id, "Secret", SecretResponse{
		Secret:   sec,
		Services: h.filterServiceRefs(r, h.cache.ServicesUsingSecret(id)),
	}), nil
}

func (h *Handlers) networkRepresentation(r *http.Request) (any, error) {
	id := r.PathValue("id")

	net, ok := h.cache.GetNetwork(id)
	if !ok {
		return nil, errNoRepresentation
	}

	return NewDetailResponse(r.Context(), "/networks/"+id, "Network", NetworkResponse{
		Network:  net,
		Services: h.filterServiceRefs(r, h.cache.ServicesUsingNetwork(id)),
	}), nil
}

func (h *Handlers) volumeRepresentation(r *http.Request) (any, error) {
	name := r.PathValue("name")

	vol, ok := h.cache.GetVolume(name)
	if !ok {
		return nil, errNoRepresentation
	}

	return NewDetailResponse(r.Context(), "/volumes/"+name, "Volume", VolumeResponse{
		Volume:   vol,
		Services: h.filterServiceRefs(r, h.cache.ServicesUsingVolume(name)),
	}), nil
}

func (h *Handlers) taskRepresentation(r *http.Request) (any, error) {
	id := r.PathValue("id")

	task, ok := h.cache.GetTask(id)
	if !ok {
		return nil, errNoRepresentation
	}

	et := cluster.EnrichTask(h.cache, task)

	return NewDetailResponse(r.Context(), "/tasks/"+id, "Task", TaskResponse{
		Task: et,
		Service: TaskServiceRef{
			AtID: absPath(r.Context(), "/services/"+et.ServiceID),
			Name: et.ServiceName,
		},
		Node: TaskNodeRef{
			AtID:     absPath(r.Context(), "/nodes/"+et.NodeID),
			Hostname: et.NodeHostname,
		},
	}), nil
}

func (h *Handlers) stackRepresentation(r *http.Request) (any, error) {
	name := r.PathValue("name")

	detail, ok := h.cache.GetStackDetail(name)
	if !ok {
		return nil, errNoRepresentation
	}

	return NewDetailResponse(r.Context(), "/stacks/"+name, "Stack", StackResponse{
		Stack: detail,
	}), nil
}

// pluginDetail is the body both the plugin GET and its precondition
// representation serialize. They share the rendering rather than the lookup,
// because only the GET maps an inspect error to a status code.
func pluginDetail(r *http.Request, name string, plugin types.Plugin) DetailResponse {
	return NewDetailResponse(r.Context(), "/plugins/"+name, "Plugin", PluginResponse{
		Plugin: plugin,
	})
}

// pluginRepresentation is the one builder that does not read the cache: plugins
// are inspected from the daemon on demand, as the GET does. Only a not-found
// inspect is "no current representation" — every other failure is reported as
// itself, so a daemon blip cannot be mistaken for a stale validator.
func (h *Handlers) pluginRepresentation(r *http.Request) (any, error) {
	name := r.PathValue("name")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	plugin, err := h.pluginClient.PluginInspect(ctx, name)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return nil, errNoRepresentation
		}

		return nil, err
	}

	return pluginDetail(r, name, *plugin), nil
}
