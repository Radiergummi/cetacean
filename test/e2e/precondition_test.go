//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/contract"
	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives the conditional-request contract: the If-Match precondition
// on every mutating route that carries one, and the If-None-Match conditional
// GET on the representation it compares against. It reserves port 19014.
//
// preconditionTargets and excusedPreconditionRoutes together are the
// self-enforcing gate: every route contract.PreconditionedRoutes() reports
// must appear in one or the other.

const preconditionPort = 19014

// staleValidator has the shape of a real ETag — 32 hex characters, quoted —
// and cannot be any resource's, so a route that accepts it is comparing
// something other than the value.
const staleValidator = `"00000000000000000000000000000000"`

// missingID and missingName address a resource of the right shape that does
// not exist, for the RFC 9110 §13.2.2 case.
const (
	missingID   = "000000000000000000000000000"
	missingName = "cetacean-e2e-absent"
)

func startPreconditionLane(t *testing.T, env *harness.Env) *sut.Process {
	t.Helper()

	return sut.Start(t, sut.Config{
		Port:       preconditionPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": "3",
		},
	})
}

// ─── the target table ───────────────────────────────────────────────────

// precondProbe is a request the handler is certain to refuse for a reason that
// is not the precondition. It is how the accepted-validator case proves the
// middleware let a request through without mutating anything.
type precondProbe struct {
	contentType string
	body        string
	problem     string
}

// precondTarget is one preconditioned route, addressed at a live resource.
type precondTarget struct {
	method string

	// uri addresses a resource that exists: a GET here yields the validator
	// the precondition compares against. Empty when liveExcuse says why this
	// environment has no such resource.
	uri string

	// missing addresses one that does not.
	missing string

	// probe is nil for a route whose accepted-validator case performs the
	// write itself — the removals, driven by
	// TestConditionalRemovalRequiresTheCurrentValidator.
	probe *precondProbe

	liveExcuse     string
	acceptedExcuse string
}

// patchProbe refuses on Content-Type: every PATCH route reaches
// parsePatchMutator, requireMergePatch or applyStructMergePatch, all of which
// answer API004 before touching the resource.
func patchProbe() *precondProbe {
	return &precondProbe{contentType: "text/plain", body: "{}", problem: "API004"}
}

// putProbe refuses on the body: every PUT route decodes JSON into its request
// type before doing anything else, so a truncated object answers API006.
func putProbe() *precondProbe {
	return &precondProbe{contentType: "application/json", body: "{", problem: "API006"}
}

// preconditionFixture holds one live resource of each kind the preconditioned
// routes address, so thirty routes cost one cluster setup. Nothing driven
// against it mutates: every write is refused, either by the precondition or by
// the handler's own input validation.
type preconditionFixture struct {
	service string
	task    string
	stack   string
	config  string
	secret  string
	network string
	volume  string
	node    string
}

