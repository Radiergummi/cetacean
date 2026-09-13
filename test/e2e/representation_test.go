//go:build e2e

package e2e_test

import (
	"encoding/json"
	"encoding/xml"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/contract"
	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives the representation matrix: every content type each
// negotiated route declares, reached through both the Accept header and the
// extension suffix, refused when a route cannot provide it, and stable across
// identical requests. It reserves port 19017 (see README.md's reserved-ports
// table).
//
// Only JSON and SSE were driven anywhere before this. Six other media forms
// are registered across 58 routes, and the inventory behind the gate is parsed
// from router.go's own dispatch wiring — so a route gaining a representation
// has to be accounted for.

const representationPort = 19017

func startRepresentationLane(t *testing.T, env *harness.Env) *sut.Process {
	t.Helper()

	return sut.Start(t, sut.Config{
		Port:       representationPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": "0",
		},
	})
}

// ─── media forms ────────────────────────────────────────────────────────

// mediaForm is how one representation is asked for and recognised.
type mediaForm struct {
	accept string

	// suffix is the extension form of the same request, empty when the
	// representation has none (SSE).
	suffix string

	// contentType is the prefix the answer's Content-Type must carry.
	contentType string

	// verify checks the body is actually in that form, rather than merely
	// labelled as it.
	verify func(*testing.T, string)
}

