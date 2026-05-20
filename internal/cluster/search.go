package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/radiergummi/cetacean/internal/cache"
)

// SearchResult is a single hit from a global search.
type SearchResult struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	State  string `json:"state,omitempty"`
}

// SearchResults is the structured result of a global search.
//
// Hits is keyed by resource type plural ("services", "nodes", ...) and is
// capped at `limit` per type. Counts is the pre-cap total of matches per type
// — clients use Counts to render "X matches, showing N" affordances. Total is
// the sum of Counts.
type SearchResults struct {
	Hits   map[string][]SearchResult
	Counts map[string]int
	Total  int
}

// Search returns matches across all swarm resource types.
//
// Each per-type slice in Hits is capped at limit (0 means up to 1000), while
// Counts always reports the pre-cap total so callers can show "X matches" even
// when displaying a small subset. Secret data is never returned; RedactSecret
// is applied where applicable.
func Search(ctx context.Context, c *cache.Cache, query string, limit int) SearchResults {
	if limit == 0 || limit > 1000 {
		limit = 1000
	}

	ql := strings.ToLower(query)

	const (
		stServices = iota
		stStacks
		stNodes
		stTasks
		stConfigs
		stSecrets
		stNetworks
		stVolumes
		stCount
	)
	type typeResults struct {
		key     string
		results []SearchResult
		count   int
	}
	var allResults [stCount]typeResults

	services := c.ListServices()
	svcNames := make(map[string]string, len(services))
	for _, s := range services {
		svcNames[s.ID] = s.Spec.Name
	}

	var wg sync.WaitGroup
	wg.Add(stCount)

	// Services
	go func() {
		defer wg.Done()
		var matches []SearchResult
		count := 0
		for _, s := range services {
			if ctx.Err() != nil {
				return
			}
			hit := ContainsFold(s.Spec.Name, ql)
			if !hit && s.Spec.TaskTemplate.ContainerSpec != nil {
				hit = ContainsFold(s.Spec.TaskTemplate.ContainerSpec.Image, ql)
			}
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if !hit {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			detail := ""
			if s.Spec.TaskTemplate.ContainerSpec != nil {
				detail = s.Spec.TaskTemplate.ContainerSpec.Image
				if i := strings.Index(detail, "@sha256:"); i > 0 {
					detail = detail[:i]
				}
			}
			running := c.RunningTaskCount(s.ID)
			matches = append(matches, SearchResult{
				Type:   "services",
				ID:     s.ID,
				Name:   s.Spec.Name,
				Detail: detail,
				State:  DeriveServiceState(s, running),
			})
		}
		allResults[stServices] = typeResults{"services", matches, count}
	}()

	// Stacks
	go func() {
		defer wg.Done()
		stacks := c.ListStacks()
		var matches []SearchResult
		count := 0
		for _, s := range stacks {
			if ctx.Err() != nil {
				return
			}
			if !ContainsFold(s.Name, ql) {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			matches = append(matches, SearchResult{
				Type:   "stacks",
				ID:     s.Name,
				Name:   s.Name,
				Detail: fmt.Sprintf("%d services", len(s.Services)),
			})
		}
		allResults[stStacks] = typeResults{"stacks", matches, count}
	}()

	// Nodes
	go func() {
		defer wg.Done()
		nodes := c.ListNodes()
		var matches []SearchResult
		count := 0
		for _, n := range nodes {
			if ctx.Err() != nil {
				return
			}
			hit := ContainsFold(n.Description.Hostname, ql)
			if !hit {
				hit = ContainsFold(n.Status.Addr, ql)
			}
			if !hit {
				hit = labelsMatch(n.Spec.Labels, ql)
			}
			if !hit {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			matches = append(matches, SearchResult{
				Type:   "nodes",
				ID:     n.ID,
				Name:   n.Description.Hostname,
				Detail: fmt.Sprintf("%s, %s", n.Spec.Role, n.Status.State),
			})
		}
		allResults[stNodes] = typeResults{"nodes", matches, count}
	}()

	// Tasks
	go func() {
		defer wg.Done()
		tasks := c.ListTasks()
		var matches []SearchResult
		count := 0
		for _, t := range tasks {
			if ctx.Err() != nil {
				return
			}
			svcName := svcNames[t.ServiceID]
			taskName := fmt.Sprintf("%s.%d", svcName, t.Slot)

			hit := ContainsFold(svcName, ql)
			if !hit && t.Spec.ContainerSpec != nil {
				hit = ContainsFold(t.Spec.ContainerSpec.Image, ql)
			}
			if !hit && t.Spec.ContainerSpec != nil {
				hit = labelsMatch(t.Spec.ContainerSpec.Labels, ql)
			}
			if !hit {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			detail := ""
			if t.Spec.ContainerSpec != nil {
				detail = t.Spec.ContainerSpec.Image
				if i := strings.Index(detail, "@sha256:"); i > 0 {
					detail = detail[:i]
				}
			}
			matches = append(matches, SearchResult{
				Type:   "tasks",
				ID:     t.ID,
				Name:   taskName,
				Detail: detail,
				State:  string(t.Status.State),
			})
		}
		allResults[stTasks] = typeResults{"tasks", matches, count}
	}()

	// Configs
	go func() {
		defer wg.Done()
		configs := c.ListConfigs()
		var matches []SearchResult
		count := 0
		for _, cfg := range configs {
			if ctx.Err() != nil {
				return
			}
			hit := ContainsFold(cfg.Spec.Name, ql)
			if !hit {
				hit = labelsMatch(cfg.Spec.Labels, ql)
			}
			if !hit {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			matches = append(matches, SearchResult{
				Type:   "configs",
				ID:     cfg.ID,
				Name:   cfg.Spec.Name,
				Detail: cfg.CreatedAt.Format(time.RFC3339),
			})
		}
		allResults[stConfigs] = typeResults{"configs", matches, count}
	}()

	// Secrets
	go func() {
		defer wg.Done()
		secrets := c.ListSecrets()
		var matches []SearchResult
		count := 0
		for _, s := range secrets {
			if ctx.Err() != nil {
				return
			}
			s = RedactSecret(s)
			hit := ContainsFold(s.Spec.Name, ql)
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if !hit {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			matches = append(matches, SearchResult{
				Type:   "secrets",
				ID:     s.ID,
				Name:   s.Spec.Name,
				Detail: s.CreatedAt.Format(time.RFC3339),
			})
		}
		allResults[stSecrets] = typeResults{"secrets", matches, count}
	}()

	// Networks
	go func() {
		defer wg.Done()
		networks := c.ListNetworks()
		var matches []SearchResult
		count := 0
		for _, n := range networks {
			if ctx.Err() != nil {
				return
			}
			hit := ContainsFold(n.Name, ql)
			if !hit {
				hit = labelsMatch(n.Labels, ql)
			}
			if !hit {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			matches = append(matches, SearchResult{
				Type:   "networks",
				ID:     n.ID,
				Name:   n.Name,
				Detail: n.Driver,
			})
		}
		allResults[stNetworks] = typeResults{"networks", matches, count}
	}()

	// Volumes
	go func() {
		defer wg.Done()
		volumes := c.ListVolumes()
		var matches []SearchResult
		count := 0
		for _, v := range volumes {
			if ctx.Err() != nil {
				return
			}
			hit := ContainsFold(v.Name, ql)
			if !hit {
				hit = labelsMatch(v.Labels, ql)
			}
			if !hit {
				continue
			}
			count++
			if len(matches) >= limit {
				continue
			}
			matches = append(matches, SearchResult{
				Type:   "volumes",
				ID:     v.Name,
				Name:   v.Name,
				Detail: v.Driver,
			})
		}
		allResults[stVolumes] = typeResults{"volumes", matches, count}
	}()

	wg.Wait()

	out := SearchResults{
		Hits:   make(map[string][]SearchResult, stCount),
		Counts: make(map[string]int, stCount),
	}
	for _, tr := range allResults {
		if tr.count == 0 {
			continue
		}
		out.Hits[tr.key] = tr.results
		out.Counts[tr.key] = tr.count
		out.Total += tr.count
	}
	return out
}

// ContainsFold reports whether s contains substr using case-insensitive
// comparison, or whether the query matches segment prefixes of s.
// substr must already be lowercased.
func ContainsFold(s, substrLower string) bool {
	if containsFoldNoAlloc(s, substrLower) {
		return true
	}

	// Segment-prefix matching requires lowercased input; only allocate if
	// the string actually contains separators (otherwise SegmentPrefixMatch
	// returns false for single-segment targets anyway).
	if !strings.ContainsAny(s, "_-") {
		return false
	}

	return SegmentPrefixMatch(strings.ToLower(s), substrLower)
}

// containsFoldNoAlloc reports whether s contains substr (which must be
// lowercased) using case-insensitive comparison without allocating.
// Only handles ASCII case folding; non-ASCII letters are compared as-is.
func containsFoldNoAlloc(s, substrLower string) bool {
	if len(substrLower) == 0 {
		return true
	}

	if len(substrLower) > len(s) {
		return false
	}

	for i := 0; i <= len(s)-len(substrLower); i++ {
		match := true

		for j := 0; j < len(substrLower); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}

			if c != substrLower[j] {
				match = false
				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

var separatorReplacer = strings.NewReplacer("_", "", "-", "")

func isSeparator(r rune) bool { return r == '_' || r == '-' }

// SegmentPrefixMatch checks if query matches target using segment-prefix
// matching. The target is split by '_' and '-' into segments, and each group
// of query characters must match the prefix of a segment, in order, with
// segments skippable. Uses memoized backtracking for ambiguous boundaries.
//
// Both arguments must already be lowercased.
func SegmentPrefixMatch(targetLower, queryLower string) bool {
	if len(queryLower) == 0 {
		return true
	}

	// Strip separators from query (user may type "go_gc" meaning "go" + "gc")
	query := separatorReplacer.Replace(queryLower)
	if len(query) == 0 {
		return true
	}

	segments := strings.FieldsFunc(targetLower, isSeparator)

	// Single-segment targets are already covered by substring match in ContainsFold
	if len(segments) <= 1 {
		return false
	}

	type key struct{ qi, si int }
	memo := map[key]bool{}

	var match func(qi, si int) bool
	match = func(qi, si int) bool {
		if qi >= len(query) {
			return true
		}

		if si >= len(segments) {
			return false
		}

		k := key{qi, si}
		if v, ok := memo[k]; ok {
			return v
		}

		result := false
		for s := si; s < len(segments) && !result; s++ {
			seg := segments[s]
			maxMatch := 0

			for maxMatch < len(seg) && qi+maxMatch < len(query) && query[qi+maxMatch] == seg[maxMatch] {
				maxMatch++
			}

			for take := maxMatch; take >= 1 && !result; take-- {
				if match(qi+take, s+1) {
					result = true
				}
			}
		}

		memo[k] = result
		return result
	}

	return match(0, 0)
}

// labelsMatch returns true if any label key or value contains the query string
// (case-insensitive, query must be already lowercased).
func labelsMatch(labels map[string]string, q string) bool {
	for k, v := range labels {
		if ContainsFold(k, q) || ContainsFold(v, q) {
			return true
		}
	}
	return false
}
