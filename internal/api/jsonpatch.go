package api

import (
	"errors"
	"fmt"
	"maps"
	"strings"
)

// errPatchApply is the sentinel wrapping every patch-application failure from
// applyJSONPatch and parsePatchMutator's merge closure. Callers use errors.Is
// to tell them from writer-side Docker errors, which decides 400/409 over 500.
var errPatchApply = errors.New("patch apply")

// PatchOp represents a single RFC 6902 JSON Patch operation.
type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value string `json:"value,omitempty"`
}

// pointerKey resolves a JSON Pointer (RFC 6901) to the flat-map key it names;
// a leading "/" may be left off. A second token would reach inside a string,
// which has no members. ~1 decodes before ~0, so "~01" is the key "~1".
func pointerKey(p string) (string, error) {
	token := strings.TrimPrefix(p, "/")

	if strings.Contains(token, "/") {
		return "", fmt.Errorf("path %q reaches inside a value; write a / in a key as ~1", p)
	}

	for rest := token; ; {
		_, escaped, found := strings.Cut(rest, "~")
		if !found {
			break
		}
		if escaped == "" || escaped[0] != '0' && escaped[0] != '1' {
			return "", fmt.Errorf("path %q has a ~ that is not ~0 or ~1", p)
		}
		rest = escaped[1:]
	}

	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~"), nil
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
		key, err := pointerKey(op.Path)
		if err != nil {
			return nil, fmt.Errorf("%w: operation %d: %w", errPatchApply, i, err)
		}
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
