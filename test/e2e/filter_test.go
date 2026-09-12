//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/filter"
	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives `?filter=`, the expr-lang expression language every list
// endpoint accepts, against a real cluster. It reserves port 19019.
// filterCases and the env builders in internal/filter are held together in
// both directions: every env-builder field must be driven, and every type with
// a filterEnv must appear in filterPaths.

const filterPort = 19019

func startFilterLane(t *testing.T) *sut.Process {
	t.Helper()

	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	return sut.Start(t, sut.Config{
		Port:       filterPort,
		DockerHost: env.DockerHost,
		Env:        map[string]string{"CETACEAN_AUTH_MODE": "none"},
	})
}

// ─── addressing the fixtures ────────────────────────────────────────────

// filterFixtures resolves the handful of live identifiers the table needs:
// an expression naming a node or a service ID cannot be written down ahead of
// the cluster that assigns it.
type filterFixtures struct {
	nodeID       string
	nodeHostname string
	webID        string // shop_web
	webImage     string
	taskID       string // one running shop_web task
	taskSlot     int
	shopNetID    string
	configID     string // orphan-config
	secretID     string // orphan-secret
}

func resolveFilterFixtures(t *testing.T, proc *sut.Process) filterFixtures {
	t.Helper()

	f := filterFixtures{}

	for _, node := range filterItems(t, proc, "/nodes", "") {
		f.nodeID = stringAt(node, "ID")
		f.nodeHostname = stringAt(node, "Description", "Hostname")
	}

	for _, service := range filterItems(t, proc, "/services", "") {
		if stringAt(service, "Spec", "Name") != "shop_web" {
			continue
		}

		f.webID = stringAt(service, "ID")
		f.webImage = stringAt(service, "Spec", "TaskTemplate", "ContainerSpec", "Image")
	}

	for _, net := range filterItems(t, proc, "/networks", "") {
		if stringAt(net, "Name") == "shop-net" {
			f.shopNetID = stringAt(net, "Id")
		}
	}

	for _, config := range filterItems(t, proc, "/configs", "") {
		if stringAt(config, "Spec", "Name") == fixtures.OrphanConfig {
			f.configID = stringAt(config, "ID")
		}
	}

	for _, secret := range filterItems(t, proc, "/secrets", "") {
		if stringAt(secret, "Spec", "Name") == fixtures.OrphanSecret {
			f.secretID = stringAt(secret, "ID")
		}
	}

	// A running shop_web task: the crash-looping fixture churns tasks
	// continuously, so anchoring on one of those would make every case a race.
	for _, task := range filterItems(t, proc, "/tasks", `state == "running"`) {
		if stringAt(task, "ServiceID") != f.webID {
			continue
		}

		f.taskID = stringAt(task, "ID")
		f.taskSlot = int(floatAt(task, "Slot"))
	}

	for name, got := range map[string]string{
		"node ID":               f.nodeID,
		"node hostname":         f.nodeHostname,
		"shop_web ID":           f.webID,
		"shop_web image":        f.webImage,
		"shop-net ID":           f.shopNetID,
		"orphan-config ID":      f.configID,
		"orphan-secret ID":      f.secretID,
		"running shop_web task": f.taskID,
	} {
		if got == "" {
			t.Fatalf("could not resolve %s from the fixture cluster", name)
		}
	}

	return f
}

// ─── the case table ─────────────────────────────────────────────────────

// filterCase drives one env field on one endpoint: `match` must select the
// named fixture, and `miss` — an expression over the same field — must not.
// Both are asserted against the live listing rather than against a count, so
// the crash-looping fixture's churn cannot decide the outcome.
type filterCase struct {
	field string
	path  string
	want  func(filterFixtures) string
	match func(filterFixtures) string
	miss  func(filterFixtures) string
}

// Shorthands: most cases name a fixture and an expression without consulting
// the cluster, and only the id-valued fields need the resolved identifiers.
func lit(s string) func(filterFixtures) string {
	return func(filterFixtures) string { return s }
}

