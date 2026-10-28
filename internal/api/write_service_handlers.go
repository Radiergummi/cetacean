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
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/docker"
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

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}
	if svc.Spec.Mode.Replicated == nil {
		problem.Write(w, r, "SVC005", "cannot scale a global-mode service")
		return
	}

	slog.InfoContext(r.Context(), "scaling service", "service", id, "replicas", *req.Replicas)

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.ScaleService(r.Context(), id, *req.Replicas)
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
		return h.serviceLifecycle.UpdateServiceEndpointMode(r.Context(), id, mode)
	})
}

func (h *Handlers) HandleUpdateServiceImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	req, ok := decodeJSON[updateImageRequest](w, r)
	if !ok {
		return
	}
	req.Image = cluster.ServiceImage(req.Image)
	if req.Image == "" {
		problem.Write(w, r, "SVC006", "image is required")
		return
	}

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	ctx, ok := h.checkHostAccessHeld(w, r, svc, "changing the image of")
	if !ok {
		return
	}

	slog.InfoContext(ctx, "updating service image", "service", id, "image", req.Image)

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.UpdateServiceImage(ctx, id, req.Image)
	})
}

func (h *Handlers) HandleRollbackService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}
	if svc.PreviousSpec == nil {
		problem.Write(w, r, "SVC007", "service has no previous spec to rollback to")
		return
	}

	ctx, ok := h.checkHostAccessHeld(w, r, svc, cluster.RollingBack)
	if !ok {
		return
	}

	slog.InfoContext(ctx, "rolling back service", "service", id)

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
		return h.serviceLifecycle.RollbackService(ctx, id)
	})
}

// checkHostAccessHeld answers OPS001 for a change cluster.CheckHostAccessHeld
// refuses. Below level 3, a write to a service holding host access is pinned to
// the cached version it was judged against, so a stale cache can't pass for
// "unchanged"; one holding none has nothing to let through and stays unpinned.
func (h *Handlers) checkHostAccessHeld(
	w http.ResponseWriter,
	r *http.Request,
	svc swarm.Service,
	change string,
) (context.Context, bool) {
	level := h.levelFor(r)
	if level >= config.OpsImpactful ||
		cluster.HeldHostAccess(svc, change == cluster.RollingBack, h.cache.GetVolume) == "" {
		return r.Context(), true
	}

	if err := cluster.CheckHostAccessHeld(svc, change, h.cache.GetVolume, level); err != nil {
		problem.Write(w, r, "OPS001", err.Error())
		return nil, false
	}

	return docker.PinChecked(r.Context(), "service", svc.ID, svc.Version), true
}

func (h *Handlers) HandleRemoveService(w http.ResponseWriter, r *http.Request) {
	handleRemove(w, r, removeSpec[swarm.Service]{
		resource:     "service",
		getter:       h.cache.GetService,
		remove:       h.serviceLifecycle.RemoveService,
		conflictCode: "SVC002",
	})
}

func (h *Handlers) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if _, ok := lookupOr404(w, r, "service", id, h.cache.GetService); !ok {
		return
	}

	slog.InfoContext(r.Context(), "restarting service", "service", id)

	h.writeServiceMutation(w, r, id, func() (swarm.Service, error) {
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
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeServiceRepresentation(w, r, svc, h.serviceEnvRepresentation)
}

func (h *Handlers) HandlePatchServiceEnv(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// The merge runs against the freshly-inspected spec inside the writer
	// (M-42); pre-merging against the cache would drop concurrent changes.
	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	mutate, ok := parsePatchMutator(w, r)
	if !ok {
		return
	}

	ctx, check, ok := h.holdServiceMap(w, r, svc, cluster.EnvChange)
	if !ok {
		return
	}

	var refused error
	guarded := func(current map[string]string) (map[string]string, error) {
		updated, err := mutate(current)
		if err != nil {
			return nil, err
		}

		refused = check(current, updated)

		return updated, refused
	}

	slog.InfoContext(ctx, "patching service env", "service", id)

	result, err := h.serviceSpec.UpdateServiceEnv(ctx, id, guarded)
	if refused != nil {
		problem.Write(w, r, "OPS001", refused.Error())
		return
	}
	if err != nil {
		if isPatchApplyError(err) {
			writePatchError(w, r, err)
			return
		}
		writeResourceError(w, r, err, "service", id, "SVC001")
		return
	}

	writeMutationResponse(
		w,
		r,
		NewDetailResponse(r.Context(), r.URL.Path, "ServiceEnv", serviceEnvBody(result)),
	)
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
		update:        h.serviceSpec.UpdateServiceLabels,
		conflictCode:  "SVC001",
		stackMember:   true,
		hold:          h.holdServiceLabels,
	})
}

func (h *Handlers) holdServiceLabels(
	w http.ResponseWriter,
	r *http.Request,
	svc swarm.Service,
) (context.Context, func(before, after map[string]string) error, bool) {
	return h.holdServiceMap(w, r, svc, cluster.LabelsChange)
}

