package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// patchFormats are the bodies requireMergePatch and parsePatchMutator admit.
var patchFormats = map[string]bool{
	"application/merge-patch+json": true,
	"application/json-patch+json":  true,
}

// Writes whose answer is not the resource, so Prefer: return=minimal has nothing to omit.
var writesWithoutMinimal = map[string]bool{
	"POST /-/resync":           true,
	"POST /plugins/privileges": true,
}

// PATCH /plugins/{name}/settings takes a plain JSON body, as Docker's plugin set does.
var patchesWithPlainJSON = map[string]bool{
	"PATCH /plugins/{name}/settings": true,
}

// What a write answers, read off the spec: each refusal its middleware or
// helpers can send must be documented on every operation they guard.
func TestEveryWriteDocumentsItsRefusals(t *testing.T) {
	_, doc, _ := loadTestSpec(t)

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if method == http.MethodGet || method == http.MethodHead {
				continue
			}

			key := method + " " + path
			responses := op.Responses.Map()

			if gated(op) && responses["403"] == nil {
				t.Errorf("%s: gated on an operations level but documents no 403", key)
			}

			if responses["200"] != nil && responses["204"] == nil && !writesWithoutMinimal[key] {
				t.Errorf("%s: answers 200 but documents no 204 for Prefer: return=minimal", key)
			}

			if method != http.MethodPatch || patchesWithPlainJSON[key] {
				continue
			}

			if responses["415"] == nil {
				t.Errorf("%s: takes a patch format but documents no 415", key)
			}

			if op.RequestBody == nil || op.RequestBody.Value == nil {
				t.Errorf("%s: documents no request body", key)
				continue
			}

			for media := range op.RequestBody.Value.Content {
				if !patchFormats[media] {
					t.Errorf(
						"%s: documents a %s body, which the handler refuses with 415",
						key,
						media,
					)
				}
			}
		}
	}
}

func gated(op *openapi3.Operation) bool {
	badges, _ := op.Extensions["x-badges"].([]any)
	for _, badge := range badges {
		if b, ok := badge.(map[string]any); ok {
			if name, _ := b["name"].(string); strings.HasPrefix(name, "operations-level:") {
				return true
			}
		}
	}

	return false
}
