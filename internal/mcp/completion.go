package mcp

import (
	"context"
	"slices"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// maxCompletionValues caps a completion response. The MCP schema documents the
// ceiling as 100 values, and a cluster holds far more tasks than that; the
// count a client is not being shown travels back as Total and HasMore rather
// than being silently dropped.
const maxCompletionValues = 100

// CompleteResourceArgument offers the names that fill a templated cetacean://
// URI, implementing mcp-go's ResourceCompletionProvider. Without it a client
// rendering cetacean://services/{id} has nothing to put in the blank.
func (s *Server) CompleteResourceArgument(
	ctx context.Context,
	uri string,
	argument mcplib.CompleteArgument,
	_ mcplib.CompleteContext,
) (*mcplib.Completion, error) {
	// No guard on the scheme: a URI that is not a cetacean:// one cuts to
	// something that is not a resource type, which completes to nothing by
	// the same path an unknown type does.
	resourceType, _, _ := strings.Cut(strings.TrimPrefix(uri, "cetacean://"), "/")

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
	// A prompt names its arguments after the resource type they take, so the
	// map describe already derives answers this too; a second table here would
	// leave a prompt gaining a `stack` argument completing to nothing. An
	// argument naming no resource type yields nothing, as an unknown key does.
	return s.completeResourceNames(
		ctx,
		describableResourceTypes[argument.Name],
		argument.Value,
	)
}

// completeResourceNames is the one body behind both providers: enumerate the
// type, keep the names matching what was typed, bound the result. The listing
// goes through lookupResource, so ACL filtering and secret redaction apply for
// free — reading the cache directly would make completion an enumeration bug.
func (s *Server) completeResourceNames(
	ctx context.Context,
	resourceType string,
	typed string,
) (*mcplib.Completion, error) {
	if _, ok := pluralToSingularRowType[resourceType]; !ok {
		// Values is a non-nil slice because the field is required in the
		// response schema and a nil slice marshals to null.
		return &mcplib.Completion{Values: []string{}}, nil
	}

	// Neither of the next two is reachable by a caller: the type was just
	// checked against the map that decides what enumerates, and hidden grants
	// yield an empty slice. Both are propagated anyway, since the one thing
	// that produces them is a type wired into one map and not the other.
	listed, err := s.lookupResource(ctx, "cetacean://"+resourceType)
	if err != nil {
		return nil, err
	}

	rows, err := s.rowsFor(ctx, resourceType, listed)
	if err != nil {
		return nil, err
	}

	// find's own filter, rather than a second substring rule beside it: a
	// completion narrows by exactly what `find` means by `query`, and the two
	// would drift the moment that rule gains a case.
	rows = filterRows(rows, rowFilters{query: strings.TrimSpace(typed)}, nil)

	values := make([]string, 0, len(rows))

	for _, row := range rows {
		if row.Name == "" {
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
