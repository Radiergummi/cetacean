package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// getLabelsSpec describes how to read labels for a resource type addressed by
// the "id" path value.
type getLabelsSpec[T any] struct {
	resource    string // e.g. "node", "service"
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
		"/"+spec.resource+"s/"+r.PathValue("id")+"/labels",
		spec.typeName,
		LabelsResponse{Labels: labels},
	)
}

// representation is the spec's representationFunc, for wiring an If-Match
// precondition onto the paired PATCH.
func (spec getLabelsSpec[T]) representation(r *http.Request) (any, error) {
	item, ok := spec.getter(r.PathValue("id"))
	if !ok {
		return nil, errNoRepresentation
	}

	return spec.detail(r, item), nil
}

// patchLabelsSpec describes how to patch labels for a resource type.
type patchLabelsSpec[T any] struct {
	getLabelsSpec[T]

	update func(
		ctx context.Context,
		id string,
		mutate func(current map[string]string) (map[string]string, error),
	) (T, error)
	conflictCode string

	// stackMember marks a type whose stack membership follows its labels.
	stackMember bool

	// hold, when set, answers a refusal itself or returns the context to write
	// with and the check a label change must pass inside the write.
	hold func(
		w http.ResponseWriter,
		r *http.Request,
		item T,
	) (context.Context, func(before, after map[string]string) error, bool)
}

func handleGetLabels[T any](
	w http.ResponseWriter,
	r *http.Request,
	h *Handlers,
	spec getLabelsSpec[T],
) {
	key := r.PathValue("id")

	item, ok := lookupACL(h, w, r, spec.resource, key, spec.getter, spec.aclResource)
	if !ok {
		return
	}
	h.setAllowSubResource(w, r, spec.aclResource(item))

	writeCachedJSON(w, r, spec.detail(r, item))
}

func handlePatchLabels[T any](
	w http.ResponseWriter,
	r *http.Request,
	evaluator *acl.Evaluator,
	spec patchLabelsSpec[T],
) {
	key := r.PathValue("id")

	// Cache lookup is for the 404 only — the merge runs against the
	// freshly-inspected spec inside the writer (M-42).
	item, ok := lookupOr404(w, r, spec.resource, key, spec.getter)
	if !ok {
		return
	}

	mutate, ok := parsePatchMutator(w, r)
	if !ok {
		return
	}

	if spec.stackMember {
		identity := auth.IdentityFromContext(r.Context())
		mutate = cluster.GuardStackLabel(mutate, func(resource string) bool {
			return evaluator.Can(identity, "write", resource)
		})
	}

	ctx := r.Context()
	var refused error
	if spec.hold != nil {
		var check func(before, after map[string]string) error
		if ctx, check, ok = spec.hold(w, r, item); !ok {
			return
		}

		inner := mutate
		mutate = func(current map[string]string) (map[string]string, error) {
			updated, err := inner(current)
			if err != nil {
				return nil, err
			}
			refused = check(current, updated)

			return updated, refused
		}
	}

	slog.Info("patching "+spec.resource+" labels", spec.resource, key)

	result, err := spec.update(ctx, key, mutate)
	if refused != nil {
		writeErrorCode(w, r, "OPS001", refused.Error())
		return
	}
	if err != nil {
		if denied, ok := errors.AsType[*cluster.StackLabelDeniedError](err); ok {
			writeErrorCode(w, r, "ACL002", denied.Error())
			return
		}
		if isPatchApplyError(err) {
			writePatchError(w, r, err)
			return
		}
		writeResourceError(w, r, err, spec.resource, key, spec.conflictCode)
		return
	}

	writeMutationResponse(w, r, spec.detail(r, result))
}
