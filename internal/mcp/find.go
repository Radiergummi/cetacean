package mcp

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"
	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// listableResourceTypes are the resource types find can enumerate with a
// `type` argument: exactly the set lookupResource lists when given no ID. find
// forwards to that same dispatch rather than reading the cache, so ACL
// filtering, secret redaction and task enrichment stay on one audited path.
var listableResourceTypes = slices.Sorted(maps.Keys(pluralToSingularRowType))

// defaultListLimit bounds what a single call pulls into a widget frame. A
// cluster can hold thousands of tasks, and a widget renders in an iframe the
// host sizes; the caller pages with offset.
const defaultListLimit = 200

// pluralToSingularRowType maps a listable type's plural key, as the cetacean://
// URIs spell it, to the singular cluster.Row.Type it produces. A cross-type
// search tags each row with its own type rather than grouping by a map key,
// so it needs the translation.
var pluralToSingularRowType = map[string]string{
	"nodes":    "node",
	"services": "service",
	"tasks":    "task",
	"stacks":   "stack",
	"configs":  "config",
	"secrets":  "secret",
	"networks": "network",
	"volumes":  "volume",
}

// findResult is the envelope for a list of resources. Total counts after
// filtering and before paging; Counts is the per-type breakdown, present only
// on a cross-type search. Raw rides beside the rows rather than replacing
// them: a tool advertising an output schema must conform to it every call.
type findResult struct {
	Type   string         `json:"type"`
	Items  []cluster.Row  `json:"items"`
	Total  int            `json:"total"`
	Counts map[string]int `json:"counts,omitempty"`
	Raw    []any          `json:"raw,omitempty"`
}

// toolFind locates cluster resources: enumerate one type (optionally narrowed
// by the post-filters), or — when `type` is omitted — search by name, label
// or image reference across every type at once, the way the tool it replaced
// did.
func (s *Server) toolFind(ctx context.Context, req mcplib.CallToolRequest) (string, error) {
	resourceType := strings.TrimSpace(req.GetString("type", ""))
	query := strings.TrimSpace(req.GetString("query", ""))

	if resourceType == "" {
		return s.findAcrossTypes(ctx, req, query)
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

	rows, err := s.rowsFor(ctx, resourceType, listed)
	if err != nil {
		return "", err
	}

	filters := rowFilters{
		query: query,
		state: strings.TrimSpace(req.GetString("state", "")),
		stack: strings.TrimSpace(req.GetString("stack", "")),
		node:  strings.TrimSpace(req.GetString("node", "")),
		image: strings.TrimSpace(req.GetString("image", "")),
		label: strings.TrimSpace(req.GetString("label", "")),
	}

	// Only the label filter reads the labels, and it is the rarest of the six;
	// building the map unconditionally walked every record on every listing.
	// labelMatches answers false for a nil map, so nil is a valid absence.
	var labels map[string]map[string]string
	if filters.label != "" {
		labels = labelsFor(listed)
	}

	rows = filterRows(rows, filters, labels)

	total := len(rows)
	rows = paginate(rows, req)

	attachResourceLinks(ctx, resourceLinksForRows(rows))

	result := findResult{Type: resourceType, Items: rows, Total: total}

	// raw changes what the answer carries, never its scope. Project the
	// returned rows' IDs back onto the untouched records: never pair the two
	// slices by index, since each RowsFor* builder sorts its own output.
	if req.GetBool("raw", false) {
		byID := rawItemsByID(listed)

		result.Raw = make([]any, 0, len(rows))
		for _, row := range rows {
			if item, ok := byID[row.ID]; ok {
				result.Raw = append(result.Raw, item)
			}
		}
	}

	return marshalResult(result)
}

// findAcrossTypes searches every listable resource type by name, label or
// image reference. Each hit carries its own singular Type, so — unlike
// cluster.SearchResults, which groups under a per-type map key — the rows
// return as one flat, sorted list.
func (s *Server) findAcrossTypes(
	ctx context.Context,
	req mcplib.CallToolRequest,
	query string,
) (string, error) {
	if query == "" {
		return "", fmt.Errorf("`query` is required when `type` is omitted")
	}

	limit := req.GetInt("limit", 3)

	results := s.filterSearchResults(ctx, cluster.Search(ctx, s.cache, query, limit))

	// Sized by the hits actually held, not by results.Total: Total is the
	// pre-cap count across the whole cluster, so on a broad query it would
	// reserve thousands of rows to hold the handful `limit` let through.
	capacity := 0
	for _, hits := range results.Hits {
		capacity += len(hits)
	}

	rows := make([]cluster.Row, 0, capacity)

	for pluralType, hits := range results.Hits {
		for _, hit := range hits {
			rows = append(rows, cluster.Row{
				ID:     hit.ID,
				Name:   hit.Name,
				Type:   pluralToSingularRowType[pluralType],
				State:  hit.State,
				Detail: hit.Detail,
			})
		}
	}

	sortFindRows(rows)

	attachResourceLinks(ctx, resourceLinksForRows(rows))

	return marshalResult(findResult{
		Items:  rows,
		Total:  results.Total,
		Counts: results.Counts,
	})
}

// sortFindRows puts a cross-type row list into a stable order. Rows are built
// by ranging over results.Hits, a map, whose iteration order is not
// guaranteed, and the result is marshalled into an MCP result a client may
// cache by ETag.
func sortFindRows(rows []cluster.Row) {
	slices.SortFunc(rows, func(a, b cluster.Row) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})
}