func newPreconditionFixture(
	t *testing.T,
	env *harness.Env,
	proc *sut.Process,
) preconditionFixture {
	t.Helper()

	// Created before the stack below so t.Cleanup's LIFO order removes the
	// services first: a config or network still attached cannot be removed.
	f := preconditionFixture{
		config:  engineConfig(t, env, sweepName("precond-config"), nil),
		secret:  engineSecret(t, env, sweepName("precond-secret"), nil),
		network: engineNetwork(t, env, sweepName("precond-network")),
		volume:  sweepName("precond-volume"),
		node:    soleNodeID(t, env),
	}

	engineVolume(t, env, f.volume)

	f.stack = fixtures.DeployStack(t, env, "precond", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	f.service = serviceID(t, proc, f.stack+"_app")

	svc := inspectService(t, env, f.stack+"_app")
	for id := range serviceTaskIDs(t, env, svc.ID) {
		f.task = id

		break
	}

	if f.task == "" {
		t.Fatalf("service %s_app has no task to address", f.stack)
	}

	// Every resource above was created straight on the engine, and the
	// handlers resolve through the cache the watcher fills asynchronously.
	for _, path := range []string{
		"/services/" + f.service,
		"/tasks/" + f.task,
		"/stacks/" + f.stack,
		"/configs/" + f.config,
		"/secrets/" + f.secret,
		"/networks/" + f.network,
		"/volumes/" + f.volume,
	} {
		awaitCached(t, proc, path)
	}

	return f
}

// preconditionTargets maps every preconditioned route to the request that
// drives it. It is a pure function of the fixture so the gate can read its
// keys without a cluster.
func preconditionTargets(f preconditionFixture) map[string]precondTarget {
	service := "/services/" + f.service
	missingService := "/services/" + missingID

	targets := map[string]precondTarget{}

	// The service sub-resources all share one service: none of the requests
	// below reaches a writer.
	for _, section := range []struct {
		suffix string
		method string
		probe  *precondProbe
	}{
		{"/configs", http.MethodPatch, patchProbe()},
		{"/container-config", http.MethodPatch, patchProbe()},
		{"/env", http.MethodPatch, patchProbe()},
		{"/healthcheck", http.MethodPatch, patchProbe()},
		{"/labels", http.MethodPatch, patchProbe()},
		{"/log-driver", http.MethodPatch, patchProbe()},
		{"/mounts", http.MethodPatch, patchProbe()},
		{"/networks", http.MethodPatch, patchProbe()},
		{"/ports", http.MethodPatch, patchProbe()},
		{"/resources", http.MethodPatch, patchProbe()},
		{"/rollback-policy", http.MethodPatch, patchProbe()},
		{"/secrets", http.MethodPatch, patchProbe()},
		{"/update-policy", http.MethodPatch, patchProbe()},
		{"/endpoint-mode", http.MethodPut, putProbe()},
		{"/healthcheck", http.MethodPut, putProbe()},
		{"/placement", http.MethodPut, putProbe()},
	} {
		targets[section.method+" /services/{id}"+section.suffix] = precondTarget{
			method:  section.method,
			uri:     service + section.suffix,
			missing: missingService + section.suffix,
			probe:   section.probe,
		}
	}

	targets["PATCH /nodes/{id}/labels"] = precondTarget{
		method:  http.MethodPatch,
		uri:     "/nodes/" + f.node + "/labels",
		missing: "/nodes/" + missingID + "/labels",
		probe:   patchProbe(),
	}
	targets["PUT /nodes/{id}/role"] = precondTarget{
		method:  http.MethodPut,
		uri:     "/nodes/" + f.node + "/role",
		missing: "/nodes/" + missingID + "/role",
		probe:   putProbe(),
	}
	targets["PATCH /configs/{id}/labels"] = precondTarget{
		method:  http.MethodPatch,
		uri:     "/configs/" + f.config + "/labels",
		missing: "/configs/" + missingID + "/labels",
		probe:   patchProbe(),
	}
	targets["PATCH /secrets/{id}/labels"] = precondTarget{
		method:  http.MethodPatch,
		uri:     "/secrets/" + f.secret + "/labels",
		missing: "/secrets/" + missingID + "/labels",
		probe:   patchProbe(),
	}

	// The removals: their accepted-validator case is the write itself, so it
	// is driven against its own throwaway resource rather than the shared
	// fixture — see TestConditionalRemovalRequiresTheCurrentValidator.
	removal := func(uri, missing string) precondTarget {
		return precondTarget{method: http.MethodDelete, uri: uri, missing: missing}
	}

	targets["DELETE /services/{id}"] = removal(service, missingService)
	targets["DELETE /tasks/{id}"] = removal("/tasks/"+f.task, "/tasks/"+missingID)
	targets["DELETE /stacks/{name}"] = removal("/stacks/"+f.stack, "/stacks/"+missingName)
	targets["DELETE /configs/{id}"] = removal("/configs/"+f.config, "/configs/"+missingID)
	targets["DELETE /secrets/{id}"] = removal("/secrets/"+f.secret, "/secrets/"+missingID)
	targets["DELETE /networks/{id}"] = removal("/networks/"+f.network, "/networks/"+missingID)
	targets["DELETE /volumes/{name}"] = removal("/volumes/"+f.volume, "/volumes/"+missingName)

	node := removal("/nodes/"+f.node, "/nodes/"+missingID)
	node.acceptedExcuse = "the environment's swarm has exactly one node; removing it " +
		"would tear down the cluster every other lane in this test binary shares"
	targets["DELETE /nodes/{id}"] = node

	plugin := removal("", "/plugins/"+missingName)
	plugin.liveExcuse = "needs a plugin registry this fixture does not provide, so " +
		"there is no installed plugin to read a validator from; the " +
		"no-representation case below still drives the route"
	plugin.acceptedExcuse = plugin.liveExcuse
	targets["DELETE /plugins/{name}"] = plugin

	return targets
}

// excusedPreconditionRoutes carries a reason for every preconditioned route
// this file does not address at all. It is empty: a stale If-Match is refused
// before the handler runs, so even a route too destructive to complete can be
// driven for the contract that matters.
var excusedPreconditionRoutes = map[string]string{}

// ─── request helpers ────────────────────────────────────────────────────

func precondRequest(
	t *testing.T,
	proc *sut.Process,
	method, path string,
	headers map[string]string,
	contentType, body string,
) httpOutcome {
	t.Helper()

	req, err := http.NewRequestWithContext(
		context.Background(), method, proc.BaseURL+path, strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}

	req.Header.Set("Accept", "application/json")

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	return send(t, proc, req)
}

// representationETag reads the validator a client would condition on, and
// fails when the paired GET does not offer one: an If-Match a caller cannot
// obtain is not a usable precondition.
func representationETag(t *testing.T, proc *sut.Process, uri string) string {
	t.Helper()

	out := precondRequest(t, proc, http.MethodGet, uri, nil, "", "")
	if out.status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200 (body: %s)", uri, out.status, out.body)
	}

	etag := out.header.Get("ETag")
	if etag == "" {
		t.Fatalf("GET %s answered 200 with no ETag, so If-Match cannot be used here", uri)
	}

	return etag
}

