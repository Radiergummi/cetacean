package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/filter"
)

// --- Stacks ---

func stackID(s cache.Stack) string { return "/stacks/" + s.Name }

func (h *Handlers) HandleListStacks(w http.ResponseWriter, r *http.Request) {
	handleList(h, w, r, listSpec[cache.Stack]{
		resourceType: "stack",
		linkTemplate: "/stacks/{name}",
		list:         h.cache.ListStacks,
		aclResource:  func(s cache.Stack) string { return "stack:" + s.Name },
		searchName:   func(s cache.Stack) string { return s.Name },
		filterEnv:    filter.StackEnv,
		sortKeys: map[string]func(cache.Stack) string{
			"name": func(s cache.Stack) string { return s.Name },
		},
		itemType: "Stack",
		idFunc:   stackID,
	})
}

func (h *Handlers) HandleGetStack(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	detail, ok := lookupACL(
		h,
		w,
		r,
		"stack",
		name,
		h.cache.GetStackDetail,
		func(s cache.StackDetail) string {
			return "stack:" + name
		},
	)
	if !ok {
		return
	}
	h.setAllow(w, r, "stack", name)
	writeCachedJSON(w, r, NewDetailResponse(r.Context(), "/stacks/"+name, "Stack", StackResponse{
		Stack: detail,
	}))
}

const stackNamespaceLabel = "container_label_com_docker_stack_namespace"

func (h *Handlers) HandleStackSummary(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}
	summaries := h.cache.ListStackSummaries()
	summaries = acl.Filter(
		h.acl,
		auth.IdentityFromContext(r.Context()),
		"read",
		summaries,
		func(s cache.StackSummary) string {
			return "stack:" + s.Name
		},
	)

	if h.promClient != nil && len(summaries) > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var memByStack, cpuByStack map[string]float64
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			memByStack = h.queryStackMetric(ctx,
				`sum by (`+stackNamespaceLabel+`)(container_memory_usage_bytes)`)
		}()
		go func() {
			defer wg.Done()
			cpuByStack = h.queryStackMetric(
				ctx,
				`sum by (`+stackNamespaceLabel+`)(rate(container_cpu_usage_seconds_total[5m])) * 100`,
			)
		}()
		wg.Wait()

		for i := range summaries {
			summaries[i].MemoryUsageBytes = int64(memByStack[summaries[i].Name])
			summaries[i].CPUUsagePercent = cpuByStack[summaries[i].Name]
		}
	}

	if summaries == nil {
		summaries = []cache.StackSummary{}
	}
	writeCachedJSON(
		w,
		r,
		NewCollectionResponse(
			r.Context(),
			wrapItems(
				summaries,
				"StackSummary",
				func(s cache.StackSummary) string { return "/stacks/" + s.Name },
			),
			len(summaries),
			len(summaries),
			0,
		),
	)
}

func (h *Handlers) queryStackMetric(ctx context.Context, query string) map[string]float64 {
	results, err := h.promClient.InstantQuery(ctx, query)
	if err != nil {
		slog.Warn("prometheus stack metric query failed", "error", err)
		return nil
	}
	out := make(map[string]float64, len(results))
	for _, r := range results {
		if name := r.Labels[stackNamespaceLabel]; name != "" {
			out[name] = r.Value
		}
	}
	return out
}