// rowsFor converts what lookupResource returned into Rows, via the one
// cluster.RowsFor* builder that knows the type. It dispatches on the concrete
// slice type, which already carries the answer. The context is needed because
// a row can name a parent, which must pass the caller's read grants first.
func (s *Server) rowsFor(
	ctx context.Context,
	resourceType string,
	listed any,
) ([]cluster.Row, error) {
	c := s.cache

	switch items := listed.(type) {
	case []swarm.Service:
		return cluster.RowsForServices(items, c.RunningTaskCounts()), nil

	case []swarm.Node:
		return cluster.RowsForNodes(items), nil

	case []cluster.EnrichedTask:
		// The services are ACL-filtered before the builder sees them, and the
		// tasks arrive already enriched from an equally filtered node listing:
		// a task row that named a service or node the caller may not read
		// would make find a way around the grants describe honours.
		return cluster.RowsForTasks(items, s.filterServices(ctx, c.ListServices())), nil

	case []cache.Stack:
		return cluster.RowsForStacks(items), nil

	case []swarm.Config:
		return cluster.RowsForConfigs(items), nil

	case []swarm.Secret:
		return cluster.RowsForSecrets(items), nil

	case []network.Summary:
		return cluster.RowsForNetworks(items), nil

	case []volume.Volume:
		return cluster.RowsForVolumes(items), nil

	default:
		// Reached only if listableResourceTypes and the branches above drift,
		// or if a listing returns something that is not a slice at all — named
		// rather than panicking either way.
		return nil, fmt.Errorf(
			"resource type %q has no row builder for %T", resourceType, listed,
		)
	}
}

// rowFilters narrows a Row list after it is built. Only meaningful once `type`
// is given: findAcrossTypes does not apply these, since a cross-type hit does
// not carry enough of the underlying record to test most of them.
type rowFilters struct {
	query string
	state string
	stack string
	node  string
	image string
	label string
}

func (f rowFilters) empty() bool {
	return f.query == "" && f.state == "" && f.stack == "" &&
		f.node == "" && f.image == "" && f.label == ""
}

