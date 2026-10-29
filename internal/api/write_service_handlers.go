package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/cluster"
)

func (h *Handlers) HandleScaleService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := decodeJSON[scaleRequest](w, r)
	if !ok {
		return
	}
	if req.Replicas == nil {
		problem.Write(w, r, "SVC004", "replicas is required")
		return
	}

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.writes.ScaleService(r.Context(), h.caller(r), id, *req.Replicas)
	})
}

func (h *Handlers) HandleGetServiceMode(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.writeServiceRepresentation(w, r, svc, h.serviceModeRepresentation)
}

func (h *Handlers) HandleGetServiceEndpointMode(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.writeServiceRepresentation(w, r, svc, h.serviceEndpointModeRepresentation)
}

type updateEndpointModeRequest struct {
	Mode string `json:"mode"`
}

func (h *Handlers) HandleUpdateServiceEndpointMode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := decodeJSON[updateEndpointModeRequest](w, r)
	if !ok {
		return
	}

	var mode swarm.ResolutionMode
	switch req.Mode {
	case "vip":
		mode = swarm.ResolutionModeVIP
	case "dnsrr":
		mode = swarm.ResolutionModeDNSRR
	default:
		problem.Write(w, r, "SVC010", "mode must be one of: vip, dnsrr")
		return
	}

	if _, ok := lookupOr404(w, r, "service", id, h.cache.GetService); !ok {
		return
	}

	slog.InfoContext(r.Context(), "updating service endpoint mode", "service", id, "mode", req.Mode)

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.writer.UpdateServiceSpec(
			r.Context(),
			id,
			h.writes.CheckedChange(h.caller(r), cluster.SetEndpointMode(mode)),
		)
	})
}

func (h *Handlers) HandleUpdateServiceImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := decodeJSON[updateImageRequest](w, r)
	if !ok {
		return
	}
	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.writes.UpdateServiceImage(r.Context(), h.caller(r), id, req.Image)
	})
}

func (h *Handlers) HandleRollbackService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.writes.RollbackService(r.Context(), h.caller(r), id)
	})
}

func (h *Handlers) HandleRemoveService(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Service]{
		resource: "service",
		getter:   h.cache.GetService,
		remove: func(ctx context.Context, key string) error {
			return h.writes.RemoveService(ctx, h.caller(r), key)
		},
		conflictCode: "SVC002",
	})
}

func (h *Handlers) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.writes.RestartService(r.Context(), h.caller(r), id)
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
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceEnvRepresentation)
}

func (h *Handlers) HandlePatchServiceEnv(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, ok := lookupOr404(w, r, "service", id, h.cache.GetService); !ok {
		return
	}

	mutate, ok := parsePatchMutator(w, r)
	if !ok {
		return
	}

	result, err := h.updateService(r, cluster.OpUpdateService, id, cluster.PatchEnv(mutate))
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(
		w,
		r,
		NewDetailResponse(r.Context(), r.URL.Path, "ServiceEnv", serviceEnvBody(result)),
	)
}

// updateService performs a section write as the request's caller.
func (h *Handlers) updateService(
	r *http.Request,
	op cluster.Operation,
	id string,
	change cluster.SpecChange,
) (swarm.Service, error) {
	return h.writes.UpdateService(r.Context(), h.caller(r), op, id, change)
}

func (h *Handlers) serviceLabelsSpec() getLabelsSpec[swarm.Service] {
	return getLabelsSpec[swarm.Service]{
		resource:    "service",
		typeName:    "ServiceLabels",
		getter:      h.cache.GetService,
		aclResource: func(s swarm.Service) string { return "service:" + s.Spec.Name },
		getLabels:   func(s swarm.Service) map[string]string { return s.Spec.Labels },
	}
}

func (h *Handlers) HandleGetServiceLabels(w http.ResponseWriter, r *http.Request) {
	handleGetLabels(w, r, h, h.serviceLabelsSpec())
}

func (h *Handlers) HandlePatchServiceLabels(w http.ResponseWriter, r *http.Request) {
	handlePatchLabels(w, r, h.acl, patchLabelsSpec[swarm.Service]{
		getLabelsSpec: h.serviceLabelsSpec(),
		update: func(
			_ context.Context,
			id string,
			mutate func(map[string]string) (map[string]string, error),
		) (swarm.Service, error) {
			return h.updateService(r, cluster.OpUpdateService, id, cluster.PatchLabels(mutate))
		},
		conflictCode: "SVC001",
	})
}

