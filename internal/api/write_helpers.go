package api

import (
	"errors"
	"log/slog"
	"net/http"

	cerrdefs "github.com/containerd/errdefs"
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

// writeConfigError handles Docker API errors for config mutations,
// mapping version conflicts to CFG005.
func writeConfigError(w http.ResponseWriter, r *http.Request, err error) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "CFG005", err.Error())
		return
	}
	writeDockerError(w, r, err, "config")
}

// writeSecretError handles Docker API errors for secret mutations,
// mapping version conflicts to SEC005.
func writeSecretError(w http.ResponseWriter, r *http.Request, err error) {
	if cerrdefs.IsConflict(err) || cerrdefs.IsFailedPrecondition(err) {
		writeErrorCode(w, r, "SEC005", err.Error())
		return
	}
	writeDockerError(w, r, err, "secret")
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