var filterCases = []filterCase{
	// Nodes. One node, so every case names it; the misses are what carry the
	// weight here.
	{"id", "/nodes",
		func(f filterFixtures) string { return f.nodeHostname },
		func(f filterFixtures) string { return fmt.Sprintf("id == %q", f.nodeID) },
		lit(`id == "nope"`)},
	{"name", "/nodes",
		func(f filterFixtures) string { return f.nodeHostname },
		func(f filterFixtures) string { return fmt.Sprintf("name == %q", f.nodeHostname) },
		lit(`name == "not-a-host"`)},
	{"state", "/nodes",
		func(f filterFixtures) string { return f.nodeHostname },
		lit(`state == "ready"`), lit(`state == "down"`)},
	{"role", "/nodes",
		func(f filterFixtures) string { return f.nodeHostname },
		lit(`role == "manager"`), lit(`role == "worker"`)},
	{"availability", "/nodes",
		func(f filterFixtures) string { return f.nodeHostname },
		lit(`availability == "active"`), lit(`availability == "drain"`)},

	// Services.
	{"id", "/services", lit("shop_web"),
		func(f filterFixtures) string { return fmt.Sprintf("id == %q", f.webID) },
		lit(`id == "nope"`)},
	{"name", "/services", lit("shop_web"),
		lit(`name == "shop_web"`), lit(`name == "shop_lonely"`)},
	{"image", "/services", lit("shop_web"),
		func(f filterFixtures) string { return fmt.Sprintf("image == %q", f.webImage) },
		lit(`image == "nginx:latest"`)},
	{"mode", "/services", lit("shop_web"),
		lit(`mode == "replicated"`), lit(`mode == "global"`)},
	{"stack", "/services", lit("shop_web"),
		lit(`stack == "shop"`), lit(`stack == "platform"`)},

	// Tasks, anchored on one running shop_web replica.
	{"id", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		func(f filterFixtures) string { return fmt.Sprintf("id == %q", f.taskID) },
		lit(`id == "nope"`)},
	{"state", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		lit(`state == "running"`), lit(`state == "failed"`)},
	{"desired_state", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		lit(`desired_state == "running"`), lit(`desired_state == "shutdown"`)},
	{"image", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		func(f filterFixtures) string {
			return fmt.Sprintf(`image == %q && state == "running"`, f.webImage)
		},
		lit(`image == "nginx:latest"`)},
	// Documented as empty until the task reaches a terminal state, which is
	// why the running replica is addressed by the empty value and excluded by
	// the crash-looping fixture's.
	{"exit_code", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		lit(`exit_code == "" && state == "running"`), lit(`exit_code == "1"`)},
	{"error", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		lit(`error == "" && state == "running"`), lit(`error != ""`)},
	{"service", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		func(f filterFixtures) string {
			return fmt.Sprintf(`service == %q && state == "running"`, f.webID)
		},
		lit(`service == "nope"`)},
	{"node", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		func(f filterFixtures) string {
			return fmt.Sprintf(`node == %q && state == "running"`, f.nodeID)
		},
		lit(`node == "nope"`)},
	{"slot", "/tasks",
		func(f filterFixtures) string { return f.taskID },
		func(f filterFixtures) string {
			return fmt.Sprintf(`slot == %d && state == "running"`, f.taskSlot)
		},
		lit(`slot == 99`)},

	// Configs and secrets.
	{"id", "/configs", lit(fixtures.OrphanConfig),
		func(f filterFixtures) string { return fmt.Sprintf("id == %q", f.configID) },
		lit(`id == "nope"`)},
	{"name", "/configs", lit(fixtures.OrphanConfig),
		lit(`name == "orphan-config"`), lit(`name == "shop-config"`)},
	{"id", "/secrets", lit(fixtures.OrphanSecret),
		func(f filterFixtures) string { return fmt.Sprintf("id == %q", f.secretID) },
		lit(`id == "nope"`)},
	{"name", "/secrets", lit(fixtures.OrphanSecret),
		lit(`name == "orphan-secret"`), lit(`name == "shop-secret"`)},

	// Networks.
	{"id", "/networks", lit("shop-net"),
		func(f filterFixtures) string { return fmt.Sprintf("id == %q", f.shopNetID) },
		lit(`id == "nope"`)},
	{"name", "/networks", lit("shop-net"),
		lit(`name == "shop-net"`), lit(`name == "bridge"`)},
	{"driver", "/networks", lit("shop-net"),
		lit(`driver == "overlay"`), lit(`driver == "bridge"`)},
	{"scope", "/networks", lit("shop-net"),
		lit(`scope == "swarm"`), lit(`scope == "local"`)},

	// Volumes, which are keyed by name and carry no id field.
	{"name", "/volumes", lit(fixtures.UsedVolume),
		lit(`name == "shop-data"`), lit(`name == "orphan-vol"`)},
	{"driver", "/volumes", lit(fixtures.UsedVolume),
		lit(`driver == "local"`), lit(`driver == "nfs"`)},
	{"scope", "/volumes", lit(fixtures.UsedVolume),
		lit(`scope == "local"`), lit(`scope == "swarm"`)},

	// Stacks, whose fields are all counts but the name. The shop stack holds
	// three services, one config, one secret, one network and one volume; the
	// platform stack holds one service and nothing else, which is what makes
	// every count below discriminate.
	{"name", "/stacks", lit(fixtures.StackShop),
		lit(`name == "shop"`), lit(`name == "platform"`)},
	{"services", "/stacks", lit(fixtures.StackShop),
		lit(`services > 1`), lit(`services > 10`)},
	{"configs", "/stacks", lit(fixtures.StackShop),
		lit(`configs == 1`), lit(`configs == 0`)},
	{"secrets", "/stacks", lit(fixtures.StackShop),
		lit(`secrets == 1`), lit(`secrets == 0`)},
	{"networks", "/stacks", lit(fixtures.StackShop),
		lit(`networks == 1`), lit(`networks == 0`)},
	{"volumes", "/stacks", lit(fixtures.StackShop),
		lit(`volumes == 1`), lit(`volumes == 0`)},
}

