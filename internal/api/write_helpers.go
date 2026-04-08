package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
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
func (h *Handlers) filterServiceRefs(r *http.Request, refs []cache.ServiceRef) []cache.ServiceRef {
	return acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		refs,
		func(ref cache.ServiceRef) string {
			return "service:" + ref.Name
		},
	)
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

// writeResourceError handles Docker API errors for resource mutations,
// mapping version conflicts to the given conflictCode.
func writeResourceError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	resource, id, conflictCode string,
) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
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
// service detail response.
func writeServiceMutation(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	fn func() (swarm.Service, error),
) {
	writeMutation(w, r, "service", id, "SVC001", func(svc swarm.Service) DetailResponse {
		return NewDetailResponse(r.Context(), "/services/"+id, "Service", ServiceResponse{
			Service: svc,
		})
	}, fn)
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

// applyStructMergePatch reads a merge-patch body, applies it to current (any
// JSON-marshalable struct), and unmarshals the result into target. Returns
// false and writes an error response on failure.
func applyStructMergePatch(
	w http.ResponseWriter,
	r *http.Request,
	current any,
	target any,
	errCode string,
	errMsg string,
) bool {
	if !requireMergePatch(w, r) {
		return false
	}
	base, err := json.Marshal(current)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal current state")
		return false
	}
	var baseMap map[string]any
	if err := json.Unmarshal(base, &baseMap); err != nil {
		writeErrorCode(w, r, "API009", "failed to unmarshal current state")
		return false
	}

	patchBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErrorCode(w, r, "API007", "failed to read request body")
		return false
	}
	var patchMap map[string]any
	if err := json.Unmarshal(patchBytes, &patchMap); err != nil {
		writeErrorCode(w, r, "API008", "invalid JSON")
		return false
	}

	mergePatch(baseMap, patchMap)

	merged, err := json.Marshal(baseMap)
	if err != nil {
		writeErrorCode(w, r, "API009", "failed to marshal merged state")
		return false
	}
	if err := json.Unmarshal(merged, target); err != nil {
		writeErrorCode(w, r, errCode, errMsg)
		return false
	}
	return true
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

// patchStringMap reads a JSON Patch or Merge Patch body, applies it to
// current, and returns the updated map. Returns nil and false (and writes
// the error response) on any failure.
func patchStringMap(
	w http.ResponseWriter,
	r *http.Request,
	current map[string]string,
) (map[string]string, bool) {
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

	if current == nil {
		current = map[string]string{}
	}

	var updated map[string]string
	if isJSONPatch {
		var ops []PatchOp
		if err := json.Unmarshal(body, &ops); err != nil {
			writeErrorCode(w, r, "API006", "invalid request body")
			return nil, false
		}
		updated, err = applyJSONPatch(current, ops)
	} else {
		updated, err = applyMergePatchStringMap(current, body)
	}

	if err != nil {
		writePatchError(w, r, err)
		return nil, false
	}

	return updated, true
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
