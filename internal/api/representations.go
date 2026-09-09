package api

import "net/http"

// representationFunc builds the value a GET at this URI would serialize, so a
// precondition can be compared against the exact ETag that GET emits.
//
// It reports false when the resource does not exist and must never write to
// the response — the precondition middleware and the GET handler answer that
// case differently (412 versus 404).
type representationFunc func(*http.Request) (any, bool)

func (h *Handlers) serviceEnvRepresentation(r *http.Request) (any, bool) {
	svc, ok := h.cache.GetService(r.PathValue("id"))
	if !ok {
		return nil, false
	}

	var env []string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		env = svc.Spec.TaskTemplate.ContainerSpec.Env
	}

	return NewDetailResponse(
		r.Context(),
		"/services/"+svc.ID+"/env",
		"ServiceEnv",
		EnvResponse{Env: envSliceToMap(env)},
	), true
}