func (h *Handlers) HandleGetServiceResources(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.writeServiceRepresentation(w, r, svc, h.serviceResourcesRepresentation)
}

func (h *Handlers) HandlePatchServiceResources(w http.ResponseWriter, r *http.Request) {
	patchServiceSpecField(h, w, r, serviceSpecFieldPatch[swarm.ResourceRequirements]{
		errCode:  "SVC011",
		errMsg:   "invalid resource specification",
		suffix:   "/resources",
		typeName: "ServiceResources",
		field: func(spec *swarm.ServiceSpec) (**swarm.ResourceRequirements, error) {
			return &spec.TaskTemplate.Resources, nil
		},
		body: serviceResourcesBody,
	})
}

// serviceSpecFieldPatch describes a struct merge patch of one service spec
// field: field locates it in the live spec, or refuses with a specPatchError.
type serviceSpecFieldPatch[T any] struct {
	errCode  string
	errMsg   string
	suffix   string
	typeName string
	field    func(*swarm.ServiceSpec) (**T, error)
	body     func(swarm.Service) any
}

// patchServiceSpecField merges a merge-patch body into one spec field, an
// unset field counting as its zero value, and answers with the field's body.
func patchServiceSpecField[T any](
	h *Handlers,
	w http.ResponseWriter,
	r *http.Request,
	patch serviceSpecFieldPatch[T],
) {
	id := r.PathValue("id")

	if _, ok := lookupOr404(w, r, "service", id, h.cache.GetService); !ok {
		return
	}

	merge, ok := structMergePatch(w, r, patch.errCode, patch.errMsg)
	if !ok {
		return
	}

	updated, err := h.updateService(
		r,
		cluster.OpUpdateService,
		id,
		func(spec *swarm.ServiceSpec) error {
			field, err := patch.field(spec)
			if err != nil {
				return err
			}

			current := *field
			if current == nil {
				current = new(T)
			}

			var result T
			if err := merge(current, &result); err != nil {
				return err
			}

			*field = &result

			return nil
		},
	)
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+patch.suffix,
		patch.typeName,
		patch.body(updated),
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
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.writeServiceRepresentation(w, r, svc, h.servicePortsRepresentation)
}

func (h *Handlers) HandlePatchServicePorts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !requireMergePatch(w, r) {
		return
	}

	updated, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	ports, present, ok := decodeListMergePatch[swarm.PortConfig](w, r, "ports")
	if !ok {
		return
	}

	if present {
		if err := cluster.ValidatePorts(ports); err != nil {
			problem.Write(w, r, "SVC020", err.Error())
			return
		}

		var err error
		updated, err = h.updateService(r, cluster.OpUpdateService, id, cluster.SetPorts(ports))
		if err != nil {
			writeServiceSectionError(w, r, id, err)
			return
		}
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/ports",
		"ServicePorts",
		servicePortsBody(updated),
	))
}

func (h *Handlers) HandleGetServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceHealthcheckRepresentation)
}

func (h *Handlers) HandlePutServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, ok := lookupOr404(w, r, "service", id, h.cache.GetService); !ok {
		return
	}

	hc, ok := decodeJSON[container.HealthConfig](w, r)
	if !ok {
		return
	}

	updated, err := h.updateService(r, cluster.OpUpdateService, id, cluster.SetHealthcheck(&hc))
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/healthcheck",
		"ServiceHealthcheck",
		serviceHealthcheckBody(updated),
	))
}

func (h *Handlers) HandleGetServicePlacement(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.servicePlacementRepresentation)
}

func (h *Handlers) HandlePutServicePlacement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, ok := lookupOr404(w, r, "service", id, h.cache.GetService); !ok {
		return
	}

	placement, ok := decodeJSON[swarm.Placement](w, r)
	if !ok {
		return
	}

	updated, err := h.updateService(
		r,
		cluster.OpUpdateService,
		id,
		cluster.SetPlacement(&placement),
	)
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/placement",
		"ServicePlacement",
		servicePlacementBody(updated),
	))
}

func (h *Handlers) HandleGetServiceUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.writeServiceRepresentation(w, r, svc, h.serviceUpdatePolicyRepresentation)
}