// holdServiceMap refuses a change to one of a service's maps, as describe
// names it, to a service holding host access below level 3; check judges it
// inside the write, against Docker's copy of the map.
func (h *Handlers) holdServiceMap(
	w http.ResponseWriter,
	r *http.Request,
	svc swarm.Service,
	describe func(before, after map[string]string) string,
) (context.Context, func(before, after map[string]string) error, bool) {
	ctx, ok := h.checkHostAccessHeld(w, r, svc, "")
	if !ok {
		return nil, nil, false
	}

	level := h.levelFor(r)
	check := func(before, after map[string]string) error {
		return cluster.CheckHostAccessHeld(svc, describe(before, after), h.cache.GetVolume, level)
	}

	return ctx, check, true
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
		name:     "resources",
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
	name     string // for the log line, e.g. "update policy"
	errCode  string
	errMsg   string
	suffix   string
	typeName string
	field    func(*swarm.ServiceSpec) (**T, error)
	body     func(swarm.Service) any

	// held names a change to what runs in the container, for
	// cluster.CheckHostAccessHeld, or returns "" for one that isn't.
	held func(before, after *T) string
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

	slog.InfoContext(r.Context(), "updating service "+patch.name, "service", id)

	level := h.levelFor(r)
	updated, err := h.serviceSpec.UpdateServiceSpec(
		r.Context(),
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

			if patch.held != nil {
				live := swarm.Service{Spec: *spec}
				change := patch.held(current, &result)
				if err := cluster.CheckHostAccessHeld(
					live,
					change,
					h.cache.GetVolume,
					level,
				); err != nil {
					return &specPatchError{"OPS001", err.Error()}
				}
			}

			*field = &result

			return nil
		},
	)
	if err != nil {
		writeServiceSpecPatch(w, r, id, err)
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

		var current []swarm.PortConfig
		if updated.Spec.EndpointSpec != nil {
			current = updated.Spec.EndpointSpec.Ports
		}
		ctx, ok := h.checkHostAccessHeld(w, r, updated, cluster.PortsChange(current, ports))
		if !ok {
			return
		}

		slog.InfoContext(ctx, "updating service ports", "service", id)

		var err error
		if updated, err = h.serviceSpec.UpdateServicePorts(ctx, id, ports); err != nil {
			writeResourceError(w, r, err, "service", id, "SVC001")
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

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	hc, ok := decodeJSON[container.HealthConfig](w, r)
	if !ok {
		return
	}

	var before *container.HealthConfig
	if cs := svc.Spec.TaskTemplate.ContainerSpec; cs != nil {
		before = cs.Healthcheck
	}

	ctx, ok := h.checkHostAccessHeld(w, r, svc, cluster.HealthcheckChange(before, &hc))
	if !ok {
		return
	}

	slog.InfoContext(ctx, "updating service healthcheck", "service", id)

	updated, err := h.serviceSpec.UpdateServiceHealthcheck(ctx, id, &hc)
	if err != nil {
		writeResourceError(w, r, err, "service", id, "SVC001")
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

	svc, ok := lookupOr404(w, r, "service", id, h.cache.GetService)
	if !ok {
		return
	}

	placement, ok := decodeJSON[swarm.Placement](w, r)
	if !ok {
		return
	}

	ctx, ok := h.checkHostAccessHeld(w, r, svc, "changing the placement of")
	if !ok {
		return
	}

	slog.InfoContext(ctx, "updating service placement", "service", id)

	updated, err := h.serviceSpec.UpdateServicePlacement(ctx, id, &placement)
	if err != nil {
		writeResourceError(w, r, err, "service", id, "SVC001")
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
		name:     "update policy",
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
		name:     "rollback policy",
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
		name:     "log driver",
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
		name:     "healthcheck",
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
		held: cluster.HealthcheckChange,
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

	slog.InfoContext(r.Context(), "updating service container config", "service", id)

	level := h.levelFor(r)
	updated, err := h.serviceSpec.UpdateServiceSpec(
		r.Context(),
		id,
		func(spec *swarm.ServiceSpec) error {
			cs := spec.TaskTemplate.ContainerSpec
			if cs == nil {
				return errNoContainerSpec
			}

			var merged containerConfigResponse
			if err := merge(containerConfigFromSpec(cs), &merged); err != nil {
				return err
			}

			current := swarm.Service{Spec: *spec}
			if err := cluster.CheckCapabilities(current, merged.CapabilityAdd, level); err != nil {
				return &specPatchError{"OPS001", err.Error()}
			}

			change := cluster.CommandChange(cs, &swarm.ContainerSpec{
				Command: merged.Command,
				Args:    merged.Args,
				Dir:     merged.Dir,
				User:    merged.User,
				Groups:  merged.Groups,
			})
			if err := cluster.CheckHostAccessHeld(
				current,
				change,
				h.cache.GetVolume,
				level,
			); err != nil {
				return &specPatchError{"OPS001", err.Error()}
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
		},
	)
	if err != nil {
		writeServiceSpecPatch(w, r, id, err)
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
		if !h.checkAttachable(w, r, svc, cluster.AttachConfig, ref.ConfigID, cfg.Spec.Name, found) {
			return
		}
		configs[i] = cluster.ConfigReference(cfg, ref.FileName)
	}

	var currentConfigs []*swarm.ConfigReference
	if cs := svc.Spec.TaskTemplate.ContainerSpec; cs != nil {
		currentConfigs = cs.Configs
	}
	if _, ok := h.checkHostAccessHeld(
		w,
		r,
		svc,
		cluster.ConfigsChange(currentConfigs, configs),
	); !ok {
		return
	}

	slog.InfoContext(r.Context(), "updating service configs", "service", id)

	updated, err := h.serviceAttachment.UpdateServiceConfigs(
		docker.PinChecked(r.Context(), "service", svc.ID, svc.Version),
		id,
		configs,
	)
	if err != nil {
		writeResourceError(w, r, err, "service", id, "SVC001")
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		"/services/"+id+"/configs",
		"ServiceConfigs",
		serviceConfigsBody(updated),
	))
}

// checkAttachable writes an error unless the caller may attach the resource.
func (h *Handlers) checkAttachable(
	w http.ResponseWriter,
	r *http.Request,
	svc swarm.Service,
	kind cluster.AttachmentKind,
	id, name string,
	found bool,
) bool {
	identity := auth.IdentityFromContext(r.Context())
	canRead := func(resource string) bool { return h.acl.Can(identity, "read", resource) }

	if !found {
		problem.Write(w, r, "SVC021", fmt.Sprintf("%s %q not found", kind, id))
		return false
	}

	if cluster.CheckAttachable(svc, canRead, kind, id, name) != nil {
		problem.Write(w, r, "ACL001", "cannot attach "+string(kind)+" "+id)
		return false
	}

	return true
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
		if !h.checkAttachable(w, r, svc, cluster.AttachSecret, ref.SecretID, sec.Spec.Name, found) {
			return
		}
		secrets[i] = cluster.SecretReference(sec, ref.FileName)
	}

	var currentSecrets []*swarm.SecretReference
	if cs := svc.Spec.TaskTemplate.ContainerSpec; cs != nil {
		currentSecrets = cs.Secrets
	}
	if _, ok := h.checkHostAccessHeld(
		w,
		r,
		svc,
		cluster.SecretsChange(currentSecrets, secrets),
	); !ok {
		return
	}

	slog.InfoContext(r.Context(), "updating service secrets", "service", id)

	updated, err := h.serviceAttachment.UpdateServiceSecrets(
		docker.PinChecked(r.Context(), "service", svc.ID, svc.Version),
		id,
		secrets,
	)
	if err != nil {
		writeResourceError(w, r, err, "service", id, "SVC001")
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
			target.ID = ref.Target
		}
		if !h.checkAttachable(
			w,
			r,
			svc,
			cluster.AttachNetwork,
			target.ID,
			target.Name,
			found && resolveErr == nil,
		) {
			return
		}
		networks[i] = swarm.NetworkAttachmentConfig{
			Target:  target.ID,
			Aliases: ref.Aliases,
		}
	}

	change := cluster.NetworksChange(svc.Spec.TaskTemplate.Networks, networks)
	if _, ok := h.checkHostAccessHeld(w, r, svc, change); !ok {
		return
	}

	slog.InfoContext(r.Context(), "updating service networks", "service", id)

	updated, err := h.serviceAttachment.UpdateServiceNetworks(
		docker.PinChecked(r.Context(), "service", svc.ID, svc.Version),
		id,
		networks,
	)
	if err != nil {
		writeResourceError(w, r, err, "service", id, "SVC001")
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
		identity := auth.IdentityFromContext(r.Context())
		canRead := func(resource string) bool { return h.acl.Can(identity, "read", resource) }

		if err := cluster.CheckVolumesAttachable(svc, canRead, mounts); err != nil {
			problem.Write(w, r, "ACL001", err.Error())
			return
		}

		if err := cluster.CheckMounts(svc, mounts, h.cache.GetVolume, h.levelFor(r)); err != nil {
			problem.Write(w, r, "OPS001", err.Error())
			return
		}

		var current []mount.Mount
		if cs := svc.Spec.TaskTemplate.ContainerSpec; cs != nil {
			current = cs.Mounts
		}
		if _, ok := h.checkHostAccessHeld(w, r, svc, cluster.MountsChange(current, mounts)); !ok {
			return
		}

		slog.InfoContext(
			r.Context(),
			"updating service mounts",
			"service",
			id,
			"count",
			len(mounts),
		)

		var err error
		updated, err = h.serviceAttachment.UpdateServiceMounts(
			docker.PinChecked(r.Context(), "service", svc.ID, svc.Version),
			id,
			mounts,
		)
		if err != nil {
			writeResourceError(w, r, err, "service", id, "SVC001")
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