// filterPaths maps each filterable endpoint to the keys its env builder emits.
// Built from the builders themselves, so a field added to one and to nothing
// else has nowhere to hide.
func filterPaths() map[string][]string {
	return map[string][]string{
		"/nodes":    envKeys(filter.NodeEnv(swarm.Node{}, nil)),
		"/services": envKeys(filter.ServiceEnv(swarm.Service{}, nil)),
		"/tasks":    envKeys(filter.TaskEnv(swarm.Task{}, nil)),
		"/configs":  envKeys(filter.ConfigEnv(swarm.Config{}, nil)),
		"/secrets":  envKeys(filter.SecretEnv(swarm.Secret{}, nil)),
		"/networks": envKeys(filter.NetworkEnv(network.Summary{}, nil)),
		"/volumes":  envKeys(filter.VolumeEnv(volume.Volume{}, nil)),
		"/stacks":   envKeys(filter.StackEnv(cache.Stack{}, nil)),
	}
}

func envKeys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}

// ─── the tests ──────────────────────────────────────────────────────────

// TestEveryFilterFieldIsDriven is the gate. internal/filter's env builders are
// the whole of what an expression may name, and this suite is the only place
// they are driven against a real cluster — so a field that reaches none of the
// cases below fails here rather than shipping untested.
func TestEveryFilterFieldIsDriven(t *testing.T) {
	driven := map[string]map[string]bool{}
	for _, c := range filterCases {
		if driven[c.path] == nil {
			driven[c.path] = map[string]bool{}
		}

		driven[c.path][c.field] = true
	}

	for path, fields := range filterPaths() {
		for _, field := range fields {
			if !driven[path][field] {
				t.Errorf(
					"%s: filter field %q is not driven by any case in filterCases",
					path,
					field,
				)
			}
		}

		for field := range driven[path] {
			if !slices.Contains(fields, field) {
				t.Errorf("%s: case drives %q, which the env builder does not emit", path, field)
			}
		}
	}

	for path := range driven {
		if _, ok := filterPaths()[path]; !ok {
			t.Errorf("case drives %s, which is not a filterable endpoint", path)
		}
	}
}

