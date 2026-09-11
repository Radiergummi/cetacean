package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/radiergummi/cetacean/internal/config"
)

// operationsLevelBadgePrefix is how api/openapi.yaml states an operation's
// operations tier: an x-badges entry named "operations-level:N". Scalar renders
// it, and this test is what makes it the statement the server is held to.
const operationsLevelBadgePrefix = "operations-level:"

// operationsLevels enumerates the configurable tiers, lowest first. The probe
// below walks them in this order, so the first level that does not refuse is
// the tier the router actually enforces.
var operationsLevels = []config.OperationsLevel{
	config.OpsReadOnly,
	config.OpsOperational,
	config.OpsConfiguration,
	config.OpsImpactful,
}

// unmeasurableOperations lists spec operations this test cannot drive, with
// the reason. It is empty, and should stay that way: the streaming endpoints
// that look unmeasurable are not. The probe asks for application/json, which
// routes /events to the SPA and the two log tails to their JSON handlers, so
// all three answer a status promptly rather than streaming. Excluding them
// would be a blind spot rather than a saving — the guard below only fires when
// an excluded operation grows a *badge*, so a gate added without one would go
// unnoticed on exactly the operations nothing else measures.
//
// An entry may only cover an operation that declares no tier. One that grows
// an operations-level badge needs a way to be measured, not a skip — the
// assertion below fails outright rather than exempting it. An entry naming an
// operation the spec no longer has fails too, so the list cannot rot into
// silently skipping a path that gets added back under the same name.
var unmeasurableOperations = map[string]string{}

// TestEveryOperationIsGatedAtItsDeclaredTier holds the OpenAPI spec's
// operations-level badges and the router's requireLevel gates together, in
// both directions: every operation must be refused with OPS001 at one level
// below its badge and admitted at the badge's own level, and an operation
// carrying no badge must be gated at no tier at all.
//
// Reads are walked as well as writes, not for symmetry but because one read is
// gated — GET /swarm/unlock-key hands out the key that decrypts a locked
// manager's raft store, and sits at tier 3. Restricting this walk to mutations
// would leave the one endpoint whose tier matters most as the one endpoint
// nothing checks.
//
// The router side is measured, not tabulated. Driving the assembled router at
// each of the four levels and taking the lowest one that does not answer
// OPS001 yields the tier as the server enforces it, so a route whose gate and
// badge disagree fails whichever of the two was changed — and so does a gate
// with no badge, or a badge on an operation nothing gates. A hand-kept table
// of routes here would be a sixth statement of the tier, which is the drift
// this test exists to stop.
//
// It needs no request bodies. requireLevel sits ahead of the handler, so an
// empty body answers 400, 404, 412 or 415 — never OPS001 — and the assertion
// is only ever about whether OPS001 appears. The bogus If-Match keeps the
// thirty precondition-carrying routes from executing their mutation on the way
// past the gate.
func TestEveryOperationIsGatedAtItsDeclaredTier(t *testing.T) {
	_, doc, _ := loadTestSpec(t)

	routers := make(map[config.OperationsLevel]http.Handler, len(operationsLevels))
	for _, level := range operationsLevels {
		routers[level] = newSeededTestRouter(t, withOpsLevel(level))
	}

	var declared, gated int

	specified := map[string]bool{}

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			key := method + " " + path
			specified[key] = true

			badge, hasBadge, err := declaredOperationsLevel(op)
			if err != nil {
				t.Errorf("%s: %v", key, err)

				continue
			}

			if hasBadge {
				declared++
			}

			if reason, ok := unmeasurableOperations[key]; ok {
				if hasBadge {
					t.Errorf(
						"%s: declares operations-level:%d but is excluded from "+
							"measurement (%s) — find a way to drive it instead of "+
							"exempting a tier from its only check",
						key, badge, reason,
					)

					continue
				}

				t.Logf("skipping %s: %s", key, reason)

				continue
			}

			requestPath, ok := resolvePath(path)
			if !ok {
				t.Errorf(
					"%s: no fixture for its path parameters — teach resolvePath the "+
						"prefix so this operation's tier can be measured",
					key,
				)

				continue
			}

			observed, admitted := observedOperationsLevel(routers, method, requestPath)
			if observed > config.OpsReadOnly {
				gated++
			}

			t.Run(key, func(t *testing.T) {
				if !admitted {
					t.Fatalf(
						"refused with OPS001 at every level including %d — no "+
							"configuration admits it",
						config.OpsImpactful,
					)
				}

				if hasBadge && observed != badge {
					t.Errorf(
						"the spec badges this operations-level:%d, but the router "+
							"gates it at tier %d — correct whichever is wrong",
						badge, observed,
					)
				}

				if !hasBadge && observed > config.OpsReadOnly {
					t.Errorf(
						"the router gates this at tier %d, but the spec declares no "+
							"operations-level badge for it",
						observed,
					)
				}
			})
		}
	}

	for key, reason := range unmeasurableOperations {
		if !specified[key] {
			t.Errorf(
				"%s: excluded from measurement (%s) but the spec has no such "+
					"operation — drop the entry",
				key, reason,
			)
		}
	}

	// A walk that measured nothing would otherwise pass in silence: every
	// assertion above is per-operation, so an empty spec, or a skip rule broad
	// enough to swallow the lot, produces no failures at all. Both counts are
	// non-zero on any tree worth shipping.
	if declared == 0 || gated == 0 {
		t.Fatalf(
			"measured badged=%d gated=%d — the walk covered nothing",
			declared, gated,
		)
	}

	// Logged on success too: the floor above only catches a walk that covered
	// nothing at all, so a skip rule that quietly halved the coverage would
	// still pass. The counts make that visible in the output.
	t.Logf("badged=%d gated=%d", declared, gated)
}