func (h *Handlers) HandlePatchServiceUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	patchServiceSpecField(h, w, r, serviceSpecFieldPatch[swarm.UpdateConfig]{
		errCode:  "SVC012",
		errMsg:   "invalid update policy specification",
		suffix:   "/update-policy",
		typeName: "ServiceUpdatePolicy",
		field: func(spec *swarm.ServiceSpec) (**swarm.UpdateConfig, error) {
			return &spec.UpdateConfig, nil
		},
		body: serviceUpdatePolicyBody,
	})
}

func (h *Handlers) HandleGetServiceRollbackPolicy(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.writeServiceRepresentation(w, r, svc, h.serviceRollbackPolicyRepresentation)
}

func (h *Handlers) HandlePatchServiceRollbackPolicy(w http.ResponseWriter, r *http.Request) {
	patchServiceSpecField(h, w, r, serviceSpecFieldPatch[swarm.UpdateConfig]{
		errCode:  "SVC013",
		errMsg:   "invalid rollback policy specification",
		suffix:   "/rollback-policy",
		typeName: "ServiceRollbackPolicy",
		field: func(spec *swarm.ServiceSpec) (**swarm.UpdateConfig, error) {
			return &spec.RollbackConfig, nil
		},
		body: serviceRollbackPolicyBody,
	})
}

func (h *Handlers) HandleGetServiceLogDriver(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}
	h.writeServiceRepresentation(w, r, svc, h.serviceLogDriverRepresentation)
}

func (h *Handlers) HandlePatchServiceLogDriver(w http.ResponseWriter, r *http.Request) {
	patchServiceSpecField(h, w, r, serviceSpecFieldPatch[swarm.Driver]{
		errCode:  "SVC019",
		errMsg:   "invalid log driver specification",
		suffix:   "/log-driver",
		typeName: "ServiceLogDriver",
		field: func(spec *swarm.ServiceSpec) (**swarm.Driver, error) {
			return &spec.TaskTemplate.LogDriver, nil
		},
		body: serviceLogDriverBody,
	})
}

func (h *Handlers) HandlePatchServiceHealthcheck(w http.ResponseWriter, r *http.Request) {
	patchServiceSpecField(h, w, r, serviceSpecFieldPatch[container.HealthConfig]{
		errCode:  "SVC014",
		errMsg:   "invalid healthcheck specification",
		suffix:   "/healthcheck",
		typeName: "ServiceHealthcheck",
		field: func(spec *swarm.ServiceSpec) (**container.HealthConfig, error) {
			if spec.TaskTemplate.ContainerSpec == nil {
				return nil, errNoContainerSpec
			}

			return &spec.TaskTemplate.ContainerSpec.Healthcheck, nil
		},
		body: serviceHealthcheckBody,
	})
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
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceContainerConfigRepresentation)
}

func (h *Handlers) HandlePatchServiceContainerConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, ok := lookupOr404(w, r, "service", id, h.cache.GetService); !ok {
		return
	}

	merge, ok := structMergePatch(w, r, "SVC018", "invalid patch result")
	if !ok {
		return
	}

	updated, err := h.updateService(
		r,
		cluster.OpUpdateService,
		id,
		cluster.EditContainer(func(cs *swarm.ContainerSpec) error {
			var merged containerConfigResponse
			if err := merge(containerConfigFromSpec(cs), &merged); err != nil {
				return err
			}

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

			return nil
		}),
	)
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		r.URL.Path,
		"ServiceContainerConfig",
		serviceContainerConfigBody(updated),
	))
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
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceConfigsRepresentation)
}

func (h *Handlers) HandlePatchServiceConfigs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !requireMergePatch(w, r) {
		return
	}

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	refs, present, ok := decodeListMergePatch[serviceConfigRef](w, r, "configs")
	if !ok {
		return
	}
	if !present {
		writeMutationResponse(w, r, NewDetailResponse(
			r.Context(), "/services/"+id+"/configs", "ServiceConfigs", serviceConfigsBody(svc),
		))
		return
	}

	configs := make([]*swarm.ConfigReference, len(refs))
	for i, ref := range refs {
		if ref.ConfigID == "" || ref.ConfigName == "" {
			problem.Write(
				w,
				r,
				"SVC015",
				"each config must have configID and configName",
			)
			return
		}
		cfg, found := h.cache.GetConfig(ref.ConfigID)
		if !found {
			writeAttachmentNotFound(w, r, cluster.AttachConfig, ref.ConfigID)
			return
		}
		configs[i] = cluster.ConfigReference(cfg, ref.FileName)
	}

	updated, err := h.updateService(
		r,
		cluster.OpUpdateServiceConfigs,
		id,
		cluster.SetConfigs(configs),
	)
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/configs",
		"ServiceConfigs",
		serviceConfigsBody(updated),
	))
}