// assertPreconditionFailed requires the RFC 9110 §13.1.1 refusal, spelled the
// way the error catalog spells it.
func assertPreconditionFailed(t *testing.T, what string, out httpOutcome) {
	t.Helper()

	if out.status != http.StatusPreconditionFailed {
		t.Errorf("%s: status = %d, want 412 (body: %s)", what, out.status, out.body)

		return
	}

	if !strings.Contains(out.body, "API013") {
		t.Errorf("%s: 412 body does not name API013: %s", what, out.body)
	}
}

// ─── the sweep ──────────────────────────────────────────────────────────

// TestPreconditionSweep drives the conditional-request contract on every route
// carrying the If-Match middleware. Nothing here mutates — each write is
// refused either by the precondition or by input validation — which is what
// makes one shared fixture safe.
func TestPreconditionSweep(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startPreconditionLane(t, env)
	fixture := newPreconditionFixture(t, env, proc)

	for route, target := range preconditionTargets(fixture) {
		t.Run(route, func(t *testing.T) {
			// RFC 9110 §13.2.2: a resource with no current representation fails
			// the precondition, ahead of the 404 the request would otherwise
			// receive. `*` matches any representation at all, so a 412 here can
			// only mean there is none.
			assertPreconditionFailed(t, "no current representation", precondRequest(
				t, proc, target.method, target.missing,
				map[string]string{"If-Match": "*"}, "", "",
			))

			if target.liveExcuse != "" {
				t.Logf("only the no-representation case is driven here: %s", target.liveExcuse)

				return
			}

			// RFC 9110 §13.1.1: a validator that is not the current one does not
			// satisfy If-Match. Driven ahead of the baseline because evaluating a
			// precondition refreshes its subject from the asynchronously filled
			// cache, so a validator read beforehand could move under the sweep
			// without anything being written.
			assertPreconditionFailed(t, "stale validator", precondRequest(
				t, proc, target.method, target.uri,
				map[string]string{"If-Match": staleValidator}, "", "",
			))

			current := representationETag(t, proc, target.uri)

			// RFC 9110 §13.1.2: a matching validator means the client already
			// holds this representation.
			notModified := precondRequest(
				t, proc, http.MethodGet, target.uri,
				map[string]string{"If-None-Match": current}, "", "",
			)
			if notModified.status != http.StatusNotModified {
				t.Errorf(
					"GET %s with the current validator: status = %d, want 304",
					target.uri, notModified.status,
				)
			}
			if notModified.body != "" {
				t.Errorf("304 carried a body of %d bytes", len(notModified.body))
			}

			// The other half of the same rule, so the 304 above cannot pass
			// by answering 304 to everything.
			modified := precondRequest(
				t, proc, http.MethodGet, target.uri,
				map[string]string{"If-None-Match": staleValidator}, "", "",
			)
			if modified.status != http.StatusOK {
				t.Errorf(
					"GET %s with a stale validator: status = %d, want 200",
					target.uri, modified.status,
				)
			}

			// §13.1.1 again: If-Match uses strong comparison, so a weak
			// validator never matches — not even the resource's own.
			assertPreconditionFailed(t, "weak validator", precondRequest(
				t, proc, target.method, target.uri,
				map[string]string{"If-Match": "W/" + current}, "", "",
			))

			if target.probe != nil {
				// The current validator satisfies the precondition, which the
				// handler's own refusal proves: reaching its input validation
				// at all means the middleware let the request through.
				accepted := precondRequest(
					t, proc, target.method, target.uri,
					map[string]string{"If-Match": current},
					target.probe.contentType, target.probe.body,
				)

				if accepted.status == http.StatusPreconditionFailed {
					t.Errorf(
						"the current validator was refused: %s",
						accepted.body,
					)
				} else if !strings.Contains(accepted.body, target.probe.problem) {
					t.Errorf(
						"probe answered %d without naming %s: %s",
						accepted.status, target.probe.problem, accepted.body,
					)
				}
			} else if target.acceptedExcuse == "" {
				t.Logf(
					"accepted-validator case driven by " +
						"TestConditionalRemovalRequiresTheCurrentValidator",
				)
			} else {
				t.Logf("accepted-validator case not driven: %s", target.acceptedExcuse)
			}

			// Nothing above was allowed to change the resource. Comparing the
			// validator rather than the body catches a write that landed and
			// a representation that drifted alike.
			if after := representationETag(t, proc, target.uri); after != current {
				t.Errorf(
					"the representation changed under refused requests: %s -> %s",
					current, after,
				)
			}
		})
	}
}

