package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

// getLabelsSpec describes how to read labels for a resource type.
type getLabelsSpec[T any] struct {
	resource    string // e.g. "node", "service"
	pathKey     string // path value key: "id" or "name"
	typeName    string // JSON-LD type: "NodeLabels", "ServiceLabels", etc.
	getter      func(string) (T, bool)
	aclResource func(T) string
	getLabels   func(T) map[string]string
}

// patchLabelsSpec describes how to patch labels for a resource type.
type patchLabelsSpec[T any] struct {
	resource     string // e.g. "node", "service"
	pathKey      string // path value key: "id" or "name"
	typeName     string // JSON-LD type: "NodeLabels", "ServiceLabels", etc.
	getter       func(string) (T, bool)
	getLabels    func(T) map[string]string
	update       func(ctx context.Context, id string, labels map[string]string) (T, error)
	conflictCode string
}

func handleGetLabels[T any](
	w http.ResponseWriter,
	r *http.Request,
	evaluator *acl.Evaluator,
	spec getLabelsSpec[T],
) {
	key := r.PathValue(spec.pathKey)

	item, ok := lookupOr404(w, r, spec.resource, key, spec.getter)
	if !ok {
		return
	}

	if !evaluator.Can(auth.IdentityFromContext(r.Context()), "read", spec.aclResource(item)) {
		writeErrorCode(w, r, "ACL001", "access denied")
		return
	}

	labels := spec.getLabels(item)
	if labels == nil {
		labels = map[string]string{}
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/"+spec.resource+"s/"+key+"/labels",
			spec.typeName,
			LabelsResponse{Labels: labels},
		),
	)
}

func handlePatchLabels[T any](
	w http.ResponseWriter,
	r *http.Request,
	spec patchLabelsSpec[T],
) {
	key := r.PathValue(spec.pathKey)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	item, ok := lookupOr404(w, r, spec.resource, key, spec.getter)
	if !ok {
		return
	}

	updated, ok := patchStringMap(w, r, spec.getLabels(item))
	if !ok {
		return
	}

	slog.Info("patching "+spec.resource+" labels", spec.resource, key)

	result, err := spec.update(r.Context(), key, updated)
	if err != nil {
		writeResourceError(w, r, err, spec.resource, key, spec.conflictCode)
		return
	}

	labels := spec.getLabels(result)
	if labels == nil {
		labels = map[string]string{}
	}

	writeMutationResponse(w, r, NewDetailResponse(
		r.Context(),
		r.URL.Path,
		spec.typeName,
		LabelsResponse{Labels: labels},
	))
}