// TestFilterSelectsByEveryField drives every field of every env builder
// through the real stack: the expression is compiled by the server, evaluated
// against a live Docker resource, and judged by whether the resource it names
// came back.
func TestFilterSelectsByEveryField(t *testing.T) {
	proc := startFilterLane(t)
	fx := resolveFilterFixtures(t, proc)

	for _, c := range filterCases {
		t.Run(strings.TrimPrefix(c.path, "/")+"/"+c.field, func(t *testing.T) {
			want := c.want(fx)

			match := c.match(fx)
			if !filterSelects(t, proc, c.path, match, want) {
				t.Errorf("GET %s?filter=%s: %q is missing from the result", c.path, match, want)
			}

			miss := c.miss(fx)
			if filterSelects(t, proc, c.path, miss, want) {
				t.Errorf(
					"GET %s?filter=%s: %q is in the result and must not be",
					c.path,
					miss,
					want,
				)
			}
		})
	}
}

// TestFilterNarrowsTheTotal pins the invariant the dashboard's infinite scroll
// rests on: `total` counts the filtered collection, not the one behind it. A
// total that outran the items it describes would leave DataTable's sentinel
// asking for a page that never arrives.
func TestFilterNarrowsTheTotal(t *testing.T) {
	proc := startFilterLane(t)

	all := filterCollection(t, proc, "/services", "")
	narrowed := filterCollection(t, proc, "/services", `mode == "global"`)

	if narrowed.Total == 0 {
		t.Fatalf("mode == \"global\" selected nothing, and the baseline holds a global service")
	}

	if narrowed.Total >= all.Total {
		t.Fatalf("filtered total = %d, unfiltered = %d, want the filter to narrow it",
			narrowed.Total, all.Total)
	}

	if narrowed.Total != len(narrowed.Items) {
		t.Errorf("total = %d but %d items came back, and the page holds every match",
			narrowed.Total, len(narrowed.Items))
	}

	empty := filterCollection(t, proc, "/services", `name == "no-such-service"`)
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Errorf("a filter matching nothing: total = %d, items = %d, want 0 and 0",
			empty.Total, len(empty.Items))
	}
}

// TestFilterComposesWithSearchSortAndPagination drives the pipeline order in
// internal/api/list.go — ACL, search, filter, sort, paginate — from outside.
// Each stage has to survive the others: a filter applied after pagination
// would return short pages, and one applied after sorting would reorder them.
func TestFilterComposesWithSearchSortAndPagination(t *testing.T) {
	proc := startFilterLane(t)

	const shop = `stack == "shop"`

	sorted := filterCollection(t, proc, "/services?sort=name&dir=asc", shop)
	names := serviceNames(sorted.Items)

	if !slices.IsSorted(names) {
		t.Errorf("filtered and sorted by name: %v, want ascending", names)
	}

	descending := filterCollection(t, proc, "/services?sort=name&dir=desc", shop)
	reversed := serviceNames(descending.Items)
	slices.Reverse(reversed)

	if !slices.Equal(names, reversed) {
		t.Errorf("dir=desc over a filter gave %v, the reverse of asc is %v",
			serviceNames(descending.Items), names)
	}

	// Paged through one at a time, a filtered collection yields exactly the
	// items the unpaged one did, in the same order.
	var paged []string
	for offset := range sorted.Total {
		page := filterCollection(t, proc,
			fmt.Sprintf("/services?sort=name&dir=asc&limit=1&offset=%d", offset), shop)

		if page.Total != sorted.Total {
			t.Errorf("offset %d: total = %d, want %d — the filter must hold across pages",
				offset, page.Total, sorted.Total)
		}

		paged = append(paged, serviceNames(page.Items)...)
	}

	if !slices.Equal(paged, names) {
		t.Errorf("paged through a filtered collection: %v, want %v", paged, names)
	}

	// Search narrows what the filter already selected, rather than replacing it.
	both := filterCollection(t, proc, "/services?search=web", shop)
	if got := serviceNames(both.Items); !slices.Equal(got, []string{"shop_web"}) {
		t.Errorf("search=web over %s: %v, want [shop_web]", shop, got)
	}

	// A search and a filter that cannot both hold selects nothing — proof the
	// two are intersected and not, say, unioned.
	neither := filterCollection(t, proc, "/services?search=web", `stack == "platform"`)
	if neither.Total != 0 {
		t.Errorf("search=web with stack == platform: total = %d, want 0", neither.Total)
	}
}

