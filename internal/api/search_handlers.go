package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

// --- Search ---

type searchResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	State  string `json:"state,omitempty"`
}

func labelsMatch(labels map[string]string, q string) bool {
	for k, v := range labels {
		if containsFold(k, q) || containsFold(v, q) {
			return true
		}
	}
	return false
}

func (h *Handlers) HandleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErrorCode(w, r, "SEA001", "missing required query parameter: q")
		return
	}
	if len(q) > 200 {
		writeErrorCode(w, r, "SEA002", "query too long (max 200 characters)")
		return
	}
	ql := strings.ToLower(q)

	limit := 3
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			limit = n
		}
	}
	// Cap per-type results to prevent unbounded allocations on large clusters.
	const maxPerType = 1000
	if limit == 0 || limit > maxPerType {
		limit = maxPerType
	}

	type typeResults struct {
		key     string
		results []searchResult
		count   int
	}

	// Fixed-size array indexed by search type for lock-free parallel writes.
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
	var allResults [stCount]typeResults

	// Build service name lookup for tasks (needed by services + tasks searches).
	services := h.cache.ListServices()
	svcNames := make(map[string]string, len(services))
	for _, s := range services {
		svcNames[s.ID] = s.Spec.Name
	}

	ctx := r.Context()

	var wg sync.WaitGroup
	wg.Add(stCount)

	// Services
	go func() {
		defer wg.Done()
		var matches []searchResult
		count := 0
		for _, s := range services {
			if ctx.Err() != nil {
				return
			}
			hit := containsFold(s.Spec.Name, ql)
			if !hit && s.Spec.TaskTemplate.ContainerSpec != nil {
				hit = containsFold(s.Spec.TaskTemplate.ContainerSpec.Image, ql)
			}
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					detail := ""
					if s.Spec.TaskTemplate.ContainerSpec != nil {
						detail = s.Spec.TaskTemplate.ContainerSpec.Image
						if i := strings.Index(detail, "@sha256:"); i > 0 {
							detail = detail[:i]
						}
					}
					running := h.cache.RunningTaskCount(s.ID)
					desired := 0
					if s.Spec.Mode.Replicated != nil && s.Spec.Mode.Replicated.Replicas != nil {
						desired = int(*s.Spec.Mode.Replicated.Replicas)
					} else if s.Spec.Mode.Global != nil {
						desired = -1 // global: just check running > 0
					}
					state := "running"
					if s.UpdateStatus != nil && s.UpdateStatus.State == swarm.UpdateStateUpdating {
						state = "updating"
					} else if desired == -1 {
						if running == 0 {
							state = "pending"
						}
					} else if desired > 0 && running == 0 {
						state = "failed"
					} else if running < desired {
						state = "pending"
					}
					matches = append(
						matches,
						searchResult{ID: s.ID, Name: s.Spec.Name, Detail: detail, State: state},
					)
				}
			}
		}
		allResults[stServices] = typeResults{"services", matches, count}
	}()

	// Stacks
	go func() {
		defer wg.Done()
		stacks := h.cache.ListStacks()
		var matches []searchResult
		count := 0
		for _, s := range stacks {
			if ctx.Err() != nil {
				return
			}
			if containsFold(s.Name, ql) {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     s.Name,
						Name:   s.Name,
						Detail: fmt.Sprintf("%d services", len(s.Services)),
					})
				}
			}
		}
		allResults[stStacks] = typeResults{"stacks", matches, count}
	}()

	// Nodes
	go func() {
		defer wg.Done()
		nodes := h.cache.ListNodes()
		var matches []searchResult
		count := 0
		for _, n := range nodes {
			if ctx.Err() != nil {
				return
			}
			hit := containsFold(n.Description.Hostname, ql)
			if !hit {
				hit = containsFold(n.Status.Addr, ql)
			}
			if !hit {
				hit = labelsMatch(n.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     n.ID,
						Name:   n.Description.Hostname,
						Detail: fmt.Sprintf("%s, %s", n.Spec.Role, n.Status.State),
					})
				}
			}
		}
		allResults[stNodes] = typeResults{"nodes", matches, count}
	}()

	// Tasks
	go func() {
		defer wg.Done()
		tasks := h.cache.ListTasks()
		var matches []searchResult
		count := 0
		for _, t := range tasks {
			if ctx.Err() != nil {
				return
			}
			svcName := svcNames[t.ServiceID]
			taskName := fmt.Sprintf("%s.%d", svcName, t.Slot)

			hit := containsFold(svcName, ql)
			if !hit && t.Spec.ContainerSpec != nil {
				hit = containsFold(t.Spec.ContainerSpec.Image, ql)
			}
			if !hit && t.Spec.ContainerSpec != nil {
				hit = labelsMatch(t.Spec.ContainerSpec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					detail := ""
					if t.Spec.ContainerSpec != nil {
						detail = t.Spec.ContainerSpec.Image
						if i := strings.Index(detail, "@sha256:"); i > 0 {
							detail = detail[:i]
						}
					}
					matches = append(matches, searchResult{
						ID:     t.ID,
						Name:   taskName,
						Detail: detail,
						State:  string(t.Status.State),
					})
				}
			}
		}
		allResults[stTasks] = typeResults{"tasks", matches, count}
	}()

	// Configs
	go func() {
		defer wg.Done()
		configs := h.cache.ListConfigs()
		var matches []searchResult
		count := 0
		for _, c := range configs {
			if ctx.Err() != nil {
				return
			}
			hit := containsFold(c.Spec.Name, ql)
			if !hit {
				hit = labelsMatch(c.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     c.ID,
						Name:   c.Spec.Name,
						Detail: c.CreatedAt.Format(time.RFC3339),
					})
				}
			}
		}
		allResults[stConfigs] = typeResults{"configs", matches, count}
	}()

	// Secrets
	go func() {
		defer wg.Done()
		secrets := h.cache.ListSecrets()
		var matches []searchResult
		count := 0
		for _, s := range secrets {
			if ctx.Err() != nil {
				return
			}
			s.Spec.Data = nil
			hit := containsFold(s.Spec.Name, ql)
			if !hit {
				hit = labelsMatch(s.Spec.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     s.ID,
						Name:   s.Spec.Name,
						Detail: s.CreatedAt.Format(time.RFC3339),
					})
				}
			}
		}
		allResults[stSecrets] = typeResults{"secrets", matches, count}
	}()

	// Networks
	go func() {
		defer wg.Done()
		networks := h.cache.ListNetworks()
		var matches []searchResult
		count := 0
		for _, n := range networks {
			if ctx.Err() != nil {
				return
			}
			hit := containsFold(n.Name, ql)
			if !hit {
				hit = labelsMatch(n.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     n.ID,
						Name:   n.Name,
						Detail: n.Driver,
					})
				}
			}
		}
		allResults[stNetworks] = typeResults{"networks", matches, count}
	}()

	// Volumes
	go func() {
		defer wg.Done()
		volumes := h.cache.ListVolumes()
		var matches []searchResult
		count := 0
		for _, v := range volumes {
			if ctx.Err() != nil {
				return
			}
			hit := containsFold(v.Name, ql)
			if !hit {
				hit = labelsMatch(v.Labels, ql)
			}
			if hit {
				count++
				if len(matches) < limit {
					matches = append(matches, searchResult{
						ID:     v.Name,
						Name:   v.Name,
						Detail: v.Driver,
					})
				}
			}
		}
		allResults[stVolumes] = typeResults{"volumes", matches, count}
	}()

	wg.Wait()

	// ACL-filter search results per resource type.
	identity := auth.IdentityFromContext(r.Context())
	resourcePrefixes := [stCount]string{
		stServices: "service:",
		stStacks:   "stack:",
		stNodes:    "node:",
		stTasks:    "task:",
		stConfigs:  "config:",
		stSecrets:  "secret:",
		stNetworks: "network:",
		stVolumes:  "volume:",
	}
	for i := range allResults {
		before := len(allResults[i].results)
		prefix := resourcePrefixes[i]
		allResults[i].results = acl.Filter(
			h.acl,
			identity,
			"read",
			allResults[i].results,
			func(sr searchResult) string {
				if i == stTasks {
					return prefix + sr.ID
				}
				return prefix + sr.Name
			},
		)
		if removed := before - len(allResults[i].results); removed > 0 {
			allResults[i].count -= removed
		}
	}

	results := make(map[string][]searchResult, stCount)
	counts := make(map[string]int, stCount)
	total := 0
	for _, s := range allResults {
		if s.count > 0 {
			results[s.key] = s.results
			counts[s.key] = s.count
			total += s.count
		}
	}

	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), "/search", "SearchResult", SearchResponse{
			Query:   q,
			Results: results,
			Counts:  counts,
			Total:   total,
		}),
	)
}