// ─── the removals ───────────────────────────────────────────────────────

// TestConditionalRemovalRequiresTheCurrentValidator drives the accepted-
// validator case for the routes whose write is the removal itself. Each runs
// against its own throwaway resource and is verified against the engine, not
// the response: a 204 that removed nothing would pass otherwise.
func TestConditionalRemovalRequiresTheCurrentValidator(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startPreconditionLane(t, env)
	ctx := context.Background()

	for route, drive := range map[string]func(*testing.T, *harness.Env, *sut.Process){
		"DELETE /configs/{id}": func(t *testing.T, env *harness.Env, proc *sut.Process) {
			id := engineConfig(t, env, sweepName("precond-rm-config"), nil)
			awaitCached(t, proc, "/configs/"+id)
			driveConditionalRemoval(t, proc, "/configs/"+id)
			awaitGone(t, "config", id, func() error {
				_, _, err := env.Docker.ConfigInspectWithRaw(ctx, id)

				return err
			})
		},
		"DELETE /secrets/{id}": func(t *testing.T, env *harness.Env, proc *sut.Process) {
			id := engineSecret(t, env, sweepName("precond-rm-secret"), nil)
			awaitCached(t, proc, "/secrets/"+id)
			driveConditionalRemoval(t, proc, "/secrets/"+id)
			awaitGone(t, "secret", id, func() error {
				_, _, err := env.Docker.SecretInspectWithRaw(ctx, id)

				return err
			})
		},
		"DELETE /networks/{id}": func(t *testing.T, env *harness.Env, proc *sut.Process) {
			id := engineNetwork(t, env, sweepName("precond-rm-network"))
			awaitCached(t, proc, "/networks/"+id)
			driveConditionalRemoval(t, proc, "/networks/"+id)
			awaitGone(t, "network", id, func() error {
				_, err := env.Docker.NetworkInspect(ctx, id, network.InspectOptions{})

				return err
			})
		},
		"DELETE /volumes/{name}": func(t *testing.T, env *harness.Env, proc *sut.Process) {
			name := sweepName("precond-rm-volume")
			engineVolume(t, env, name)
			awaitCached(t, proc, "/volumes/"+name)
			driveConditionalRemoval(t, proc, "/volumes/"+name)
			awaitGone(t, "volume", name, func() error {
				_, err := env.Docker.VolumeInspect(ctx, name)

				return err
			})
		},
		"DELETE /services/{id}": func(t *testing.T, env *harness.Env, proc *sut.Process) {
			name := deployThrowawayService(t, env, "precond-rm-svc")
			id := serviceID(t, proc, name)
			awaitCached(t, proc, "/services/"+id)
			driveConditionalRemoval(t, proc, "/services/"+id)
			awaitGone(t, "service", name, func() error {
				_, _, err := env.Docker.ServiceInspectWithRaw(
					ctx, id, swarm.ServiceInspectOptions{},
				)

				return err
			})
		},
		"DELETE /stacks/{name}": func(t *testing.T, env *harness.Env, proc *sut.Process) {
			stack := fixtures.DeployStack(t, env, "precond-rm-stack", []fixtures.ServiceSpec{
				{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
			})
			awaitCached(t, proc, "/stacks/"+stack)

			driveConditionalRemoval(t, proc, "/stacks/"+stack)
			awaitGone(t, "stack service", stack+"_app", func() error {
				_, _, err := env.Docker.ServiceInspectWithRaw(
					ctx, stack+"_app", swarm.ServiceInspectOptions{},
				)

				return err
			})
		},
		"DELETE /tasks/{id}": func(t *testing.T, env *harness.Env, proc *sut.Process) {
			name := deployThrowawayService(t, env, "precond-rm-task")
			svc := inspectService(t, env, name)

			before := serviceTaskIDs(t, env, svc.ID)
			if len(before) == 0 {
				t.Fatalf("service %s has no task to remove", name)
			}

			var task string
			for id := range before {
				task = id

				break
			}

			awaitCached(t, proc, "/tasks/"+task)
			driveConditionalRemoval(t, proc, "/tasks/"+task)

			// A removed task is force-shutdown rather than erased, so the
			// engine-side proof is the replacement the orchestrator schedules.
			awaitReplacementTask(t, env, svc.ID, before)
		},
	} {
		t.Run(route, func(t *testing.T) {
			drive(t, env, proc)
		})
	}
}

// driveConditionalRemoval refuses a stale validator, proves the resource
// survived it, then removes with the current one.
func driveConditionalRemoval(t *testing.T, proc *sut.Process, uri string) {
	t.Helper()

	// One refusal before the baseline is taken: evaluating a precondition
	// refreshes its subject from the engine, so a validator read beforehand
	// could move under the refusal below without anything having been written.
	assertPreconditionFailed(t, "stale validator on removal", precondRequest(
		t, proc, http.MethodDelete, uri,
		map[string]string{"If-Match": staleValidator}, "", "",
	))

	current := representationETag(t, proc, uri)

	// The same refusal again, now against a baseline the refresh cannot move.
	// What it must not do is remove anything.
	assertPreconditionFailed(t, "stale validator on removal", precondRequest(
		t, proc, http.MethodDelete, uri,
		map[string]string{"If-Match": staleValidator}, "", "",
	))

	if survived := representationETag(t, proc, uri); survived != current {
		t.Errorf("a refused removal changed %s: %s -> %s", uri, current, survived)
	}

	removed := precondRequest(
		t, proc, http.MethodDelete, uri,
		map[string]string{"If-Match": current}, "", "",
	)
	if removed.status != http.StatusNoContent && removed.status != http.StatusOK {
		t.Fatalf(
			"DELETE %s with the current validator: status = %d (body: %s)",
			uri, removed.status, removed.body,
		)
	}
}

// awaitReplacementTask blocks until the service holds a task it did not hold
// before, or fails after two minutes.
func awaitReplacementTask(
	t *testing.T,
	env *harness.Env,
	serviceID string,
	before map[string]bool,
) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)

	for time.Now().Before(deadline) {
		for id := range serviceTaskIDs(t, env, serviceID) {
			if !before[id] {
				return
			}
		}

		time.Sleep(2 * time.Second)
	}

	t.Errorf("service %s never got a replacement task after the conditional removal", serviceID)
}

