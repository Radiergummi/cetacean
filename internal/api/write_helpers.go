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

// writeServiceError handles Docker API errors for service mutations,
// mapping version conflicts to SVC001.
func writeServiceError(w http.ResponseWriter, r *http.Request, err error, id string) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "SVC001", err.Error())
		return
	}
	writeDockerError(w, r, err, "service", id)
}

// writeNodeError handles Docker API errors for node mutations,
// mapping version conflicts to NOD002.
func writeNodeError(w http.ResponseWriter, r *http.Request, err error, id string) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "NOD002", err.Error())
		return
	}
	writeDockerError(w, r, err, "node", id)
}

// writeConfigError handles Docker API errors for config mutations,
// mapping version conflicts to CFG005.
func writeConfigError(w http.ResponseWriter, r *http.Request, err error, id string) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "CFG005", err.Error())
		return
	}
	writeDockerError(w, r, err, "config", id)
}

// writeSecretError handles Docker API errors for secret mutations,
// mapping version conflicts to SEC005.
func writeSecretError(w http.ResponseWriter, r *http.Request, err error, id string) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "SEC005", err.Error())
		return
	}
	writeDockerError(w, r, err, "secret", id)
}

// writeServiceMutation calls a service writer function and writes the standard
// service detail response. It handles error mapping and JSON-LD wrapping.
func writeServiceMutation(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	fn func() (swarm.Service, error),
) {
	updated, err := fn()
	if err != nil {
		writeServiceError(w, r, err, id)
		return
	}
	writeMutationResponse(
		w,
		r,
		NewDetailResponse(r.Context(), "/services/"+id, "Service", ServiceResponse{
			Service: updated,
		}),
	)
}

// writeNodeMutation calls a node writer function and writes the standard
// node detail response.
func writeNodeMutation(
	w http.ResponseWriter,
	r *http.Request,
	id string,
	fn func() (swarm.Node, error),
) {
	updated, err := fn()
	if err != nil {
		writeNodeError(w, r, err, id)
		return
	}
	writeMutationResponse(w, r, NewDetailResponse(r.Context(), "/nodes/"+id, "Node", NodeResponse{
		Node: updated,
	}))
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
