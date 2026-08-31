package mcp

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// listableResourceTypes are the resource types list_resources can enumerate.
//
// It is exactly the set lookupResource lists when given no ID, and the tool
// forwards to that same dispatch rather than reading the cache itself — so ACL
// filtering, secret redaction and task enrichment all happen on the one audited
// path. A type added to the resource tree must be added here too, which
// TestListResourcesCoversEveryListableType enforces.
var listableResourceTypes = []string{
	"nodes",
	"services",
	"tasks",
	"stacks",
	"configs",
	"secrets",
	"networks",
	"volumes",
}

// defaultListLimit bounds what a single call pulls into a widget frame. A
// cluster can hold thousands of tasks, and a widget renders in an iframe the
// host sizes; the caller pages with offset.
const defaultListLimit = 200

// listResourcesResult is the envelope the table widget renders.
//
// The widget is built and shipped separately from the server and cannot be
// type-checked against it, so this shape is the contract between the two and is
// advertised as the tool's outputSchema. Total is the count *before* paging, so
// a widget can say "showing 200 of 1,432" without a second call.
type listResourcesResult struct {
	Type  string `json:"type"`
	Items []any  `json:"items"`
	Total int    `json:"total"`
}

// toolListResources enumerates one resource type for the table widget.
func (s *Server) toolListResources(
	ctx context.Context,
	req mcplib.CallToolRequest,
) (string, error) {
	resourceType, err := req.RequireString("type")
	if err != nil {
		return "", err
	}

	if !slices.Contains(listableResourceTypes, resourceType) {
		return "", fmt.Errorf(
			"unknown resource type %q; expected one of %v",
			resourceType, listableResourceTypes,
		)
	}

	// Reuse the resource dispatch rather than the cache directly: it is where
	// ACL filtering and redaction live, and duplicating it here would be a
	// second path to keep in step.
	listed, err := s.lookupResource(ctx, "cetacean://"+resourceType)
	if err != nil {
		return "", err
	}

	items, ok := toSlice(listed)
	if !ok {
		return "", fmt.Errorf("resource type %q does not enumerate", resourceType)
	}

	total := len(items)

	offset := min(max(req.GetInt("offset", 0), 0), total)

	limit := req.GetInt("limit", defaultListLimit)
	if limit <= 0 || limit > defaultListLimit {
		limit = defaultListLimit
	}

	items = items[offset:min(offset+limit, total)]

	return marshalResult(listResourcesResult{
		Type:  resourceType,
		Items: items,
		Total: total,
	})
}

// toSlice widens any slice to []any. lookupResource returns a differently typed
// slice per resource ([]swarm.Node, []swarm.Service, ...), and the envelope is
// uniform; reflection keeps this one function rather than eight type switches
// that a new resource type could silently miss.
func toSlice(v any) ([]any, bool) {
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Slice {
		return nil, false
	}

	out := make([]any, value.Len())
	for i := range out {
		out[i] = value.Index(i).Interface()
	}

	return out, true
}