// ─── focused cases ──────────────────────────────────────────────────────

// TestUnpreconditionedWriteIgnoresIfMatch pins the other half of the documented
// surface: the mutating endpoints that carry no precondition because no GET
// serves their exact path simply do not evaluate the header. Driving one keeps
// the sweep honest — every 412 it asserts has to come from the middleware.
func TestUnpreconditionedWriteIgnoresIfMatch(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startPreconditionLane(t, env)

	name := deployThrowawayService(t, env, "precond-unguarded")
	id := serviceID(t, proc, name)

	out := precondRequest(
		t, proc, http.MethodPut, "/services/"+id+"/scale",
		map[string]string{"If-Match": staleValidator},
		"application/json", `{"replicas":2}`,
	)

	if out.status == http.StatusPreconditionFailed {
		t.Fatalf("PUT /services/{id}/scale refused a stale If-Match; docs/api.md " +
			"lists it among the endpoints that carry no precondition")
	}

	if out.status != http.StatusOK {
		t.Fatalf("PUT /services/{id}/scale: status = %d, want 200 (body: %s)", out.status, out.body)
	}

	// The engine, not the response: a write that answered 200 without scaling
	// would otherwise pass.
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		svc := inspectService(t, env, name)
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil &&
			*svc.Spec.Mode.Replicated.Replicas == 2 {
			return
		}

		time.Sleep(time.Second)
	}

	t.Errorf("service %s never reached 2 replicas on the engine", name)
}

