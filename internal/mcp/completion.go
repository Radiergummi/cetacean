package mcp

import (
	"context"
	"slices"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// maxCompletionValues caps a completion response. The MCP schema documents the
// ceiling as 100 values, and a cluster holds far more tasks than that; the
// count a client is not being shown travels back as Total and HasMore rather
// than being silently dropped.
const maxCompletionValues = 100

// promptArgumentTypes maps a prompt argument to the resource type whose names
// answer it. The prompts declare these arguments as "Name or ID", and a name
// is what an agent can act on, so completion is the difference between picking
// a service and pasting one.
var promptArgumentTypes = map[string]string{
	"service": "services",
	"node":    "nodes",
}

// CompleteResourceArgument offers the names that fill a templated
// cetacean:// URI, implementing mcp-go's ResourceCompletionProvider.
//
// A client rendering cetacean://services/{id} otherwise has nothing to put in
// the blank: the user pastes an ID, or the agent spends a find call to
// discover one.
func (s *Server) CompleteResourceArgument(
	ctx context.Context,
	uri string,
	argument mcplib.CompleteArgument,
	_ mcplib.CompleteContext,
) (*mcplib.Completion, error) {
	path := strings.TrimPrefix(uri, "cetacean://")
	if path == uri {
		return noCompletions(), nil
	}

	resourceType, _, _ := strings.Cut(path, "/")

	return s.completeResourceNames(ctx, resourceType, argument.Value)
}

// CompletePromptArgument offers the names that fill a prompt's argument,
// implementing mcp-go's PromptCompletionProvider.
func (s *Server) CompletePromptArgument(
	ctx context.Context,
	_ string,
	argument mcplib.CompleteArgument,
	_ mcplib.CompleteContext,
) (*mcplib.Completion, error) {
	// The argument name carries the type, and every prompt naming a service
	// means the same thing by it, so the prompt itself does not narrow the
	// answer. An argument that names no resource type completes to nothing.
	return s.completeResourceNames(ctx, promptArgumentTypes[argument.Name], argument.Value)
}

// completeResourceNames is the one body behind both providers: enumerate the
// type the caller may read, keep the names matching what they have typed, and
// bound the result.
//
// The listing goes through lookupResource, the same audited path find and the
// resource reads use, so ACL filtering and secret redaction happen here for
// free. Reading the cache directly would make completion a way to enumerate
// resources the caller cannot otherwise see — a disclosure bug rather than a
// convenience.
func (s *Server) completeResourceNames(
	ctx context.Context,
	resourceType string,
	typed string,
) (*mcplib.Completion, error) {
	if _, ok := pluralToSingularRowType[resourceType]; !ok {
		return noCompletions(), nil
	}

	// Neither of the next two can be reached by a caller: the type was just
	// checked against the same map that decides what enumerates, and a caller
	// whose grants hide the whole type gets an empty slice rather than an
	// error. So both are propagated — swallowing them would hide the one thing
	// that could actually produce them, a resource type wired into one of
	// these maps and not the other.
	listed, err := s.lookupResource(ctx, "cetacean://"+resourceType)
	if err != nil {
		return nil, err
	}

	rows, err := rowsFor(s.cache, resourceType, listed)
	if err != nil {
		return nil, err
	}

	// Substring rather than prefix, matching find's `query`: Docker names
	// carry their stack as a prefix, so a caller typing "prometheus" means
	// "monitoring_prometheus" and a prefix match would offer them nothing.
	needle := strings.ToLower(strings.TrimSpace(typed))

	values := make([]string, 0, len(rows))

	for _, row := range rows {
		if row.Name == "" {
			continue
		}

		if needle != "" && !cluster.ContainsFold(row.Name, needle) {
			continue
		}

		values = append(values, row.Name)
	}

	slices.Sort(values)
	values = slices.Compact(values)

	total := len(values)
	if total > maxCompletionValues {
		values = values[:maxCompletionValues]
	}

	return &mcplib.Completion{
		Values:  values,
		Total:   total,
		HasMore: total > len(values),
	}, nil
}

// noCompletions is the empty answer, spelled once. Values is a non-nil slice
// because the field is required in the response schema and a nil slice
// marshals to null.
func noCompletions() *mcplib.Completion {
	return &mcplib.Completion{Values: []string{}}
}