// filterRows applies every non-empty filter in f, keeping a row only if it
// matches all of them. labels supplies label data per row ID for the `label`
// filter, since cluster.Row itself carries no labels.
func filterRows(
	rows []cluster.Row,
	f rowFilters,
	labels map[string]map[string]string,
) []cluster.Row {
	if f.empty() {
		return rows
	}

	out := make([]cluster.Row, 0, len(rows))

	// ContainsFold documents its second argument as already lowered, and these
	// three do not change across the loop.
	query := strings.ToLower(f.query)
	node := strings.ToLower(f.node)
	image := strings.ToLower(f.image)

	for _, row := range rows {
		if f.query != "" && !cluster.ContainsFold(row.Name, query) {
			continue
		}

		if f.state != "" && !strings.EqualFold(row.State, f.state) {
			continue
		}

		if f.stack != "" && !strings.EqualFold(row.Stack, f.stack) {
			continue
		}

		// node and image both test Detail: it holds the node hostname for a
		// task row and the image for a service row, so each filter is only
		// meaningful for the type that put it there.
		if f.node != "" && !cluster.ContainsFold(row.Detail, node) {
			continue
		}

		if f.image != "" && !cluster.ContainsFold(row.Detail, image) {
			continue
		}

		if f.label != "" && !labelMatches(labels[row.ID], f.label) {
			continue
		}

		out = append(out, row)
	}

	return out
}

// labelMatches reports whether labels satisfies filter, using Docker's own
// label-filter syntax: "key" tests presence, "key=value" tests an exact value.
func labelMatches(labels map[string]string, filter string) bool {
	if labels == nil {
		return false
	}

	key, value, hasValue := strings.Cut(filter, "=")

	got, ok := labels[key]
	if !ok {
		return false
	}

	if !hasValue {
		return true
	}

	return got == value
}

// labelsFor collects each row's labels from the raw slice, keyed by the ID
// cluster.Row uses, since the compact Row does not carry them. A type with
// nothing resembling a label is absent, and labelMatches reads a missing
// entry as no match.
func labelsFor(listed any) map[string]map[string]string {
	out := map[string]map[string]string{}

	switch items := listed.(type) {
	case []swarm.Service:
		for _, svc := range items {
			out[svc.ID] = svc.Spec.Labels
		}

	case []swarm.Node:
		for _, n := range items {
			out[n.ID] = n.Spec.Labels
		}

	case []cluster.EnrichedTask:
		for _, t := range items {
			if t.Spec.ContainerSpec != nil {
				out[t.ID] = t.Spec.ContainerSpec.Labels
			}
		}

	case []swarm.Config:
		for _, cfg := range items {
			out[cfg.ID] = cfg.Spec.Labels
		}

	case []swarm.Secret:
		for _, sec := range items {
			out[sec.ID] = sec.Spec.Labels
		}

	case []network.Summary:
		for _, n := range items {
			out[n.ID] = n.Labels
		}

	case []volume.Volume:
		for _, v := range items {
			out[v.Name] = v.Labels
		}
	}

	return out
}

// rawItemsByID indexes the raw slice by the ID cluster.Row.ID carries — the
// identity labelsFor keys by, plus stacks, which have no labels but still need
// one. It is what lets raw mode filter by the Row's identity and return the
// untouched record, since every RowsFor* builder sorts its own output.
func rawItemsByID(listed any) map[string]any {
	out := map[string]any{}

	switch items := listed.(type) {
	case []swarm.Service:
		for _, svc := range items {
			out[svc.ID] = svc
		}

	case []swarm.Node:
		for _, n := range items {
			out[n.ID] = n
		}

	case []cluster.EnrichedTask:
		for _, t := range items {
			out[t.ID] = t
		}

	case []cache.Stack:
		for _, st := range items {
			out[st.Name] = st
		}

	case []swarm.Config:
		for _, cfg := range items {
			out[cfg.ID] = cfg
		}

	case []swarm.Secret:
		for _, sec := range items {
			out[sec.ID] = sec
		}

	case []network.Summary:
		for _, n := range items {
			out[n.ID] = n
		}

	case []volume.Volume:
		for _, v := range items {
			out[v.Name] = v
		}
	}

	return out
}

// paginate slices rows to the `offset`/`limit` window a caller asked for,
// clamping both to the slice bounds and to defaultListLimit.
func paginate(items []cluster.Row, req mcplib.CallToolRequest) []cluster.Row {
	total := len(items)

	offset := min(max(req.GetInt("offset", 0), 0), total)

	limit := req.GetInt("limit", defaultListLimit)
	if limit <= 0 || limit > defaultListLimit {
		limit = defaultListLimit
	}

	return items[offset:min(offset+limit, total)]
}