// TestFilterRefusesMalformedExpressions drives the three FLT codes. Each is a
// documented error the API reference tells clients to expect, and each was
// reachable only through a unit test until now.
func TestFilterRefusesMalformedExpressions(t *testing.T) {
	proc := startFilterLane(t)

	cases := []struct {
		name string
		expr string
		code string
	}{
		{"too long", strings.Repeat("a", 513), "FLT001"},
		{"syntax error", `name ==`, "FLT002"},
		{"unbalanced parenthesis", `(name == "x"`, "FLT002"},
		// Compiles — expr cannot know the env's types without one — and fails
		// when the string it yields turns out not to be a bool.
		{"not a predicate", `name`, "FLT003"},
		{"comparing a string to a number", `name > 1`, "FLT003"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			//nolint:bodyclose // closed in filterRequestAs
			resp, body := filterRequest(t, proc, "/services", c.expr)

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400\n%s", resp.StatusCode, body)
			}

			if got := resp.Header.Get(
				"Content-Type",
			); !strings.HasPrefix(
				got,
				"application/problem+json",
			) {
				t.Errorf("Content-Type = %q, want application/problem+json", got)
			}

			var problem struct {
				Type   string `json:"type"`
				Status int    `json:"status"`
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal(body, &problem); err != nil {
				t.Fatalf("decode problem: %v\n%s", err, body)
			}

			if want := "/api/errors/" + c.code; problem.Type != want {
				t.Errorf("type = %q, want %q (detail: %s)", problem.Type, want, problem.Detail)
			}

			if problem.Status != http.StatusBadRequest {
				t.Errorf("problem status = %d, want 400", problem.Status)
			}
		})
	}

	// A field no env builder emits is not an error: expr resolves it to nil,
	// and nil equals nothing. The result is an empty collection, which is the
	// documented behaviour and the reason a mistyped field name looks like a
	// cluster with nothing in it.
	empty := filterCollection(t, proc, "/services", `no_such_field == "shop"`)
	if empty.Total != 0 {
		t.Errorf("an unknown field selected %d services, want 0", empty.Total)
	}
}

// TestFilterSharesOneCompileCacheAcrossTypes covers internal/filter's
// process-wide program cache, keyed by expression text alone: the same
// expression evaluated against two resource types must answer for each, and
// the random single-entry eviction at 64 entries must not change an answer.
func TestFilterSharesOneCompileCacheAcrossTypes(t *testing.T) {
	proc := startFilterLane(t)

	// One expression, two endpoints, two different env builders behind it.
	const shared = `name == "shop-config"`

	if !filterSelects(t, proc, "/configs", shared, "shop-config") {
		t.Errorf("%s on /configs did not select shop-config", shared)
	}

	if got := filterCollection(t, proc, "/secrets", shared); got.Total != 0 {
		t.Errorf("%s on /secrets selected %d secrets, want 0", shared, got.Total)
	}

	// Overflow the 64-entry cache, then ask the first expression again: an
	// evicted program has to be recompiled, not lost.
	for i := range 80 {
		filterCollection(t, proc, "/services", fmt.Sprintf(`name == "filler-%d"`, i))
	}

	if !filterSelects(t, proc, "/configs", shared, "shop-config") {
		t.Errorf("%s stopped selecting shop-config after the compile cache turned over", shared)
	}
}

// TestFilterCannotWidenACLScope holds the pipeline order that matters for
// disclosure: internal/api/list.go filters by grant before it evaluates the
// expression, so an expression is a way to narrow what you may already read
// and never a way to ask whether something else exists.
func TestFilterCannotWidenACLScope(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(shopOnlyPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	proc := sut.Start(t, sut.Config{
		Port:       filterPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_ACL_POLICY_FILE":      policy,
		},
	})

	identify := func(r *http.Request) {
		r.Header.Set("X-Auth-User", "viewer")
		r.Header.Set("X-Auth-Groups", "viewers")
	}

	// The grant covers shop_web alone. A filter naming any other service must
	// answer empty rather than confirming it exists.
	visible := filterCollectionAs(t, proc, "/services", "", identify)
	if got := serviceNames(visible.Items); !slices.Equal(got, []string{"shop_web"}) {
		t.Fatalf("the grant covers shop_web alone, but the listing held %v", got)
	}

	for _, expr := range []string{
		`name == "shop_lonely"`,
		`mode == "global"`,
		`stack == "platform"`,
		`true`,
	} {
		got := filterCollectionAs(t, proc, "/services", expr, identify)

		for _, name := range serviceNames(got.Items) {
			if name != "shop_web" {
				t.Errorf("filter=%s returned %q, which the caller has no grant for", expr, name)
			}
		}
	}
}

