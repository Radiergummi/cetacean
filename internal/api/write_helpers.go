package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// lookupOr404 resolves a resource from the cache by key. Returns false (and
// writes a 404 error response) if the resource is not found. The error code
// is looked up from notFoundCodes by resource name.
func lookupOr404[T any](
	w http.ResponseWriter,
	r *http.Request,
	resource string,
	key string,
	getter func(string) (T, bool),
) (T, bool) {
	item, ok := getter(key)
	if !ok {
		code := notFoundCodes[resource]
		writeErrorCode(w, r, code, fmt.Sprintf("%s %q not found", resource, key))
	}
	return item, ok
}

// lookupACL resolves a resource by key, checks the read ACL, and returns it.
// Returns false (and writes the error response) if not found or denied.
func lookupACL[T any](
	h *Handlers,
	w http.ResponseWriter,
	r *http.Request,
	resource string,
	key string,
	getter func(string) (T, bool),
	aclResource func(T) string,
) (T, bool) {
	item, ok := lookupOr404(w, r, resource, key, getter)
	if !ok {
		return item, false
	}

	if !h.acl.Can(auth.IdentityFromContext(r.Context()), "read", aclResource(item)) {
		writeErrorCode(w, r, "ACL001", "access denied")
		var zero T
		return zero, false
	}

	return item, true
}

// filterServiceRefs applies ACL read filtering to a list of service
// cross-references. Used by detail handlers that include "used by" services.
// Always returns a non-nil slice so the field serializes as [] instead of null.
func (h *Handlers) filterServiceRefs(r *http.Request, refs []cache.ServiceRef) []cache.ServiceRef {
	filtered := acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		refs,
		func(ref cache.ServiceRef) string {
			return "service:" + ref.Name
		},
	)
	if filtered == nil {
		return []cache.ServiceRef{}
	}
	return filtered
}

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
	"stack":   "STK001",
}

// decodeJSON reads and decodes a JSON request body into T, enforcing a 1MB
// size limit. Returns the decoded value and true on success. On failure, it
// writes an API006 error response and returns the zero value and false.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return v, false
	}

	return v, true
}

