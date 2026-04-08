package api

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
)

// createDataResourceSpec describes how to create a data resource (config or
// secret). Used by handleCreateDataResource to eliminate per-resource
// boilerplate.
type createDataResourceSpec struct {
	resource     string // "config" or "secret"
	nameErrCode  string // validation error code
	conflictCode string // create-conflict error code
	basePath     string // URL path prefix including trailing slash
	typeName     string // JSON-LD type name

	create        func(ctx context.Context, name string, data []byte) (string, error)
	buildFallback func(id string, name string) any
	buildResponse func(id string) (any, bool)
}

func handleCreateDataResource(w http.ResponseWriter, r *http.Request, spec createDataResourceSpec) {
	req, ok := decodeJSON[createResourceRequest](w, r)
	if !ok {
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeErrorCode(w, r, spec.nameErrCode, "name is required")
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		writeErrorCode(w, r, spec.nameErrCode, "data must be valid base64")
		return
	}

	slog.Info("creating "+spec.resource, "name", req.Name)

	id, err := spec.create(r.Context(), req.Name, data)
	if err != nil {
		if cerrdefs.IsConflict(err) {
			writeErrorCode(w, r, spec.conflictCode, err.Error())
			return
		}

		writeDockerError(w, r, err, spec.resource, req.Name)
		return
	}

	w.Header().Set("Location", absPath(r.Context(), spec.basePath+id))

	if preferMinimal(r) {
		writePreferCreated(w)
		return
	}

	resp, ok := spec.buildResponse(id)
	if !ok {
		resp = spec.buildFallback(id, req.Name)
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, NewDetailResponse(r.Context(), spec.basePath+id, spec.typeName, resp))
}