const shopOnlyPolicy = `grants:
  - resources: ["service:shop_web"]
    audience: ["group:viewers"]
    permissions: ["read"]
`

// ─── plumbing ───────────────────────────────────────────────────────────

type filterCollectionResponse struct {
	Items []map[string]any `json:"items"`
	Total int              `json:"total"`
}

func filterURL(path, expr string) string {
	if expr == "" {
		return path
	}

	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}

	return path + sep + "filter=" + url.QueryEscape(expr)
}

func filterRequest(t *testing.T, proc *sut.Process, path, expr string) (*http.Response, []byte) {
	t.Helper()

	return filterRequestAs(t, proc, path, expr, nil)
}

func filterRequestAs(
	t *testing.T,
	proc *sut.Process,
	path, expr string,
	identify func(*http.Request),
) (*http.Response, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		proc.BaseURL+filterURL(path, expr),
		nil,
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	if identify != nil {
		identify(req)
	}

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", filterURL(path, expr), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", filterURL(path, expr), err)
	}

	return resp, body
}

func filterCollection(t *testing.T, proc *sut.Process, path, expr string) filterCollectionResponse {
	t.Helper()

	return filterCollectionAs(t, proc, path, expr, nil)
}

func filterCollectionAs(
	t *testing.T,
	proc *sut.Process,
	path, expr string,
	identify func(*http.Request),
) filterCollectionResponse {
	t.Helper()

	//nolint:bodyclose // closed in filterRequestAs
	resp, body := filterRequestAs(t, proc, path, expr, identify)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200\n%s", filterURL(path, expr), resp.StatusCode, body)
	}

	var out filterCollectionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %s: %v\n%s", filterURL(path, expr), err, body)
	}

	return out
}

// filterItems reads a whole listing, above the default page size, so a case
// judging presence cannot be fooled by pagination.
func filterItems(t *testing.T, proc *sut.Process, path, expr string) []map[string]any {
	t.Helper()

	return filterCollection(t, proc, path+"?limit=200", expr).Items
}

// filterSelects answers whether the named resource is in the result. Names are
// per-endpoint: a task has none, and is addressed by ID.
func filterSelects(t *testing.T, proc *sut.Process, path, expr, want string) bool {
	t.Helper()

	for _, item := range filterItems(t, proc, path, expr) {
		if filterItemName(path, item) == want {
			return true
		}
	}

	return false
}

func filterItemName(path string, item map[string]any) string {
	switch path {
	case "/nodes":
		return stringAt(item, "Description", "Hostname")
	case "/services", "/configs", "/secrets":
		return stringAt(item, "Spec", "Name")
	case "/tasks":
		return stringAt(item, "ID")
	case "/networks", "/volumes":
		return stringAt(item, "Name")
	case "/stacks":
		return stringAt(item, "name")
	default:
		return ""
	}
}

func serviceNames(items []map[string]any) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, stringAt(item, "Spec", "Name"))
	}

	return names
}

func stringAt(item map[string]any, keys ...string) string {
	var cursor any = item
	for _, key := range keys {
		m, ok := cursor.(map[string]any)
		if !ok {
			return ""
		}

		cursor = m[key]
	}

	s, _ := cursor.(string)

	return s
}

func floatAt(item map[string]any, keys ...string) float64 {
	var cursor any = item
	for _, key := range keys {
		m, ok := cursor.(map[string]any)
		if !ok {
			return 0
		}

		cursor = m[key]
	}

	f, _ := cursor.(float64)

	return f
}