var mediaForms = map[contract.Representation]mediaForm{
	contract.RepresentationJSON: {
		accept:      "application/json",
		suffix:      ".json",
		contentType: "application/json",
		verify:      verifyJSONObject,
	},
	contract.RepresentationHTML: {
		accept:      "text/html",
		suffix:      ".html",
		contentType: "text/html",
		verify: func(t *testing.T, body string) {
			t.Helper()

			// The SPA's own index.html, which is what an HTML request is
			// documented to receive on a resource path.
			if !strings.Contains(strings.ToLower(body), "<!doctype html") {
				t.Errorf("an HTML request did not return a document: %.120s", body)
			}
		},
	},
	contract.RepresentationAtom: {
		accept:      "application/atom+xml",
		suffix:      ".atom",
		contentType: "application/atom+xml",
		verify: func(t *testing.T, body string) {
			t.Helper()

			var feed struct {
				XMLName xml.Name
				ID      string `xml:"id"`
				Title   string `xml:"title"`
			}

			if err := xml.Unmarshal([]byte(body), &feed); err != nil {
				t.Errorf("the Atom body is not XML: %v (%.120s)", err, body)

				return
			}

			// RFC 4287 §4.1.1: a feed's document element is atom:feed, and id
			// and title are both required.
			if feed.XMLName.Local != "feed" {
				t.Errorf("the Atom document element is %q, want feed", feed.XMLName.Local)
			}

			if feed.ID == "" || feed.Title == "" {
				t.Errorf(
					"the Atom feed omits a required child: id=%q title=%q",
					feed.ID, feed.Title,
				)
			}
		},
	},
	contract.RepresentationJSONFeed: {
		accept:      "application/feed+json",
		suffix:      ".feed",
		contentType: "application/feed+json",
		verify: func(t *testing.T, body string) {
			t.Helper()

			var feed struct {
				Version string `json:"version"`
				Title   string `json:"title"`
			}

			if err := json.Unmarshal([]byte(body), &feed); err != nil {
				t.Errorf("the JSON Feed body is not JSON: %v (%.120s)", err, body)

				return
			}

			// JSON Feed 1.1 requires version and title, and pins the version
			// to the spec's own URL.
			if !strings.Contains(feed.Version, "jsonfeed.org/version/1.1") {
				t.Errorf("the feed names version %q, want the 1.1 spec URL", feed.Version)
			}

			if feed.Title == "" {
				t.Error("the feed omits its required title")
			}
		},
	},
	contract.RepresentationCSV: {
		accept:      "text/csv",
		suffix:      ".csv",
		contentType: "text/csv",
		verify: func(t *testing.T, body string) {
			t.Helper()

			// The header row, which a listing carries even when it has no
			// rows: the CSV comes off the list its JSON handler prepared.
			header, _, _ := strings.Cut(body, "\n")
			if !strings.Contains(header, ",") {
				t.Errorf("the CSV body has no header row: %.120s", body)
			}
		},
	},
	contract.RepresentationJGF: {
		accept:      "application/vnd.jgf+json",
		suffix:      ".jgf",
		contentType: "application/vnd.jgf+json",
		verify: func(t *testing.T, body string) {
			t.Helper()

			// JSON Graph Format: a multi-graph document carries `graphs`, a
			// single-graph one `graph`. Cetacean serves the first, and both
			// are accepted here so the check is of the format rather than of
			// today's shape.
			var doc struct {
				Graph  *jgfGraph  `json:"graph"`
				Graphs []jgfGraph `json:"graphs"`
			}

			if err := json.Unmarshal([]byte(body), &doc); err != nil {
				t.Errorf("the JGF body is not JSON: %v (%.120s)", err, body)

				return
			}

			graphs := doc.Graphs
			if doc.Graph != nil {
				graphs = append(graphs, *doc.Graph)
			}

			if len(graphs) == 0 {
				t.Errorf("the JGF document carries neither graph nor graphs: %.200s", body)

				return
			}

			for _, graph := range graphs {
				if graph.Nodes == nil {
					t.Errorf("a JGF graph carries no nodes: %.200s", body)
				}
			}
		},
	},
	contract.RepresentationGraphML: {
		accept:      "application/graphml+xml",
		suffix:      ".graphml",
		contentType: "application/graphml+xml",
		verify: func(t *testing.T, body string) {
			t.Helper()

			var doc struct {
				XMLName xml.Name
			}

			if err := xml.Unmarshal([]byte(body), &doc); err != nil {
				t.Errorf("the GraphML body is not XML: %v (%.120s)", err, body)

				return
			}

			if doc.XMLName.Local != "graphml" {
				t.Errorf("the GraphML document element is %q, want graphml", doc.XMLName.Local)
			}
		},
	},
	contract.RepresentationDOT: {
		accept:      "text/vnd.graphviz",
		suffix:      ".dot",
		contentType: "text/vnd.graphviz",
		verify: func(t *testing.T, body string) {
			t.Helper()

			// Graphviz's own grammar: a graph body opens with `graph` or
			// `digraph` and is brace-delimited.
			trimmed := strings.TrimSpace(body)
			if !strings.HasPrefix(trimmed, "graph") && !strings.HasPrefix(trimmed, "digraph") &&
				!strings.HasPrefix(trimmed, "strict") {
				t.Errorf("the DOT body does not open a graph: %.120s", trimmed)
			}

			if !strings.Contains(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
				t.Errorf("the DOT body is not brace-delimited: %.120s", trimmed)
			}
		},
	},
	contract.RepresentationSSE: {
		accept:      "text/event-stream",
		contentType: "text/event-stream",
	},
}

// jgfGraph is the part of a JGF graph this lane checks for.
type jgfGraph struct {
	Nodes map[string]any `json:"nodes"`
	Edges []any          `json:"edges"`
}

// assertContentType requires the answer to be labelled with the media type
// that was asked for. A feed that reports itself as plain JSON, or a graph
// export that reports itself as XML, is undiscoverable to a client
// dispatching on the label — which is the whole point of asking for it.
func assertContentType(t *testing.T, representation contract.Representation, got string) {
	t.Helper()

	want := mediaForms[representation].contentType

	if !strings.HasPrefix(got, want) {
		t.Errorf("Content-Type = %q, want %s", got, want)
	}
}

func verifyJSONObject(t *testing.T, body string) {
	t.Helper()

	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Errorf("the JSON body is not an object: %v (%.120s)", err, body)
	}
}

// ─── addressing the routes ──────────────────────────────────────────────

// representationFixture holds one addressable resource per type, so a route
// pattern can be turned into a path mechanically rather than through 58
// hand-written entries that would drift from the inventory.
type representationFixture struct {
	values map[string]string
}

