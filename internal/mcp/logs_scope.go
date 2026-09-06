package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/docker"
	"github.com/radiergummi/cetacean/internal/logs"
)

const (
	// maxScopedServices bounds a fan-out. A cluster-wide read of a hundred
	// services is not a question anyone is asking; it is a way to exhaust a
	// context window.
	maxScopedServices = 25

	// scopedTailPerService is the per-service cap. The caller's `tail` is the
	// cap on the merged result, not on each service, or a twenty-service read
	// would return twenty times what was asked for.
	scopedTailPerService = 50

	// scopedConcurrency bounds simultaneous Docker log reads.
	scopedConcurrency = 4
)

// filterLogContains narrows lines to those holding sub, case-insensitively.
// An empty sub keeps everything.
//
// The match is cluster.ContainsFoldNoAlloc rather than a lowered copy of every
// message: a cluster-wide grep widens the tail tenfold and fans out over 25
// services, so lowering each message allocates a copy of the whole read.
// cluster.ContainsFold is deliberately not used — it adds segment-prefix
// matching, which is what a *name* search means, not what a log grep does.
func filterLogContains(lines []logs.LogLine, sub string) []logs.LogLine {
	if sub == "" {
		return lines
	}

	needle := strings.ToLower(sub)
	kept := make([]logs.LogLine, 0, len(lines))

	for _, line := range lines {
		if cluster.ContainsFoldNoAlloc(line.Message, needle) {
			kept = append(kept, line)
		}
	}

	return kept
}

// readScopedLogs merges the output of every service in a scope.
//
// "Anything cluster-wide in the last five minutes", "every error in the last
// hour", "grep the cluster" are enumerate-then-fan-out when the caller does
// them: one list call, then one log call per service, each round-trip paid for
// in the model's context. The join costs the server almost nothing and the
// caller one call.
//
// A service that cannot be read is reported in Errors rather than failing the
// whole read: a cluster-wide grep that dies on the first unreachable service
// is worse than no tool at all.
func (s *Server) readScopedLogs(
	ctx context.Context,
	scope string,
	target string,
	opts logOptions,
) (LogResourceResponse, error) {
	if s.logs == nil {
		return LogResourceResponse{Lines: []logs.LogLine{}, Errors: []string{}}, nil
	}

	services, err := s.servicesInScope(ctx, scope, target)
	if err != nil {
		return LogResourceResponse{}, err
	}

	perService := opts
	perService.tail = scopedTailPerService

	var (
		mu       sync.Mutex
		merged   []logs.LogLine
		failures []string
		wg       sync.WaitGroup
	)

	sem := make(chan struct{}, scopedConcurrency)

	for _, svc := range services {
		wg.Add(1)

		go func(svc swarm.Service) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := s.readLogsImpl(ctx, docker.ServiceLog, svc.ID, perService)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", svc.Spec.Name, err))

				return
			}

			for _, line := range resp.Lines {
				if line.Attrs == nil {
					line.Attrs = map[string]string{}
				}
				line.Attrs["serviceName"] = svc.Spec.Name
				line.Attrs["serviceId"] = svc.ID

				merged = append(merged, line)
			}
		}(svc)
	}

	wg.Wait()

	sort.Strings(failures)

	if failures == nil {
		failures = []string{}
	}

	// The same ordering, cut and cursor the single-service read applies: a
	// scoped tail resumes from a cursor internal/logs produced, and honours
	// the same tail bounds, because it is the same function.
	resp := finishLogRead(merged, boundLogTail(opts.tail), opts.since)
	resp.Errors = failures

	if resp.Lines == nil {
		resp.Lines = []logs.LogLine{}
	}

	return resp, nil
}

// servicesInScope resolves a scope to the services it covers, already filtered
// to what the caller may read — so a cluster-wide grep cannot become a way to
// read output from a service the caller has no grant for.
func (s *Server) servicesInScope(
	ctx context.Context,
	scope string,
	target string,
) ([]swarm.Service, error) {
	all := s.filterServices(ctx, s.cache.ListServices())

	var picked []swarm.Service

	switch scope {
	case "cluster":
		picked = all

	case "stack":
		if target == "" {
			return nil, fmt.Errorf("stack: a stack name is required")
		}

		for _, svc := range all {
			if strings.EqualFold(svc.Spec.Labels["com.docker.stack.namespace"], target) {
				picked = append(picked, svc)
			}
		}

		if len(picked) == 0 {
			return nil, fmt.Errorf("stack %q has no services you can read", target)
		}

	default:
		return nil, fmt.Errorf("scope %q: want \"cluster\" or \"stack\"", scope)
	}

	// Sorted so the per-service cap below is deterministic rather than
	// whichever services the map happened to yield first.
	sort.Slice(picked, func(i, j int) bool {
		return picked[i].Spec.Name < picked[j].Spec.Name
	})

	if len(picked) > maxScopedServices {
		picked = picked[:maxScopedServices]
	}

	return picked, nil
}
