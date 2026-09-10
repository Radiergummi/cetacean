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

// detail builds the JSON-LD body a labels GET serializes for an already
// resolved item. Both the GET handler and the precondition representation go
// through it, so they cannot describe the same labels differently.
func (spec getLabelsSpec[T]) detail(r *http.Request, item T) DetailResponse {
	labels := spec.getLabels(item)
	if labels == nil {
		labels = map[string]string{}
	}

	return NewDetailResponse(
		r.Context(),
		"/"+spec.resource+"s/"+r.PathValue(spec.pathKey)+"/labels",
		spec.typeName,
		LabelsResponse{Labels: labels},
	)
}

// representation is the spec's representationFunc, for wiring an If-Match
// precondition onto the paired PATCH.
func (spec getLabelsSpec[T]) representation(r *http.Request) (any, error) {
	item, ok := spec.getter(r.PathValue(spec.pathKey))
	if !ok {
		return nil, errNoRepresentation
	}

	return spec.detail(r, item), nil
}

// patchLabelsSpec describes how to patch labels for a resource type.
type patchLabelsSpec[T any] struct {
	resource  string // e.g. "node", "service"
	pathKey   string // path value key: "id" or "name"
	typeName  string // JSON-LD type: "NodeLabels", "ServiceLabels", etc.
	getter    func(string) (T, bool)
	getLabels func(T) map[string]string
	update    func(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (T, error)
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

	writeCachedJSON(w, r, spec.detail(r, item))
}

func handlePatchLabels[T any](
	w http.ResponseWriter,
	r *http.Request,
	spec patchLabelsSpec[T],
) {
	key := r.PathValue(spec.pathKey)
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Cache lookup is for the 404 only — the merge runs against the
	// freshly-inspected spec inside the writer (M-42).
	if _, ok := lookupOr404(w, r, spec.resource, key, spec.getter); !ok {
		return
	}

	mutate, ok := parsePatchMutator(w, r)
	if !ok {
		return
	}

	slog.Info("patching "+spec.resource+" labels", spec.resource, key)

	result, err := spec.update(r.Context(), key, mutate)
	if err != nil {
		if isPatchApplyError(err) {
			writePatchError(w, r, err)
			return
		}
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
