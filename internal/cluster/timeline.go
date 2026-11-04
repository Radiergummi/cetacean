package cluster

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/radiergummi/cetacean/internal/cache"
)

// ErrNoGrant refuses a cluster-wide read to a caller holding no grant at all.
var ErrNoGrant = errors.New("no grants found for this identity")

// TimelineScope is a timeline read as both transports take it: Type names
// what Resource is, Types narrows to entries of those types, and one entry in
// Types with no Type is read as the Type. An empty filter value is no filter.
type TimelineScope struct {
	Window   TimeWindow
	Type     cache.EventType
	Types    []cache.EventType
	Resource string

	// Tasks widens a service's timeline to its tasks' entries.
	Tasks bool

	Search   string
	Kinds    []string
	Tags     []string
	Authors  []string
	MatchAny bool
}

// Query resolves s for caller into the query cluster.ReadTimeline takes. An
// ambiguous name comes back as a *cache.AmbiguousNameError only when the
// caller may read it; otherwise it is read as given, and so finds nothing.
func (s TimelineScope) Query(c *cache.Cache, caller AnnotationCaller) (cache.HistoryQuery, error) {
	if !caller.AnyGrant {
		return cache.HistoryQuery{}, ErrNoGrant
	}

	types := slices.DeleteFunc(
		slices.Clone(s.Types),
		func(t cache.EventType) bool { return t == "" },
	)
	if s.Type == "" && len(types) == 1 {
		s.Type, types = types[0], nil
	}

	kinds := nonEmpty(s.Kinds)
	for _, kind := range kinds {
		if !slices.Contains(cache.Kinds, kind) {
			return cache.HistoryQuery{}, fmt.Errorf(
				"kind %q is not one of %s", kind, strings.Join(cache.Kinds, ", "),
			)
		}
	}

	resourceID, err := ResolveIdentifier(c, string(s.Type), s.Resource, caller.CanRead)
	if ambiguous, ok := errors.AsType[*cache.AmbiguousNameError](err); ok &&
		!caller.CanRead(string(s.Type)+":"+ambiguous.Name) {
		resourceID, err = s.Resource, nil
	}
	if err != nil {
		return cache.HistoryQuery{}, err
	}

	query := cache.HistoryQuery{
		Type:       s.Type,
		Types:      types,
		ResourceID: resourceID,
		After:      s.Window.Since,
		Before:     s.Window.Until,
		Search:     s.Search,
		Kinds:      kinds,
		Tags:       nonEmpty(s.Tags),
		Authors:    nonEmpty(s.Authors),
		MatchAny:   s.MatchAny,
		Visible:    HistoryReadable(caller.CanRead),

		ReadableAnnotation: caller.MayRead,
	}

	if s.Tasks && query.Type == cache.EventService && query.ResourceID != "" {
		query.Type, query.Types = "", []cache.EventType{cache.EventService, cache.EventTask}
	}

	return query, nil
}

func nonEmpty(values []string) []string {
	return slices.DeleteFunc(slices.Clone(values), func(v string) bool { return v == "" })
}