// TestIfMatchAcceptsAValidatorObtainedUnderContentEncoding drives RFC 9110
// §8.8.3 as Cetacean resolves it: the coding suffix on an ETag distinguishes
// two cached representations, but a precondition asserts resource state, so a
// gzip validator must satisfy an If-Match on an identity-negotiated write. The
// oversized label is there because the rule only engages past the 1 KiB
// compression threshold.
func TestIfMatchAcceptsAValidatorObtainedUnderContentEncoding(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startPreconditionLane(t, env)

	stack := fixtures.DeployStack(t, env, "precond-coding", []fixtures.ServiceSpec{{
		Name:     "app",
		Replicas: 1,
		Command:  []string{"sleep infinity"},
		Labels:   map[string]string{"cetacean.e2e.bulk": strings.Repeat("x", 2048)},
	}})

	id := serviceID(t, proc, stack+"_app")
	uri := "/services/" + id + "/labels"

	// Setting Accept-Encoding by hand also turns off net/http's transparent
	// decompression, which is fine: only the validator is read here.
	encoded := precondRequest(
		t, proc, http.MethodGet, uri,
		map[string]string{"Accept-Encoding": "gzip"}, "", "",
	)
	if encoded.status != http.StatusOK {
		t.Fatalf("GET %s: status = %d (body: %s)", uri, encoded.status, encoded.body)
	}

	etag := encoded.header.Get("ETag")
	if !strings.HasSuffix(strings.Trim(etag, `"`), "-gzip") {
		t.Fatalf(
			"GET %s under Accept-Encoding: gzip returned %q, which carries no coding "+
				"suffix, so this case would assert nothing",
			uri, etag,
		)
	}

	// The handler refuses the Content-Type, which is proof enough that the
	// precondition let the request through.
	probe := patchProbe()

	out := precondRequest(
		t, proc, http.MethodPatch, uri,
		map[string]string{"If-Match": etag}, probe.contentType, probe.body,
	)
	if out.status == http.StatusPreconditionFailed {
		t.Fatalf("a validator obtained under gzip was refused as If-Match: %s", out.body)
	}

	if !strings.Contains(out.body, probe.problem) {
		t.Errorf(
			"probe answered %d without naming %s: %s", out.status, probe.problem, out.body,
		)
	}
}