// declaredOperationsLevel reads the operations-level badge off a spec
// operation. A malformed badge is an error rather than an absence: a typo in
// the level would otherwise silently exempt the operation from this test.
func declaredOperationsLevel(op *openapi3.Operation) (config.OperationsLevel, bool, error) {
	raw, ok := op.Extensions["x-badges"]
	if !ok {
		return 0, false, nil
	}

	badges, ok := raw.([]any)
	if !ok {
		return 0, false, fmt.Errorf("x-badges is %T, want a list", raw)
	}

	for _, entry := range badges {
		badge, ok := entry.(map[string]any)
		if !ok {
			return 0, false, fmt.Errorf("x-badges entry is %T, want a mapping", entry)
		}

		name, _ := badge["name"].(string)
		if !strings.HasPrefix(name, operationsLevelBadgePrefix) {
			continue
		}

		level, err := strconv.Atoi(strings.TrimPrefix(name, operationsLevelBadgePrefix))
		if err != nil {
			return 0, false, fmt.Errorf("badge %q: %w", name, err)
		}

		if level < int(config.OpsReadOnly) || level > int(config.OpsImpactful) {
			return 0, false, fmt.Errorf(
				"badge %q: level out of range %d..%d",
				name, config.OpsReadOnly, config.OpsImpactful,
			)
		}

		return config.OperationsLevel(level), true, nil
	}

	return 0, false, nil
}

// observedOperationsLevel returns the lowest configured level at which the
// router admits the request past its tier gate. Zero means the operation is
// gated at no tier. The bool is false when even the highest level refuses,
// which no configuration could then satisfy.
func observedOperationsLevel(
	routers map[config.OperationsLevel]http.Handler,
	method, path string,
) (config.OperationsLevel, bool) {
	for _, level := range operationsLevels {
		if !refusesForOperationsLevel(routers[level], method, path) {
			return level, true
		}
	}

	return 0, false
}

// refusesForOperationsLevel reports whether the router answers this request
// with OPS001. It reads the problem document's type rather than the status,
// because ACL002 is a 403 as well and a policy-less test router must never be
// mistaken for a tier refusal.
func refusesForOperationsLevel(router http.Handler, method, path string) bool {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("If-Match", `"bogus-etag"`)

	// No operation streams under application/json today, but one that started
	// to would otherwise hang the walk until the whole package times out. A
	// deadline turns that into a measured tier of 0, which fails against the
	// badge instead.
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()

	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		return false
	}

	var problem ProblemDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		return false
	}

	return strings.HasSuffix(problem.Type, "/api/errors/OPS001")
}
