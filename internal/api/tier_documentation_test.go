package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// The operations level a write endpoint needs is stated four times: the route's
// chain, resourceWriteMethods, an x-badges marker in api/openapi.yaml, and a
// row in docs/api.md. These two tests chain them together — the badge against
// the tier the router enforces, the table against the badge.

// operationsLevelBadge matches the marker as it appears in the OpenAPI
// document: `- name: "operations-level:2"`.
var operationsLevelBadge = regexp.MustCompile(`operations-level:(\d)`)

// tierTableRow matches a row of the write-endpoint table in docs/api.md. The
// method half is a list, because two endpoints share one row
// (`PUT`, `PATCH /services/{id}/healthcheck`), and a row naming one method
// parses as a list of one.
var tierTableRow = regexp.MustCompile(
	"^\\|\\s*((?:`[A-Z]+`,\\s*)*)`([A-Z]+) ([^`]+)`\\s*\\|\\s*(\\d)\\s*\\|",
)

// badgedOperation is one operation the OpenAPI document gates, as the document
// states it.
type badgedOperation struct {
	method string
	path   string
	level  config.OperationsLevel
}

func (o badgedOperation) String() string { return o.method + " " + o.path }

// badgedOperations reads every operations-level badge out of the OpenAPI
// document, keyed by "METHOD /path". It walks the parsed document rather than
// the YAML text, so a badge is attributed to the operation the parser assigns
// it to rather than the nearest path heading above it.
func badgedOperations(t *testing.T) map[string]badgedOperation {
	t.Helper()

	_, doc, _ := loadTestSpec(t)

	found := make(map[string]badgedOperation)

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			raw, ok := op.Extensions["x-badges"]
			if !ok {
				continue
			}

			match := operationsLevelBadge.FindStringSubmatch(fmt.Sprint(raw))
			if match == nil {
				continue
			}

			level, err := strconv.Atoi(match[1])
			if err != nil {
				t.Fatalf("%s %s: unparseable operations level %q", method, path, match[1])
			}

			operation := badgedOperation{
				method: method,
				path:   path,
				level:  config.OperationsLevel(level),
			}
			found[operation.String()] = operation
		}
	}

	if len(found) == 0 {
		t.Fatal("no operations-level badges found — has the marker changed shape?")
	}

	return found
}

// resolveTierPath is resolvePath plus the plugin name it has no fixture for.
// The entry is added here because resolvePath is shared with the spec
// conformance tests, which read the response and need a resolvable fixture;
// this one reads only whether requireLevel refused, which runs first.
func resolveTierPath(template string) (string, bool) {
	const pluginPrefix = "/plugins/{name}"

	if template == pluginPrefix || strings.HasPrefix(template, pluginPrefix+"/") {
		return strings.Replace(template, pluginPrefix, "/plugins/plugin-1", 1), true
	}

	return resolvePath(template)
}

// routersByLevel builds one router per operations level, seeded with the
// fixtures resolvePath addresses. Built once and shared: the assertion needs
// four routers, not four per operation, and NewRouter registers well over a
// hundred routes.
func routersByLevel(t *testing.T) map[config.OperationsLevel]http.Handler {
	t.Helper()

	levels := []config.OperationsLevel{
		config.OpsReadOnly,
		config.OpsOperational,
		config.OpsConfiguration,
		config.OpsImpactful,
	}

	routers := make(map[config.OperationsLevel]http.Handler, len(levels))

	for _, level := range levels {
		c := cache.New(nil)
		populateSpecFixtures(c)

		routers[level] = newTestRouterWithCache(
			t,
			c,
			withOpsLevel(level),
			withWriteClient(seededWriteClient()),
		)
	}

	return routers
}

// refusedForOperationsLevel drives the real router at the given level and
// reports whether requireLevel specifically refused. The error code is checked
// rather than the status, since requireWriteACL also answers 403 and would
// otherwise stand in for a tier refusal.
func refusedForOperationsLevel(
	t *testing.T,
	routers map[config.OperationsLevel]http.Handler,
	operation badgedOperation,
	level config.OperationsLevel,
) bool {
	t.Helper()

	requestPath, ok := resolveTierPath(operation.path)
	if !ok {
		t.Skipf("no fixture for the path parameters in %s", operation.path)
	}

	req := httptest.NewRequest(operation.method, requestPath, strings.NewReader("{}"))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/merge-patch+json")
	rec := httptest.NewRecorder()

	routers[level].ServeHTTP(rec, req)

	return strings.Contains(rec.Body.String(), "OPS001")
}

// Fails when an operations-level badge names a tier the router does not gate the
// operation at, in either direction. Each operation is probed twice, since one
// probe cannot separate a tier from every tier below it — and the refusal half
// doubles as the check that the route exists at the documented path.
func TestBadgedOperationsLevelIsTheTierEnforced(t *testing.T) {
	routers := routersByLevel(t)

	for name, operation := range badgedOperations(t) {
		t.Run(name, func(t *testing.T) {
			if operation.level == config.OpsReadOnly {
				t.Fatalf("badged operations-level:0 — level 0 disables writes, "+
					"so no write endpoint can require it: %s", operation)
			}

			if !refusedForOperationsLevel(t, routers, operation, operation.level-1) {
				t.Errorf(
					"admitted at operations level %d, but the OpenAPI document "+
						"badges it operations-level:%d — either the badge names a "+
						"tier above the one the route enforces, or nothing is "+
						"registered at this path",
					operation.level-1, operation.level,
				)
			}

			if refusedForOperationsLevel(t, routers, operation, operation.level) {
				t.Errorf(
					"refused with OPS001 at operations level %d, the level the "+
						"OpenAPI document badges it at — the route is gated above "+
						"its badge",
					operation.level,
				)
			}
		})
	}
}

// Fails when docs/api.md's write-endpoint table disagrees with the OpenAPI
// badges, or lists an endpoint the document does not gate. The table is checked
// against the badges rather than the router, since the test above already holds
// the badges to the router: one link per test, and the chain reaches both documents.
func TestTierTableMatchesTheBadges(t *testing.T) {
	badges := badgedOperations(t)

	const docPath = "../../docs/api.md"

	doc, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}

	documented := make(map[string]config.OperationsLevel)

	for line := range strings.SplitSeq(string(doc), "\n") {
		match := tierTableRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		level, err := strconv.Atoi(match[4])
		if err != nil {
			t.Fatalf("unparseable level in %q", line)
		}

		// A shared row lists its extra methods ahead of the one carrying the
		// path: "`PUT`, `PATCH /services/{id}/healthcheck`" documents both.
		methods := []string{match[2]}
		for extra := range strings.SplitSeq(match[1], ",") {
			if extra = strings.Trim(strings.TrimSpace(extra), "`"); extra != "" {
				methods = append(methods, extra)
			}
		}

		for _, method := range methods {
			documented[method+" "+match[3]] = config.OperationsLevel(level)
		}
	}

	if len(documented) == 0 {
		t.Fatal("no table rows parsed from docs/api.md — has the table changed shape?")
	}

	for name, operation := range badges {
		level, listed := documented[name]
		if !listed {
			t.Errorf("%s is badged operations-level:%d but has no row in the "+
				"docs/api.md table", name, operation.level)

			continue
		}

		if level != operation.level {
			t.Errorf("%s: docs/api.md says level %d, the OpenAPI badge says %d",
				name, level, operation.level)
		}
	}

	for name := range documented {
		if _, badged := badges[name]; !badged {
			t.Errorf("%s has a row in the docs/api.md table but no "+
				"operations-level badge in the OpenAPI document", name)
		}
	}
}