// TestConditionalWriteIsEvaluatedAgainstTheEngine drives the case If-Match
// exists to refuse with no third party involved: a validator the server itself
// superseded, replayed on the next write.
//
// The window is the one the cache cannot see — representations are built from
// the asynchronously filled cache while every writer re-inspects the engine for
// a fresh Version — so the precondition refreshes its subject from the engine
// before evaluating, and the replay is refused even though a GET in the same
// instant still answers the superseded validator. The replay is sent as the
// sweep's probe, so the outcome reports the precondition's verdict alone.
func TestConditionalWriteIsEvaluatedAgainstTheEngine(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startPreconditionLane(t, env)

	name := deployThrowawayService(t, env, "precond-cache")
	id := serviceID(t, proc, name)
	uri := "/services/" + id + "/env"

	before := representationETag(t, proc, uri)

	applied := precondRequest(
		t, proc, http.MethodPatch, uri,
		map[string]string{"If-Match": before},
		"application/merge-patch+json", `{"PRECOND_FIRST":"1"}`,
	)
	if applied.status != http.StatusOK {
		t.Fatalf("first conditional write: status = %d (body: %s)", applied.status, applied.body)
	}

	// The engine holds the new value the moment the write returned, so the
	// resource has changed and a validator built from it must have too.
	svc := inspectService(t, env, name)

	var applying []string
	if svc.Spec.TaskTemplate.ContainerSpec != nil {
		applying = svc.Spec.TaskTemplate.ContainerSpec.Env
	}

	if !slices.Contains(applying, "PRECOND_FIRST=1") {
		t.Fatalf("the first write answered 200 without reaching the engine: %v", applying)
	}

	probe := patchProbe()

	replayed := precondRequest(
		t, proc, http.MethodPatch, uri,
		map[string]string{"If-Match": before}, probe.contentType, probe.body,
	)

	if replayed.status != http.StatusPreconditionFailed {
		t.Fatalf(
			"the superseded validator %s was admitted (status %d): the engine already "+
				"held PRECOND_FIRST=1, so If-Match had to refuse it; body: %s",
			before, replayed.status, replayed.body,
		)
	}

	if !strings.Contains(replayed.body, "API013") {
		t.Errorf("412 body does not name API013: %s", replayed.body)
	}
}

// ─── the gate ───────────────────────────────────────────────────────────

// TestEveryPreconditionedRouteIsDrivenOrExcused fails when a precondition is
// wired onto a route this file does not address.
func TestEveryPreconditionedRouteIsDrivenOrExcused(t *testing.T) {
	inventory := preconditionedRoutes(t)
	targets := preconditionTargets(preconditionFixture{})

	live := make(map[string]bool, len(inventory))

	var partial []string

	for _, route := range inventory {
		key := route.String()
		live[key] = true

		target, ok := targets[key]
		if !ok {
			if reason, excused := excusedPreconditionRoutes[key]; !excused {
				t.Errorf(
					"%s carries an If-Match precondition but is neither driven in "+
						"preconditionTargets nor excused in excusedPreconditionRoutes",
					key,
				)
			} else if strings.TrimSpace(reason) == "" {
				t.Errorf("%s has an empty excuse reason", key)
			}

			continue
		}

		if target.liveExcuse != "" || target.acceptedExcuse != "" {
			partial = append(partial, key)
		}
	}

	for key := range targets {
		if !live[key] {
			t.Errorf(
				"preconditionTargets has a stale entry %q: no such preconditioned route",
				key,
			)
		}
	}

	for key := range excusedPreconditionRoutes {
		if !live[key] {
			t.Errorf(
				"excusedPreconditionRoutes has a stale entry %q: no such preconditioned route",
				key,
			)
		}
	}

	slices.Sort(partial)
	t.Logf(
		"preconditions: %d driven, %d excused, %d partially driven\npartial:\n  %s",
		len(targets), len(excusedPreconditionRoutes), len(partial),
		strings.Join(partial, "\n  "),
	)
}

// TestEveryPreconditionedRouteHasAPairedGET pins the precondition to the
// representation a client reads its validator from. A route with no GET at the
// same URI advertises a condition nobody can satisfy.
func TestEveryPreconditionedRouteHasAPairedGET(t *testing.T) {
	routes := contractRoutes(t)

	gettable := make(map[string]bool, len(routes))
	for _, route := range routes {
		if route.Method == http.MethodGet {
			gettable[route.Pattern] = true
		}
	}

	for _, route := range preconditionedRoutes(t) {
		if !gettable[route.Pattern] {
			t.Errorf(
				"%s is preconditioned on the representation of %s, which has no GET",
				route, route.Pattern,
			)
		}
	}
}

// preconditionedRoutes returns the If-Match inventory, bracketed by the same
// chdir contractRoutes needs: contract parses a path relative to its own
// directory, and this package's test binary runs in test/e2e.
func preconditionedRoutes(t *testing.T) []contract.Route {
	t.Helper()

	var (
		found []contract.Route
		err   error
	)

	inContractDir(t, func() {
		found, err = contract.PreconditionedRoutes()
	})

	if err != nil {
		t.Fatalf("contract.PreconditionedRoutes: %v", err)
	}

	if len(found) == 0 {
		t.Fatal("contract.PreconditionedRoutes returned nothing")
	}

	return found
}
