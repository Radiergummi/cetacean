package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

	services, inScope, err := s.servicesInScope(ctx, scope, target)
	if err != nil {
		return LogResourceResponse{}, err
	}

	// Each service resumes from its own position. One cursor for the whole
	// fan-out is whichever service was furthest ahead, and logs.FilterSince is
	// a flat cut — so a line another service stamps earlier but Docker flushes
	// late is dropped on the resume and never returned by any later call.
	resumed, perServiceResume := decodeScopedCursor(opts.since)

	perService := opts
	perService.tail = scopedTailPerService

	if perServiceResume {
		perService.since = ""
	}

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

			svcOpts := perService
			if perServiceResume {
				svcOpts.since = resumed[svc.ID]
			}

			resp, err := s.readLogsImpl(ctx, docker.ServiceLog, svc.ID, svcOpts)

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
	resp.Cursor = nextScopedCursor(services, resumed, resp.Lines)

	// The cap is disclosed in the tool description, but the description is not
	// the half a model reasons over: a cluster-wide grep that read 25 of 60
	// services and matched nothing answers identically to a clean cluster
	// unless the payload itself says how far it looked.
	if dropped := inScope - len(services); dropped > 0 {
		resp.Note = fmt.Sprintf(
			"read the %d alphabetically-first of %d service(s) in scope; the "+
				"remaining %d were not read — narrow the scope with `stack` or "+
				"`service` to cover them",
			len(services), inScope, dropped,
		)
	}

	return resp, nil
}

// servicesInScope resolves a scope to the services it covers, already filtered
// to what the caller may read — so a cluster-wide grep cannot become a way to
// read output from a service the caller has no grant for.
//
// inScope is how many services the scope actually held, before the fan-out cap
// cut it down. The caller needs both numbers to say what it did not read.
func (s *Server) servicesInScope(
	ctx context.Context,
	scope string,
	target string,
) (picked []swarm.Service, inScope int, err error) {
	all := s.filterServices(ctx, s.cache.ListServices())

	switch scope {
	case "cluster":
		picked = all

	case "stack":
		if target == "" {
			return nil, 0, fmt.Errorf("stack: a stack name is required")
		}

		for _, svc := range all {
			if strings.EqualFold(svc.Spec.Labels["com.docker.stack.namespace"], target) {
				picked = append(picked, svc)
			}
		}

		if len(picked) == 0 {
			return nil, 0, fmt.Errorf("stack %q has no services you can read", target)
		}

	default:
		return nil, 0, fmt.Errorf("scope %q: want \"cluster\" or \"stack\"", scope)
	}

	// Sorted so the per-service cap below is deterministic rather than
	// whichever services the map happened to yield first.
	sort.Slice(picked, func(i, j int) bool {
		return picked[i].Spec.Name < picked[j].Spec.Name
	})

	inScope = len(picked)

	if len(picked) > maxScopedServices {
		picked = picked[:maxScopedServices]
	}

	return picked, inScope, nil
}

// scopedCursorPrefix marks a cursor holding one position per service rather
// than a single timestamp. The response documents its cursor as opaque, so
// widening what it carries needs no shape change — but a caller may also pass a
// plain timestamp as `since`, and the two have to be told apart.
const scopedCursorPrefix = "cs1:"

// nextScopedCursor records where each service in scope was left.
//
// The positions come from the lines actually returned, never from what was
// fetched: the merged tail cut can drop a service's lines entirely, and a
// position past a line the caller never saw loses it for good. A service that
// returned nothing keeps the position it arrived with, and a service no longer
// in scope is forgotten rather than carried forever.
func nextScopedCursor(
	services []swarm.Service,
	resumed map[string]string,
	delivered []logs.LogLine,
) string {
	positions := make(map[string]string, len(services))

	for _, svc := range services {
		if previous := resumed[svc.ID]; previous != "" {
			positions[svc.ID] = previous
		}
	}

	// finishLogRead sorted the lines oldest-first, so the last one seen for a
	// service is its newest and the plain assignment is the max.
	for _, line := range delivered {
		id := line.Attrs["serviceId"]
		if id == "" {
			continue
		}

		if cursor, ok := logs.ParseCursor(line.Timestamp); ok {
			positions[id] = cursor
		}
	}

	return encodeScopedCursor(positions)
}

// encodeScopedCursor renders one position per service. An empty set renders as
// an empty cursor: nothing was delivered, so there is nothing to resume past.
func encodeScopedCursor(positions map[string]string) string {
	if len(positions) == 0 {
		return ""
	}

	// encoding/json sorts map keys, so the same positions always encode
	// identically — a cursor whose bytes shifted between identical reads would
	// look like progress to a client comparing them.
	body, err := json.Marshal(positions)
	if err != nil {
		return ""
	}

	return scopedCursorPrefix + base64.RawURLEncoding.EncodeToString(body)
}

// decodeScopedCursor reads a cursor encodeScopedCursor produced. Anything else
// — a caller's own `since`, or a cursor from a single-service read — reports
// false and applies to every service alike, which is what it meant.
func decodeScopedCursor(cursor string) (map[string]string, bool) {
	encoded, found := strings.CutPrefix(cursor, scopedCursorPrefix)
	if !found {
		return nil, false
	}

	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false
	}

	var positions map[string]string
	if err := json.Unmarshal(body, &positions); err != nil {
		return nil, false
	}

	return positions, true
}