func writeDockerError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	resource string,
	id string,
) {
	if cerrdefs.IsNotFound(err) {
		detail := fmt.Sprintf("%s %q not found", resource, id)
		if code, ok := notFoundCodes[resource]; ok {
			writeErrorCode(w, r, code, detail)
		} else {
			writeProblem(w, r, http.StatusNotFound, detail)
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

// sequenceConflict is Swarmkit's optimistic-concurrency refusal: the version
// the update carried is no longer the object's current one, because something
// committed in between.
//
// It has to be matched on its message. Swarmkit raises it with gRPC code
// Unknown, which Docker renders as a bare HTTP 500 and cerrdefs classifies as
// an internal error, so there is no class to test for — and the message is the
// one `docker service update` prints for the same race.
const sequenceConflict = "update out of sequence"

// writeResourceError handles Docker API errors for resource mutations,
// mapping version conflicts to the given conflictCode.
func writeResourceError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	resource, id, conflictCode string,
) {
	if cerrdefs.IsConflict(err) ||
		cerrdefs.IsFailedPrecondition(err) ||
		strings.Contains(err.Error(), sequenceConflict) {
		writeErrorCode(w, r, conflictCode, err.Error())
		return
	}
	writeDockerError(w, r, err, resource, id)
}

// writeMutation calls a writer function and writes the standard detail
// response. It handles error mapping via writeResourceError and JSON-LD
// wrapping via the resp callback.
func writeMutation[T any](
	w http.ResponseWriter,
	r *http.Request,
	resource, id, conflictCode string,
	resp func(T) DetailResponse,
	fn func() (T, error),
) {
	updated, err := fn()
	if err != nil {
		writeResourceError(w, r, err, resource, id, conflictCode)
		return
	}
	writeMutationResponse(w, r, resp(updated))
}

// writeServiceMutation calls a service writer function and writes the standard
// service detail response, honouring the RFC 7240 wait and respond-async
// preferences on the way.
//
// It spells out what writeMutation does rather than calling it: the preference
// handling sits between the write and the response, and a wait that runs out
// answers 202 instead.
func (h *Handlers) writeServiceMutation(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	fn func() (swarm.Service, error),
) {
	svc, err := fn()
	if err != nil {
		writeResourceError(w, r, err, "service", id, "SVC001")
		return
	}

	svc, handled := h.awaitPreferred(w, r, id, svc)
	if handled {
		return
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(), "/services/"+id, "Service", ServiceResponse{Service: svc},
	))
}

// awaitPreferred applies the RFC 7240 wait and respond-async preferences to a
// service mutation Docker has already accepted. It returns the service the
// caller should render, and reports whether it wrote the response itself.
//
// The version to converge to comes from the service the write returned, not
// from the asynchronously filled cache, where reading it back is a race. The
// request context is passed through as given, so a client that hangs up cancels
// its own wait rather than leaving a five-minute goroutine behind.
func (h *Handlers) awaitPreferred(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	svc swarm.Service,
) (swarm.Service, bool) {
	// id addresses the response, matching how the 200 identifies itself;
	// svc.ID addresses the cache, which is keyed by ID alone.
	wait, wanted := preferWait(r)
	async := preferRespondAsync(r)

	if !wanted && !async {
		return svc, false
	}

	var (
		progress string
		err      error
	)

	if wanted {
		progress, err = cluster.AwaitService(
			r.Context(), h.cache, svc.ID, svc.Version.Index,
			cluster.ConvergencePollInterval, wait,
		)
	}

	// RFC 7240 §2 asks for the wait actually applied, which preferWait may
	// have clamped to the server ceiling, not for the one requested.
	if wanted && !async && err == nil {
		applyPreference(
			w,
			"wait="+strconv.FormatInt(int64(wait/time.Second), 10),
		)

		// The service Docker returned describes the moment it accepted the
		// write — the state the wait existed to move past. AwaitService only
		// succeeds once the cache holds that version or beyond, so the cached
		// copy is the settled one, unless it has since been removed.
		if settled, ok := h.cache.GetService(svc.ID); ok {
			return settled, false
		}

		return svc, false
	}

	// RFC 7240 §4.1 asks only for somewhere to obtain status: the service's own
	// UpdateStatus reports convergence, with a per-resource SSE stream beside it.
	w.Header().Set("Location", absPath(r.Context(), "/services/"+id))

	if async {
		applyPreference(w, "respond-async")
	}

	// writeJSONStatus, not writeCachedJSONStatus: an ETag here would invite a
	// 304 on a write that did happen.
	writeJSONStatus(w, http.StatusAccepted, NewDetailResponse(
		r.Context(), "/services/"+id, "Service", AcceptedServiceResponse{
			Service:  svc,
			Progress: progress,
		},
	))

	return svc, true
}

// writeNodeMutation calls a node writer function and writes the standard
// node detail response.
func writeNodeMutation(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	fn func() (swarm.Node, error),
) {
	writeMutation(w, r, "node", id, "NOD002", func(node swarm.Node) DetailResponse {
		return NewDetailResponse(r.Context(), "/nodes/"+id, "Node", NodeResponse{
			Node: node,
		})
	}, fn)
}

// specPatchError is a refusal raised inside a service-spec mutator, carrying
// the code to answer with. The mutator runs inside the writer (see
// structMergePatch), so the handler sees only an error and has to be able to
// tell one of these from a Docker failure.
type specPatchError struct {
	code    string
	message string
}

func (e *specPatchError) Error() string { return e.message }

// errNoContainerSpec is what a mutator reaching for a service's container spec
// raises when there is none, matching the code the writers answered with when
// they made that check themselves.
var errNoContainerSpec = &specPatchError{"ENG003", "service has no container spec"}

// structMergePatch reads a merge-patch body and returns a function that merges
// it into a current value (any JSON-marshalable struct) and unmarshals the
// result into target. Returns false and writes an error response when the
// request itself is unusable — a wrong Content-Type, an unreadable body,
// invalid JSON.
//
// Parsing and applying are separate so the merge can run inside the writer,
// against the service as the engine currently holds it. Merging into the
// asynchronously filled cache would silently discard a field written moments
// earlier, and nothing downstream could catch it: the writer reads the version
// it writes with in the same breath as the spec, so the engine's own
// optimistic concurrency sees nothing stale (M-42).
func structMergePatch(
	w http.ResponseWriter,
	r *http.Request,
	errCode string,
	errMsg string,
) (func(current, target any) error, bool) {
	if !requireMergePatch(w, r) {
		return nil, false
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return nil, false
	}

	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return nil, false
	}

	return func(current, target any) error {
		base, err := json.Marshal(current)
		if err != nil {
			return &specPatchError{"API009", "failed to marshal current state"}
		}

		var baseMap map[string]any
		if err := json.Unmarshal(base, &baseMap); err != nil {
			return &specPatchError{"API009", "failed to unmarshal current state"}
		}

		mergePatch(baseMap, patchMap)

		merged, err := json.Marshal(baseMap)
		if err != nil {
			return &specPatchError{"API009", "failed to marshal merged state"}
		}

		if err := json.Unmarshal(merged, target); err != nil {
			return &specPatchError{errCode, errMsg}
		}

		return nil
	}, true
}