func newRepresentationFixture(
	t *testing.T,
	env *harness.Env,
	proc *sut.Process,
) representationFixture {
	t.Helper()

	config := engineConfig(t, env, sweepName("repr-config"), nil)
	secret := engineSecret(t, env, sweepName("repr-secret"), nil)
	network := engineNetwork(t, env, sweepName("repr-network"))
	volume := sweepName("repr-volume")

	engineVolume(t, env, volume)

	stack := fixtures.DeployStack(t, env, "repr", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	service := serviceID(t, proc, stack+"_app")

	var task string
	for id := range serviceTaskIDs(t, env, inspectService(t, env, stack+"_app").ID) {
		task = id

		break
	}

	if task == "" {
		t.Fatal("the fixture service has no task to address")
	}

	fixture := representationFixture{values: map[string]string{
		"services": service,
		"nodes":    soleNodeID(t, env),
		"tasks":    task,
		"stacks":   stack,
		"configs":  config,
		"secrets":  secret,
		"networks": network,
		"volumes":  volume,

		// Not a cluster resource: /api/errors/{code} addresses the error
		// catalog, and API003 is one this lane asserts on elsewhere.
		"api": "API003",
	}}

	for _, path := range []string{
		"/services/" + service,
		"/tasks/" + task,
		"/stacks/" + stack,
		"/configs/" + config,
		"/secrets/" + secret,
		"/networks/" + network,
		"/volumes/" + volume,
	} {
		awaitCached(t, proc, path)
	}

	return fixture
}

// routeQuery carries the query string a route needs before it will serve
// anything at all, so the lane addresses the representation rather than the
// route's own argument validation.
var routeQuery = map[string]string{
	"GET /search": "?q=repr",
}

// path turns a route pattern into an addressable path by substituting each
// parameter with the fixture resource its leading segment names. A pattern
// whose type has no fixture entry is reported, never guessed.
func (f representationFixture) path(pattern string) (string, bool) {
	// {$} anchors a ServeMux pattern to the exact path. It names no parameter,
	// and the path it anchors is the one without it.
	pattern = strings.TrimSuffix(pattern, "{$}")

	segments := strings.Split(strings.TrimPrefix(pattern, "/"), "/")

	for i, segment := range segments {
		if !strings.HasPrefix(segment, "{") {
			continue
		}

		value, ok := f.values[segments[0]]
		if !ok {
			return "", false
		}

		segments[i] = value
	}

	return "/" + strings.Join(segments, "/"), true
}

// address builds the full request target for a route, including any query it
// needs. A suffix goes before the query, which is where the path ends.
func (f representationFixture) address(route, suffix string) (string, bool) {
	_, pattern, _ := strings.Cut(route, " ")

	path, ok := f.path(pattern)
	if !ok {
		return "", false
	}

	return path + suffix + routeQuery[route], true
}

// excusedRepresentationRoutes carries a reason for every negotiated route this
// lane does not address.
var excusedRepresentationRoutes = map[string]string{
	"GET /plugins/{name}": "needs a plugin registry this fixture does not provide, so " +
		"there is no plugin to address; the collection at /plugins is driven",
	"GET /metrics": "every representation is answered 503 without Prometheus, which " +
		"this environment does not run — the unconfigured case is driven by the read sweep",
	"GET /cluster/metrics": "same as /metrics: Prometheus is not configured here",
}

// unnegotiatedRoutes names routes the undeclared-type rule does not reach,
// because they are not content-negotiated resources at all.
var unnegotiatedRoutes = map[string]string{
	"/": "the SPA catch-all is the dashboard's static file server as well as its " +
		"HTML fallback, and every built asset is fetched with Accept: */*, which " +
		"resolves to the first supported type — so refusing a non-HTML Accept here " +
		"would refuse every script, stylesheet and font the dashboard loads",
}

// excusedRoutePairs excuses one representation of one route, where the route
// as a whole is driven.
var excusedRoutePairs = map[string]string{
	"GET /topology / JSON": "the route answers a plain application/json request with " +
		"the JGF document and labels it application/vnd.jgf+json (router.go's switch " +
		"pairs ContentTypeJGF with ContentTypeJSON deliberately — a graph is what this " +
		"route's JSON is); the JGF entry beside this one drives that body",
}

// excusedRepresentations carries a reason for a media form this lane does not
// drive at all.
var excusedRepresentations = map[contract.Representation]string{
	contract.RepresentationSSE: "a stream has to be held open and read frame by frame, " +
		"which is a different shape of test: driven by sse_test.go and sse_acl_test.go " +
		"for the resource streams and by logs_test.go for the log tail",
}

// ─── the matrix ─────────────────────────────────────────────────────────

// representationTargets pairs each addressable negotiated route with the
// representations it declares.
func representationTargets(
	t *testing.T,
	fixture representationFixture,
) map[string]map[contract.Representation]string {
	t.Helper()

	targets := map[string]map[contract.Representation]string{}

	for route, declared := range negotiatedRoutes(t) {
		if _, excused := excusedRepresentationRoutes[route]; excused {
			continue
		}

		address, ok := fixture.address(route, "")
		if !ok {
			t.Fatalf(
				"%s takes a path parameter this lane has no fixture for; add one to "+
					"newRepresentationFixture or excuse the route",
				route,
			)
		}

		forms := map[contract.Representation]string{}

		for _, representation := range declared {
			if _, excused := excusedRepresentations[representation]; excused {
				continue
			}

			if _, excused := excusedRoutePairs[pairKey(route, representation)]; excused {
				continue
			}

			forms[representation] = address
		}

		if len(forms) > 0 {
			targets[route] = forms
		}
	}

	return targets
}

func pairKey(route string, representation contract.Representation) string {
	return route + " / " + string(representation)
}

// TestRepresentationMatrix drives every declared representation of every
// addressable negotiated route: the Accept header is honoured, the answer is
// labelled with the media type asked for, and the body is actually in that
// form rather than merely labelled as it.
func TestRepresentationMatrix(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startRepresentationLane(t, env)
	fixture := newRepresentationFixture(t, env, proc)

	for route, forms := range representationTargets(t, fixture) {
		t.Run(route, func(t *testing.T) {
			for representation, path := range forms {
				form := mediaForms[representation]

				t.Run(string(representation), func(t *testing.T) {
					out := precondRequest(
						t, proc, http.MethodGet, path,
						map[string]string{"Accept": form.accept}, "", "",
					)

					if out.status != http.StatusOK {
						t.Fatalf(
							"Accept: %s → status = %d (body: %.200s)",
							form.accept, out.status, out.body,
						)
					}

					assertContentType(t, representation, out.header.Get("Content-Type"))

					form.verify(t, out.body)

					// RFC 9110 §12.5.1: a response that varies by Accept must
					// say so, or a shared cache serves one client's
					// negotiation to another.
					if vary := out.header.Get("Vary"); !strings.Contains(vary, "Accept") {
						t.Errorf("Vary = %q, want it to name Accept", vary)
					}
				})
			}
		})
	}
}

// TestRepresentationSuffixesMatchTheAcceptHeader drives the other addressing
// form the API documents. A suffix and its Accept header are two spellings of
// one request, so they must produce the same representation — and the suffix
// is the form a browser address bar can reach, which is why it exists.
func TestRepresentationSuffixesMatchTheAcceptHeader(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startRepresentationLane(t, env)
	fixture := newRepresentationFixture(t, env, proc)

	for route, forms := range representationTargets(t, fixture) {
		t.Run(route, func(t *testing.T) {
			for representation := range forms {
				form := mediaForms[representation]
				if form.suffix == "" {
					continue
				}

				target, ok := fixture.address(route, form.suffix)
				if !ok {
					t.Fatalf("%s has no addressable form", route)
				}

				t.Run(string(representation), func(t *testing.T) {
					// No Accept at all, so the suffix is the only thing
					// selecting the representation.
					suffixed := precondRequest(t, proc, http.MethodGet, target, nil, "", "")

					if suffixed.status != http.StatusOK {
						t.Fatalf(
							"%s → status = %d (body: %.200s)",
							form.suffix, suffixed.status, suffixed.body,
						)
					}

					assertContentType(t, representation, suffixed.header.Get("Content-Type"))

					form.verify(t, suffixed.body)

					// The suffix must also beat a contradicting Accept, which
					// is what "extension suffix takes priority" means.
					contradicted := precondRequest(
						t, proc, http.MethodGet, target,
						map[string]string{"Accept": "text/vnd.graphviz"}, "", "",
					)

					// The suffix must also beat a contradicting Accept, compared
					// against what the suffix alone produced rather than
					// against the declared type, so D-14 is reported once
					// rather than twice.
					if ct := contradicted.header.Get(
						"Content-Type",
					); ct != suffixed.header.Get("Content-Type") {
						t.Errorf(
							"a contradicting Accept overrode the %s suffix: Content-Type = %q, "+
								"want the %q the suffix alone produced",
							form.suffix, ct, suffixed.header.Get("Content-Type"),
						)
					}
				})
			}
		})
	}
}

// TestUndeclaredRepresentationsAreRefused drives the other half of
// negotiation, without which the matrix above could pass on a server that
// answered every request with the same thing. A route that cannot produce a
// media type must say so — internal/api/errors.go spells that API003 / 406
// ("The Accept header does not match any media type this endpoint can
// produce"), or API001 for the SSE special case, and it is the answer
// internal/api/dispatch.go's dispatchFeed already gives for a feed format a
// route has no handler for.
//
// Quarantined per finding D-13: a media type the route cannot produce is
// answered with a representation of some other type instead, in two shapes.
func TestUndeclaredRepresentationsAreRefused(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startRepresentationLane(t, env)
	fixture := newRepresentationFixture(t, env, proc)

	for route, declared := range negotiatedRoutes(t) {
		if _, excused := excusedRepresentationRoutes[route]; excused {
			continue
		}

		if reason, exempt := unnegotiatedRoutes[route]; exempt {
			t.Logf("%s: not subject to the rule — %s", route, reason)

			continue
		}

		address, ok := fixture.address(route, "")
		if !ok {
			t.Fatalf("%s takes a path parameter this lane has no fixture for", route)
		}

		t.Run(route, func(t *testing.T) {
			for representation, form := range mediaForms {
				if slices.Contains(declared, representation) {
					continue
				}

				t.Run(string(representation), func(t *testing.T) {
					out := precondRequest(
						t, proc, http.MethodGet, address,
						map[string]string{"Accept": form.accept}, "", "",
					)

					if out.status != http.StatusNotAcceptable {
						t.Fatalf(
							"Accept: %s → status = %d, Content-Type = %q; a route "+
								"that cannot produce this type must answer 406, not "+
								"hand back another one correctly labelled as the one "+
								"the client did not ask for",
							form.accept, out.status, out.header.Get("Content-Type"),
						)
					}

					// API001 is the SSE-specific spelling of the same
					// refusal; either is the documented answer.
					if !strings.Contains(out.body, "API001") &&
						!strings.Contains(out.body, "API003") {
						t.Errorf("the 406 names neither API001 nor API003: %.200s", out.body)
					}
				})
			}
		})
	}
}

// TestRepresentationETagsAreStable drives what internal/api/jsonld.go's
// deterministic key ordering exists for. An ETag that changes between two
// identical requests makes every conditional request miss and every If-Match
// fail, which no amount of correct 304 handling can recover from.
func TestRepresentationETagsAreStable(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startRepresentationLane(t, env)
	fixture := newRepresentationFixture(t, env, proc)

	// The resources this lane creates are quiescent, so a validator that moves
	// between two back-to-back reads moved because the representation was
	// rendered differently, not because the cluster changed.
	for route, forms := range representationTargets(t, fixture) {
		t.Run(route, func(t *testing.T) {
			for representation, path := range forms {
				if representation == contract.RepresentationHTML {
					continue // the SPA is a static file, served by net/http
				}

				form := mediaForms[representation]

				t.Run(string(representation), func(t *testing.T) {
					headers := map[string]string{"Accept": form.accept}

					first := precondRequest(t, proc, http.MethodGet, path, headers, "", "")
					if first.status != http.StatusOK {
						t.Fatalf("status = %d", first.status)
					}

					etag := first.header.Get("ETag")
					if etag == "" {
						t.Fatalf(
							"a 200 carried no ETag, though docs/api.md says every " +
								"JSON response does",
						)
					}

					second := precondRequest(t, proc, http.MethodGet, path, headers, "", "")
					if again := second.header.Get("ETag"); again != etag {
						t.Fatalf(
							"two identical requests produced %s then %s", etag, again,
						)
					}

					if second.body != first.body {
						t.Errorf("two identical requests produced different bodies")
					}

					headers["If-None-Match"] = etag

					conditional := precondRequest(t, proc, http.MethodGet, path, headers, "", "")
					if conditional.status != http.StatusNotModified {
						t.Errorf(
							"a conditional request with the current validator: status = %d, "+
								"want 304",
							conditional.status,
						)
					}
				})
			}
		})
	}
}

// ─── the gate ───────────────────────────────────────────────────────────

// TestEveryNegotiatedRouteIsDrivenOrExcused fails when a route gains a
// representation this lane does not reach.
func TestEveryNegotiatedRouteIsDrivenOrExcused(t *testing.T) {
	inventory := negotiatedRoutes(t)

	var (
		pairs   int
		skipped []string
	)

	for route, declared := range inventory {
		if reason, excused := excusedRepresentationRoutes[route]; excused {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("%s has an empty excuse reason", route)
			}

			skipped = append(skipped, route)

			continue
		}

		for _, representation := range declared {
			if reason, excused := excusedRepresentations[representation]; excused {
				if strings.TrimSpace(reason) == "" {
					t.Errorf("%s has an empty excuse reason", representation)
				}

				continue
			}

			if _, known := mediaForms[representation]; !known {
				t.Errorf(
					"%s declares representation %q, which mediaForms cannot ask for or "+
						"recognise; add it or excuse it",
					route, representation,
				)

				continue
			}

			pairs++
		}
	}

	for route := range excusedRepresentationRoutes {
		if _, ok := inventory[route]; !ok {
			t.Errorf(
				"excusedRepresentationRoutes has a stale entry %q: no such negotiated route",
				route,
			)
		}
	}

	// Every media form this lane knows how to ask for must be reachable
	// somewhere, or it is dead weight rather than coverage.
	for representation := range mediaForms {
		if _, excused := excusedRepresentations[representation]; excused {
			continue
		}

		if !slices.ContainsFunc(
			slices.Collect(maps.Values(inventory)),
			func(declared []contract.Representation) bool {
				return slices.Contains(declared, representation)
			},
		) {
			t.Errorf("mediaForms knows %q, which no route declares", representation)
		}
	}

	slices.Sort(skipped)
	t.Logf(
		"representations: %d route/representation pairs driven across %d routes, "+
			"%d routes excused\nexcused:\n  %s",
		pairs, len(inventory)-len(skipped), len(skipped), strings.Join(skipped, "\n  "),
	)
}

// negotiatedRoutes returns the representation inventory, bracketed by the
// chdir contract's source-relative parsing needs.
func negotiatedRoutes(t *testing.T) map[string][]contract.Representation {
	t.Helper()

	var (
		found map[string][]contract.Representation
		err   error
	)

	inContractDir(t, func() {
		found, err = contract.Representations()
	})

	if err != nil {
		t.Fatalf("contract.Representations: %v", err)
	}

	if len(found) == 0 {
		t.Fatal("contract.Representations returned nothing")
	}

	return found
}
