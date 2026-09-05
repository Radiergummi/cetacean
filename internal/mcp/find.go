package mcp

import (
	"context"
	"fmt"
	"maps"
	"reflect"
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
// `type` argument.
//
// It is exactly the set lookupResource lists when given no ID, and the tool
// forwards to that same dispatch rather than reading the cache itself — so ACL
// filtering, secret redaction and task enrichment all happen on the one audited
// path. A type added to the resource tree must be added here too, which
// TestFindCoversEveryListableType enforces.
var listableResourceTypes = slices.Sorted(maps.Keys(pluralToSingularRowType))

// defaultListLimit bounds what a single call pulls into a widget frame. A
// cluster can hold thousands of tasks, and a widget renders in an iframe the
// host sizes; the caller pages with offset.
const defaultListLimit = 200

// pluralToSingularRowType maps a listable resource type's plural key (matching
// cetacean:// resource URIs) to the singular cluster.Row.Type it produces.
// cluster.Row follows TopologyNode's singular convention; the resource tree
// does not, so a cross-type search — which tags each row with its own type
// rather than grouping by a map key — needs the translation.
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

// findResult is the envelope for a list of resources.
//
// Total is the count before paging and after filtering, so a caller can say
// "showing 200 of 1,432" without a second call.
type findResult struct {
	Type  string        `json:"type"`
	Items []cluster.Row `json:"items"`
	Total int           `json:"total"`
}

// findRawResult is find's `raw: true` envelope: the untouched resource records
// (the same shape lookupResource already returns for widgets and resources)
// rather than the compact Row. It exists for a caller that needs a field Row
// does not carry, and is never advertised as an output schema — the tool
// always advertises findResult, because the compact shape is what a
// well-behaved caller gets by default. That leaves this result deliberately
// non-conforming to the schema the client was told to expect, so toolFind
// calls markTextOnlyResult before returning it, and registerTools skips the
// structuredContent wrapper rather than shipping a payload that would fail
// the server's own output-schema validation.
type findRawResult struct {
	Type  string `json:"type"`
	Items []any  `json:"items"`
	Total int    `json:"total"`
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

	// A type test, not a conversion: the raw path projects filtered rows back
	// onto the concrete slice by ID, so nothing needs it widened to []any.
	if reflect.ValueOf(listed).Kind() != reflect.Slice {
		return "", fmt.Errorf("resource type %q does not enumerate", resourceType)
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

	if req.GetBool("raw", false) {
		// raw changes the shape of what comes back, never the scope: a
		// caller who asked for one stack's worth of services must not
		// silently get every service back because raw skipped the filters
		// that shape would have applied. Project the *filtered* rows' IDs
		// back onto the untouched records — never pair the two slices by
		// index, since rowsFor's builders each sort their own output and
		// positional correspondence with `listed` does not hold.
		byID := rawItemsByID(listed)

		items := make([]any, 0, len(rows))
		for _, row := range rows {
			if item, ok := byID[row.ID]; ok {
				items = append(items, item)
			}
		}

		total := len(items)
		items = paginate(items, req)

		// findRawResult is not the shape find's outputSchema describes (that
		// is findResult, the compact Row list) — presenting it as
		// structuredContent would fail the server's output-schema
		// validation on the very call that asked for the untouched record.
		markTextOnlyResult(ctx)

		return marshalResult(findRawResult{Type: resourceType, Items: items, Total: total})
	}

	total := len(rows)
	rows = paginate(rows, req)

	return marshalResult(findResult{Type: resourceType, Items: rows, Total: total})
}

// findAcrossTypes searches every listable resource type by name, label or
// image reference, the way the tool find replaced did with no `type` given.
// Each result already carries its own singular Type, so — unlike
// cluster.SearchResults, which groups hits under a per-type map key — the
// rows can be returned as one flat, sorted list.
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

	return marshalResult(findResult{Items: rows, Total: results.Total})
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

// rowsFor converts the slice lookupResource returned for resourceType into
// the compact Row shape, by calling the one cluster.RowsFor* builder that
// knows that type. The type assertions mirror exactly what each
// lookupResource branch returns, so a mismatch here is a bug in this
// function, not a caller error — hence the error message names both sides.
//
// It takes the context because a row can name a resource other than the one it
// describes — a task row names its parent service and the node it runs on — and
// those names have to pass the caller's read grants first, exactly as
// digestOf's do.
func (s *Server) rowsFor(
	ctx context.Context,
	resourceType string,
	listed any,
) ([]cluster.Row, error) {
	c := s.cache

	switch resourceType {
	case "services":
		items, ok := listed.([]swarm.Service)
		if !ok {
			return nil, fmt.Errorf("find: services returned %T, not []swarm.Service", listed)
		}

		return cluster.RowsForServices(items, c.RunningTaskCounts()), nil

	case "nodes":
		items, ok := listed.([]swarm.Node)
		if !ok {
			return nil, fmt.Errorf("find: nodes returned %T, not []swarm.Node", listed)
		}

		return cluster.RowsForNodes(items), nil

	case "tasks":
		items, ok := listed.([]cluster.EnrichedTask)
		if !ok {
			return nil, fmt.Errorf("find: tasks returned %T, not []cluster.EnrichedTask", listed)
		}

		// Both parent listings are ACL-filtered before the builder sees them:
		// a task row that named a service or node the caller may not read
		// would make find a way around the grants describe honours.
		return cluster.RowsForTasks(
			items,
			s.filterServices(ctx, c.ListServices()),
			s.filterNodes(ctx, c.ListNodes()),
		), nil

	case "stacks":
		items, ok := listed.([]cache.Stack)
		if !ok {
			return nil, fmt.Errorf("find: stacks returned %T, not []cache.Stack", listed)
		}

		return cluster.RowsForStacks(items), nil

	case "configs":
		items, ok := listed.([]swarm.Config)
		if !ok {
			return nil, fmt.Errorf("find: configs returned %T, not []swarm.Config", listed)
		}

		return cluster.RowsForConfigs(items), nil

	case "secrets":
		items, ok := listed.([]swarm.Secret)
		if !ok {
			return nil, fmt.Errorf("find: secrets returned %T, not []swarm.Secret", listed)
		}

		return cluster.RowsForSecrets(items), nil

	case "networks":
		items, ok := listed.([]network.Summary)
		if !ok {
			return nil, fmt.Errorf("find: networks returned %T, not []network.Summary", listed)
		}

		return cluster.RowsForNetworks(items), nil

	case "volumes":
		items, ok := listed.([]volume.Volume)
		if !ok {
			return nil, fmt.Errorf("find: volumes returned %T, not []volume.Volume", listed)
		}

		ptrs := make([]*volume.Volume, len(items))
		for i := range items {
			ptrs[i] = &items[i]
		}

		return cluster.RowsForVolumes(ptrs), nil

	default:
		// Unreachable given the listableResourceTypes check in toolFind, but
		// named rather than panicking if the two ever drift.
		return nil, fmt.Errorf("resource type %q has no row builder", resourceType)
	}
}

// rowFilters narrows a Row list after it is built — a caller who already
// knows roughly what they want (a stack, a state, an image) filters without
// walking the whole type themselves. Only meaningful once `type` is given:
// findAcrossTypes does not apply these, since a cross-type hit does not carry
// enough of the underlying record to test most of them.
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

// labelsFor collects each row's label map, keyed by the ID cluster.Row uses,
// from the raw slice lookupResource returned. cluster.Row does not carry
// labels — it is the compact shape find hands back — so the `label` filter
// reads them from the pre-conversion record instead. Types with nothing
// resembling a label (stacks) are simply absent from the result, and
// labelMatches treats a missing entry as no match.
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

// rawItemsByID indexes the raw slice lookupResource returned by the same ID
// cluster.Row.ID carries for each entry — the same per-type identity
// labelsFor keys by, with cache.Stack added since a stack has no labels to
// collect but still needs an identity for raw mode to filter by. This is what
// lets raw mode filter by the compact Row's identity and hand back the
// untouched record: pairing the two slices by index would not work, since
// every RowsFor* builder sorts its own output.
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

// paginate slices items to the `offset`/`limit` window a caller asked for,
// clamping both to the slice bounds and to defaultListLimit. Generic so the
// same bounds logic serves both the raw ([]any) and compact ([]cluster.Row)
// results.
func paginate[T any](items []T, req mcplib.CallToolRequest) []T {
	total := len(items)

	offset := min(max(req.GetInt("offset", 0), 0), total)

	limit := req.GetInt("limit", defaultListLimit)
	if limit <= 0 || limit > defaultListLimit {
		limit = defaultListLimit
	}

	return items[offset:min(offset+limit, total)]
}
