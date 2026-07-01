package api

import (
	"errors"
	"fmt"
	"maps"
)

// errPatchApply is the sentinel wrapping every patch-application failure
// from applyJSONPatch and the merge-patch closure built in
// parsePatchMutator. Callers use errors.Is to distinguish them from
// writer-side (Docker) errors so the HTTP layer can pick the right status
// code (400/409 vs 500).
var errPatchApply = errors.New("patch apply")

// PatchOp represents a single RFC 6902 JSON Patch operation.
type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}

// normalizePath strips a leading "/" if present, for convenience on flat maps.
// Per RFC 6902, paths use JSON Pointer (RFC 6901) syntax with a leading "/".
// We also accept paths without the leading slash for ergonomic flat-map usage.
func normalizePath(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}

// testFailedError indicates a "test" operation found a value mismatch.
// Handlers should map this to 409 Conflict.
type testFailedError struct {
	key, expected, actual string
}

func (e *testFailedError) Error() string {
	return fmt.Sprintf("test failed for %q: expected %q, got %q", e.key, e.expected, e.actual)
}

// applyJSONPatch applies RFC 6902 operations to a flat string map.
// Supports add, remove, replace, test. Returns
// the updated map or an error. Move and copy are rejected as
// unsupported for flat key-value maps.
func applyJSONPatch(m map[string]string, ops []PatchOp) (map[string]string, error) {
	// Copy the input map
	result := make(map[string]string, len(m))
	maps.Copy(result, m)

	for i, op := range ops {
		key := normalizePath(op.Path)
		if key == "" {
			return nil, fmt.Errorf("%w: operation %d: empty path", errPatchApply, i)
		}
		switch op.Op {
		case "add":
			result[key] = op.Value
		case "remove":
			if _, ok := result[key]; !ok {
				return nil, fmt.Errorf(
					"%w: operation %d: key %q does not exist",
					errPatchApply,
					i,
					key,
				)
			}
			delete(result, key)
		case "replace":
			if _, ok := result[key]; !ok {
				return nil, fmt.Errorf(
					"%w: operation %d: key %q does not exist",
					errPatchApply,
					i,
					key,
				)
			}
			result[key] = op.Value
		case "test":
			v, ok := result[key]
			if !ok {
				return nil, &testFailedError{key: key, expected: op.Value, actual: "(missing)"}
			}
			if v != op.Value {
				return nil, &testFailedError{key: key, expected: op.Value, actual: v}
			}
		case "move", "copy":
			return nil, fmt.Errorf(
				"%w: operation %d: %q is not supported for flat key-value maps",
				errPatchApply, i, op.Op,
			)
		default:
			return nil, fmt.Errorf(
				"%w: operation %d: unknown operation %q",
				errPatchApply,
				i,
				op.Op,
			)
		}
	}
	return result, nil
}