// writeServiceSpecPatch answers a service merge patch: a specPatchError is the
// request's own fault, anything else is the engine's.
func writeServiceSpecPatch(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	err error,
) {
	var spe *specPatchError
	if errors.As(err, &spe) {
		writeErrorCode(w, r, spe.code, spe.message)
		return
	}

	writeResourceError(w, r, err, "service", id, "SVC001")
}

// requireMergePatch validates Content-Type is application/merge-patch+json.
// Returns false and writes a 415 error response if not satisfied.
func requireMergePatch(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/merge-patch+json") {
		writeErrorCode(w, r, "API004", "expected Content-Type: application/merge-patch+json")
		return false
	}
	return true
}

// parsePatchMutator validates Content-Type, reads the request body, and
// returns a MapMutator that applies the parsed JSON Patch (RFC 6902) or JSON
// Merge Patch (RFC 7396) to whatever fresh current-state map the writer hands
// it. The mutator is invoked inside the Docker writer against a live inspect
// — pre-merging against the in-memory cache would race third-party writers
// and silently drop their concurrent changes.
//
// Content-type and body-read failures are written to w and ok=false is
// returned. Patch *application* failures (test-failed, unknown op, replace
// of a missing key) bubble up from the mutator so the caller can decide the
// HTTP status — see writePatchError.
func parsePatchMutator(
	w http.ResponseWriter,
	r *http.Request,
) (func(map[string]string) (map[string]string, error), bool) {
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
		return nil, false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return nil, false
	}

	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeErrorCode(w, r, "API006", "invalid request body")
			return nil, false
		}
		return func(current map[string]string) (map[string]string, error) {
			if current == nil {
				current = map[string]string{}
			}
			return applyJSONPatch(current, ops)
		}, true
	}

	// Merge Patch (RFC 7396): unmarshal the patch document upfront so invalid
	// JSON surfaces as a 400 before the writer even runs an inspect.
	var mergePatch map[string]*string
	if err := json.Unmarshal(body, &mergePatch); err != nil {
		writeErrorCode(w, r, "API006", "invalid request body")
		return nil, false
	}
	return func(current map[string]string) (map[string]string, error) {
		out := make(map[string]string, len(current)+len(mergePatch))
		maps.Copy(out, current)
		for k, v := range mergePatch {
			if v == nil {
				delete(out, k)
				continue
			}
			out[k] = *v
		}
		return out, nil
	}, true
}

// isPatchApplyError reports whether err originated from patch application
// inside the writer-side mutator. These errors map to 400/409 rather than
// the writer's usual 500 / SVC001 / NODE001 codes.
func isPatchApplyError(err error) bool {
	if err == nil {
		return false
	}
	var tfe *testFailedError
	if errors.As(err, &tfe) {
		return true
	}
	return errors.Is(err, errPatchApply)
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

type createResourceRequest struct {
	Name string `json:"name"`
	Data string `json:"data"`
}