// writeAttachmentNotFound answers a reference to a resource the cache does not hold.
func writeAttachmentNotFound(
	w http.ResponseWriter,
	r *http.Request,
	kind cluster.AttachmentKind,
	id string,
) {
	problem.Write(w, r, "SVC021", fmt.Sprintf("%s %q not found", kind, id))
}

func (h *Handlers) HandleGetServiceSecrets(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceSecretsRepresentation)
}

func (h *Handlers) HandlePatchServiceSecrets(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !requireMergePatch(w, r) {
		return
	}

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	refs, present, ok := decodeListMergePatch[serviceSecretRef](w, r, "secrets")
	if !ok {
		return
	}
	if !present {
		writeMutationResponse(w, r, NewDetailResponse(
			r.Context(), "/services/"+id+"/secrets", "ServiceSecrets", serviceSecretsBody(svc),
		))
		return
	}

	secrets := make([]*swarm.SecretReference, len(refs))
	for i, ref := range refs {
		if ref.SecretID == "" || ref.SecretName == "" {
			problem.Write(
				w,
				r,
				"SVC016",
				"each secret must have secretID and secretName",
			)
			return
		}
		sec, found := h.cache.GetSecret(ref.SecretID)
		if !found {
			writeAttachmentNotFound(w, r, cluster.AttachSecret, ref.SecretID)
			return
		}
		secrets[i] = cluster.SecretReference(sec, ref.FileName)
	}

	updated, err := h.updateService(
		r,
		cluster.OpUpdateServiceSecrets,
		id,
		cluster.SetSecrets(secrets),
	)
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/secrets",
		"ServiceSecrets",
		serviceSecretsBody(updated),
	))
}

func (h *Handlers) HandleGetServiceNetworks(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceNetworksRepresentation)
}

func (h *Handlers) HandlePatchServiceNetworks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !requireMergePatch(w, r) {
		return
	}

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	refs, present, ok := decodeListMergePatch[serviceNetworkRef](w, r, "networks")
	if !ok {
		return
	}
	if !present {
		writeMutationResponse(w, r, NewDetailResponse(
			r.Context(), "/services/"+id+"/networks", "ServiceNetworks", serviceNetworksBody(svc),
		))
		return
	}

	networks := make([]swarm.NetworkAttachmentConfig, len(refs))
	for i, ref := range refs {
		if ref.Target == "" {
			problem.Write(w, r, "SVC017", "each network must have a target")
			return
		}
		target, found, resolveErr := h.cache.ResolveNetwork(ref.Target)
		if h.reportAmbiguousName(w, r, "network", resolveErr) {
			return
		}
		if resolveErr != nil || !found {
			writeAttachmentNotFound(w, r, cluster.AttachNetwork, ref.Target)
			return
		}
		networks[i] = swarm.NetworkAttachmentConfig{
			Target:  target.ID,
			Aliases: ref.Aliases,
		}
	}

	slog.InfoContext(r.Context(), "updating service networks", "service", id)

	updated, err := h.writer.UpdateServiceSpec(
		r.Context(),
		id,
		h.writes.CheckedChange(h.caller(r), cluster.SetNetworks(networks)),
	)
	if err != nil {
		writeServiceSectionError(w, r, id, err)
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/networks",
		"ServiceNetworks",
		serviceNetworksBody(updated),
	))
}

func (h *Handlers) HandleGetServiceMounts(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceMountsRepresentation)
}

func (h *Handlers) HandlePatchServiceMounts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if !requireMergePatch(w, r) {
		return
	}

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	mounts, present, ok := decodeListMergePatch[mount.Mount](w, r, "mounts")
	if !ok {
		return
	}

	updated := svc
	if present {
		var err error
		updated, err = h.updateService(
			r,
			cluster.OpUpdateServiceMounts,
			id,
			cluster.SetMounts(mounts),
		)
		if err != nil {
			writeServiceSectionError(w, r, id, err)
			return
		}
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/mounts",
		"ServiceMounts",
		serviceMountsBody(updated),
	))
}
