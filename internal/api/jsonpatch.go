package api

import (
	"fmt"

	json "github.com/goccy/go-json"
)

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
	for k, v := range m {
		result[k] = v
	}

	for i, op := range ops {
		key := normalizePath(op.Path)
		if key == "" {
			return nil, fmt.Errorf("operation %d: empty path", i)
		}
		switch op.Op {
		case "add":
			result[key] = op.Value
		case "remove":
			if _, ok := result[key]; !ok {
				return nil, fmt.Errorf("operation %d: key %q does not exist", i, key)
			}
			delete(result, key)
		case "replace":
			if _, ok := result[key]; !ok {
				return nil, fmt.Errorf("operation %d: key %q does not exist", i, key)
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
				"operation %d: %q is not supported for flat key-value maps",
				i,
				op.Op,
			)
		default:
			return nil, fmt.Errorf("operation %d: unknown operation %q", i, op.Op)
		}
	}
	return result, nil
}

// applyMergePatchStringMap applies RFC 7396 JSON Merge Patch to a flat string map.
// Keys with null values are deleted; keys with string values are set/overwritten.
func applyMergePatchStringMap(m map[string]string, body []byte) (map[string]string, error) {
	var patch map[string]*string
	if err := json.Unmarshal(body, &patch); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}

	for k, v := range patch {
		if v == nil {
			delete(result, k)
		} else {
			result[k] = *v
		}
	}

	return result, nil
}
