//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives the MCP surfaces that stream or span more than one request:
// the subscriptions/listen notification stream and its ACL filtering, the
// completions capability, and the tasks extension a mutation is augmented
// with. It reserves port 19016 (see README.md's reserved-ports table).
//
// The valuable half is the security one. A notification is the one thing the
// server sends unasked, so a delivery that ignores the caller's grants
// discloses that a resource exists and just changed. Completions are the same
// hazard in the other direction: a list of names the caller cannot read.
//
// Identity arrives through CETACEAN_MCP_AUTH_BYPASS=headers, as in
// mcp_sweep_test.go — a supported deployment, and the same reasoning applies:
// this file tests what an established identity may receive, not how it was
// established. The OAuth flow is oauth_test.go's lane.

const mcpStreamPort = 19016

// mcpStreamPolicy separates three callers by what they may read, which is what
// makes a withheld notification distinguishable from one that simply never
// fired: "all" reads everything, "services" reads services (and, through the
// resolver, their tasks) and nothing else, and "nobody" matches no grant at
// all.
const mcpStreamPolicy = `grants:
  - resources: ["*"]
    audience: ["group:stream-all"]
    permissions: ["read", "write"]

  - resources: ["service:*"]
    audience: ["group:stream-services"]
    permissions: ["read"]
`

var (
	streamAll      = readPersona{name: "all", user: "all@example.com", groups: "stream-all"}
	streamServices = readPersona{
		name:   "services",
		user:   "services@example.com",
		groups: "stream-services",
	}
	streamNobody = readPersona{name: "nobody", user: "nobody@example.com", groups: ""}
)

func startMCPStream(t *testing.T, env *harness.Env, extra map[string]string) *sut.Process {
	t.Helper()

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(mcpStreamPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	config := map[string]string{
		"CETACEAN_AUTH_MODE":            "headers",
		"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
		"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
		"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
		"CETACEAN_ACL_POLICY_FILE":      policy,
		"CETACEAN_OPERATIONS_LEVEL":     "3",
		"CETACEAN_MCP":                  "true",
		"CETACEAN_MCP_AUTH_BYPASS":      "headers",
	}

	maps.Copy(config, extra)

	return sut.Start(t, sut.Config{Port: mcpStreamPort, DockerHost: env.DockerHost, Env: config})
}

// ─── resolving through an authenticated read ────────────────────────────

// The SUT here authenticates, so the lane cannot use the write sweep's
// uncredentialed serviceID and awaitCached: both would be answered 401 and the
// failure would look like a missing resource. These two are the same reads,
// issued as a persona.

// serviceIdentAs resolves a service name to its ID through an authenticated
// listing.
func serviceIdentAs(t *testing.T, proc *sut.Process, persona readPersona, name string) string {
	t.Helper()

	resp := readAs(t, proc, persona, "/services", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /services as %s: status = %d", persona.name, resp.StatusCode)
	}

	var body struct {
		Items []struct {
			ID   string `json:"ID"`
			Spec struct {
				Name string `json:"Name"`
			} `json:"Spec"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /services: %v", err)
	}

	for _, item := range body.Items {
		if item.Spec.Name == name {
			return item.ID
		}
	}

	t.Fatalf("service %q is not in /services as %s", name, persona.name)

	return ""
}

// awaitCachedAs waits until Cetacean's own cache holds the resource. Every
// resource this lane creates is created on the engine, and the watcher fills
// the cache asynchronously; subscribing or completing before that is a race,
// not a result.
func awaitCachedAs(t *testing.T, proc *sut.Process, persona readPersona, path string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		resp := readAs(t, proc, persona, path, "")
		status := resp.StatusCode
		resp.Body.Close()

		if status == http.StatusOK {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("Cetacean never served %s to %s (last status %d)", path, persona.name, status)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// ─── the notification stream ────────────────────────────────────────────

// mcpNotification is one JSON-RPC notification delivered on a listen stream.
type mcpNotification struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// uri returns the resource a resources/updated notification names, or "".
func (n mcpNotification) uri() string {
	uri, _ := n.Params["uri"].(string)

	return uri
}

// listenStream is an open subscriptions/listen stream.
type listenStream struct {
	persona       readPersona
	notifications <-chan mcpNotification

	// acknowledged is the subset of the requested filter the server reported
	// establishing, which is the inventory the gate at the end of this file
	// reads.
	acknowledged []string
}

// openListen opens a subscriptions/listen stream as persona and blocks until
// the server acknowledges it. The acknowledgement is the first message on the
// stream by protocol, and waiting for it is what makes a later event
// impossible to miss: a subscription established after the mutation would see
// nothing, and the test would read that as a withheld notification.
func openListen(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	filter map[string]any,
) listenStream {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "subscriptions/listen",
		"params": map[string]any{
			"notifications": filter,
			"_meta": map[string]any{
				"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal listen request: %v", err)
	}

	req, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, proc.BaseURL+"/mcp", bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("new listen request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	req.Header.Set("X-Auth-User", persona.user)

	if persona.groups != "" {
		req.Header.Set("X-Auth-Groups", persona.groups)
	}

	resp, err := proc.StreamClient().Do(req)
	if err != nil {
		t.Fatalf("listen as %s: %v", persona.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("listen as %s: status = %d", persona.name, resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		resp.Body.Close()
		t.Fatalf("listen as %s: Content-Type = %q, want text/event-stream", persona.name, ct)
	}

	frames := make(chan sseFrame, 256)
	done := make(chan struct{})

	t.Cleanup(func() {
		close(done)
		resp.Body.Close()
	})

	go readSSEFrames(resp.Body, frames, done)

	notifications := make(chan mcpNotification, 256)

	go func() {
		defer close(notifications)

		for frame := range frames {
			var n mcpNotification
			if err := json.Unmarshal([]byte(frame.data), &n); err != nil {
				continue
			}

			notifications <- n
		}
	}()

	stream := listenStream{persona: persona, notifications: notifications}

	ack := stream.await(t, 30*time.Second, func(n mcpNotification) bool {
		return n.Method == "notifications/subscriptions/acknowledged"
	})

	fields, ok := ack.Params["notifications"].(map[string]any)
	if !ok {
		t.Fatalf("%s: the acknowledgement names no established filter: %+v", persona.name, ack)
	}

	for field, value := range fields {
		switch v := value.(type) {
		case bool:
			if !v {
				continue
			}
		case []any:
			if len(v) == 0 {
				continue
			}
		}

		stream.acknowledged = append(stream.acknowledged, field)
	}

	if len(stream.acknowledged) == 0 {
		t.Fatalf("%s: the server established none of the requested filter", persona.name)
	}

	return stream
}

// await drains the stream until match reports true, returning that
// notification and failing on timeout.
func (s listenStream) await(
	t *testing.T,
	within time.Duration,
	match func(mcpNotification) bool,
) mcpNotification {
	t.Helper()

	deadline := time.After(within)

	for {
		select {
		case n, ok := <-s.notifications:
			if !ok {
				t.Fatalf("%s: the stream closed before the expected notification", s.persona.name)
			}

			if match(n) {
				return n
			}
		case <-deadline:
			t.Fatalf("%s: the expected notification never arrived", s.persona.name)
		}
	}
}

// awaitBarrier drains until match reports true and returns everything seen
// before it, so an absence can be asserted over a window whose end is a
// delivery that had to come after the one being ruled out. Same session, same
// channel, so ordering is not an assumption.
func (s listenStream) awaitBarrier(
	t *testing.T,
	within time.Duration,
	match func(mcpNotification) bool,
) []mcpNotification {
	t.Helper()

	var seen []mcpNotification

	deadline := time.After(within)

	for {
		select {
		case n, ok := <-s.notifications:
			if !ok {
				t.Fatalf("%s: the stream closed before the barrier", s.persona.name)
			}

			if match(n) {
				return seen
			}

			seen = append(seen, n)
		case <-deadline:
			t.Fatalf(
				"%s: the barrier never arrived; saw %d notifications",
				s.persona.name, len(seen),
			)
		}
	}
}

// drain returns everything already buffered without waiting.
func (s listenStream) drain() []mcpNotification {
	var seen []mcpNotification

	for {
		select {
		case n, ok := <-s.notifications:
			if !ok {
				return seen
			}

			seen = append(seen, n)
		default:
			return seen
		}
	}
}

// touchService writes a label to a service on the engine, which is what makes
// the watcher emit a service update the cache turns into a notification. The
// change is made through Docker rather than through Cetacean so the
// notification path is driven by a third party's write, as it would be in
// production.
func touchService(t *testing.T, env *harness.Env, name, value string) {
	t.Helper()

	ctx := context.Background()

	svc, _, err := env.Docker.ServiceInspectWithRaw(ctx, name, swarm.ServiceInspectOptions{})
	if err != nil {
		t.Fatalf("ServiceInspectWithRaw %s: %v", name, err)
	}

	if svc.Spec.Labels == nil {
		svc.Spec.Labels = map[string]string{}
	}

	svc.Spec.Labels["cetacean.e2e.touch"] = value

	if _, err := env.Docker.ServiceUpdate(
		ctx, svc.ID, svc.Version, svc.Spec, swarm.ServiceUpdateOptions{},
	); err != nil {
		t.Fatalf("ServiceUpdate %s: %v", name, err)
	}
}

// TestMCPSubscriptionsDeliverAndRespectReadGrants drives the property a
// notification stream lives or dies by: a resources/updated reaches a
// subscriber that may read the resource and no one else. Both callers
// subscribe to the same two URIs, so a withheld notification is the ACL's
// doing and not a missing subscription.
func TestMCPSubscriptionsDeliverAndRespectReadGrants(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, nil)

	configName := sweepName("mcpstream-config")
	config := engineConfig(t, env, configName, nil)

	stack := fixtures.DeployStack(t, env, "mcpstream", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	service := stack + "_app"
	serviceIdent := serviceIdentAs(t, proc, streamAll, service)

	awaitCachedAs(t, proc, streamAll, "/configs/"+config)
	awaitCachedAs(t, proc, streamAll, "/services/"+serviceIdent)

	configURI := "cetacean://configs/" + config
	serviceURI := "cetacean://services/" + serviceIdent

	filter := map[string]any{"resourceSubscriptions": []string{configURI, serviceURI}}

	all := openListen(t, proc, streamAll, filter)
	services := openListen(t, proc, streamServices, filter)

	isConfig := func(n mcpNotification) bool {
		return n.Method == "notifications/resources/updated" && n.uri() == configURI
	}
	isService := func(n mcpNotification) bool {
		return n.Method == "notifications/resources/updated" && n.uri() == serviceURI
	}

	// The config is touched first and waited for, and only then the service.
	// Dispatch walks every session in one pass per cache event, so once the
	// all-reading stream has the config notification, every session that was
	// going to receive one already has — and the service change that follows
	// is strictly later, which makes it a usable barrier for the absence
	// asserted below.
	touchConfig(t, env, config, configName)

	all.await(t, 90*time.Second, isConfig)

	touchService(t, env, service, "1")

	all.await(t, 90*time.Second, isService)

	for _, n := range services.awaitBarrier(t, 90*time.Second, isService) {
		if isConfig(n) {
			t.Errorf("a caller granted read on services only was told %s changed", configURI)
		}
	}
}

// touchConfig relabels a config on the engine, which is what makes the watcher
// emit the update a subscriber is notified of. Docker configs are immutable
// apart from their labels, which is exactly the mutation wanted here.
func touchConfig(t *testing.T, env *harness.Env, id, name string) {
	t.Helper()

	ctx := context.Background()

	cfg, _, err := env.Docker.ConfigInspectWithRaw(ctx, id)
	if err != nil {
		t.Fatalf("ConfigInspectWithRaw %s: %v", id, err)
	}

	spec := cfg.Spec
	spec.Name = name

	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}

	spec.Labels["cetacean.e2e.touch"] = "1"

	if err := env.Docker.ConfigUpdate(ctx, id, cfg.Version, spec); err != nil {
		t.Fatalf("ConfigUpdate %s: %v", id, err)
	}
}

// TestMCPNotificationTypesAreOptIn drives the rule 2026-07-28 states directly:
// a server MUST NOT send a notification type the client did not request. The
// subscriber below asks only for resource subscriptions, so a
// resources/list_changed on its stream is a violation however much the cluster
// is churning — and its own resources/updated is the barrier proving the
// window it is asserted over was a live one.
func TestMCPNotificationTypesAreOptIn(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, nil)

	stack := fixtures.DeployStack(t, env, "mcpoptin", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	service := stack + "_app"
	serviceIdent := serviceIdentAs(t, proc, streamAll, service)
	awaitCachedAs(t, proc, streamAll, "/services/"+serviceIdent)

	serviceURI := "cetacean://services/" + serviceIdent

	quiet := openListen(t, proc, streamAll, map[string]any{
		"resourceSubscriptions": []string{serviceURI},
	})
	listening := openListen(t, proc, streamAll, map[string]any{
		"resourcesListChanged":  true,
		"resourceSubscriptions": []string{serviceURI},
	})

	// A create is what list_changed reports; an update is not.
	created := engineConfig(t, env, sweepName("mcpoptin-config"), nil)
	awaitCachedAs(t, proc, streamAll, "/configs/"+created)

	touchService(t, env, service, "1")

	isService := func(n mcpNotification) bool {
		return n.Method == "notifications/resources/updated" && n.uri() == serviceURI
	}

	listening.await(t, 90*time.Second, func(n mcpNotification) bool {
		return n.Method == "notifications/resources/list_changed"
	})

	for _, n := range quiet.awaitBarrier(t, 90*time.Second, isService) {
		if n.Method == "notifications/resources/list_changed" {
			t.Errorf("a stream that opted into resource subscriptions only was sent %s", n.Method)
		}
	}
}

// TestMCPListChangedIsWithheldFromACallerWithNoGrants drives the coarser half
// of the same boundary. resources/list_changed carries no resource URI, so
// what it discloses is timing: that something of a type just appeared or went
// away. A caller matching no grant reads nothing of any type, so the
// notification must never reach it.
//
// Both streams also name a resource subscription. That is not decoration:
// see TestMCPListChangedReachesAFilterOnlySubscriber, which pins finding
// D-11 — a stream opting into list_changed alone never has an identity
// recorded, so this assertion would hold for a reason that has nothing to do
// with the grant being checked.
func TestMCPListChangedIsWithheldFromACallerWithNoGrants(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, nil)

	stack := fixtures.DeployStack(t, env, "mcpnogrant", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	id := serviceIdentAs(t, proc, streamAll, stack+"_app")
	awaitCachedAs(t, proc, streamAll, "/services/"+id)

	filter := map[string]any{
		"resourcesListChanged":  true,
		"resourceSubscriptions": []string{"cetacean://services/" + id},
	}

	granted := openListen(t, proc, streamAll, filter)
	ungranted := openListen(t, proc, streamNobody, filter)

	created := engineConfig(t, env, sweepName("mcpnogrant-config"), nil)
	awaitCachedAs(t, proc, streamAll, "/configs/"+created)

	isListChanged := func(n mcpNotification) bool {
		return n.Method == "notifications/resources/list_changed"
	}

	granted.await(t, 90*time.Second, isListChanged)

	// The dispatcher writes to every matching session in one pass over one
	// cache event, so by the time the granted stream has read its copy off the
	// wire, an ungranted delivery would already have been written. The extra
	// second covers the trip through the two SSE writers, not a race in the
	// dispatch itself.
	time.Sleep(time.Second)

	for _, n := range ungranted.drain() {
		if isListChanged(n) {
			t.Error("a caller matching no grant was told a resource list changed")
		}
	}
}

// TestMCPListChangedReachesAFilterOnlySubscriber drives the subscription form
// 2026-07-28 makes ordinary: opting into a list_changed notification without
// naming any resource to watch. The revision's filter has four independent
// fields and nothing couples them, so a client that only wants to know when to
// refetch is a conforming client.
//
// It never hears anything, whenever an ACL policy is configured.
// internal/mcp/notifications.go records the caller's identity in Subscribe
// (:75), which runs once per requested URI — and in nothing else. SetFilter
// (:81), the other half of the same hook, creates the session record without
// one. So a filter-only stream is left with a nil identity, and
// listChangedTargets hands that nil to canReadAnyOfType, which under a policy
// matches no grant and withholds the notification.
//
// Quarantined per finding D-11: the silence is tolerated only while a stream
// that differs by naming a resource subscription does receive it, which is
// what shows the event fired and was dispatched.
func TestMCPListChangedReachesAFilterOnlySubscriber(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, nil)

	stack := fixtures.DeployStack(t, env, "mcpfilteronly", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	id := serviceIdentAs(t, proc, streamAll, stack+"_app")
	awaitCachedAs(t, proc, streamAll, "/services/"+id)

	filterOnly := openListen(t, proc, streamAll, map[string]any{"resourcesListChanged": true})

	witness := openListen(t, proc, streamAll, map[string]any{
		"resourcesListChanged":  true,
		"resourceSubscriptions": []string{"cetacean://services/" + id},
	})

	created := engineConfig(t, env, sweepName("mcpfilteronly-config"), nil)
	awaitCachedAs(t, proc, streamAll, "/configs/"+created)

	isListChanged := func(n mcpNotification) bool {
		return n.Method == "notifications/resources/list_changed"
	}

	// The witness holds the same grants and the same list_changed opt-in, and
	// differs only in naming a URI. Its delivery is what makes the silence
	// below attributable.
	witness.await(t, 90*time.Second, isListChanged)

	time.Sleep(time.Second)

	if slices.ContainsFunc(filterOnly.drain(), isListChanged) {
		return
	}

	t.Logf(
		"FINDING D-11: a subscriptions/listen stream opting into " +
			"resourcesListChanged alone received nothing, while a stream with the " +
			"same identity and the same opt-in that also named a resource " +
			"subscription received the notification. internal/mcp/notifications.go " +
			"records the caller's identity only in Subscribe, which runs per " +
			"requested URI; SetFilter creates the session record without one, so " +
			"listChangedTargets hands a nil identity to canReadAnyOfType and a " +
			"configured policy withholds the notification. The filter's four " +
			"fields are independent in the revision, so this is a conforming " +
			"client that is silently never told to refetch.",
	)
}

// ─── completions ────────────────────────────────────────────────────────

type completionResult struct {
	Completion struct {
		Values  []string `json:"values"`
		Total   int      `json:"total"`
		HasMore bool     `json:"hasMore"`
	} `json:"completion"`
}

func complete(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	ref map[string]any,
	argument map[string]any,
) completionResult {
	t.Helper()

	envelope, status := mcpAs(t, proc, persona, "completion/complete", map[string]any{
		"ref":      ref,
		"argument": argument,
	})

	if envelope.Error != nil {
		t.Fatalf("completion/complete as %s: %s", persona.name, *envelope.Error)
	}

	if status != http.StatusOK {
		t.Fatalf("completion/complete as %s: status = %d", persona.name, status)
	}

	var result completionResult
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode completion: %v (%s)", err, envelope.Result)
	}

	return result
}

// TestMCPCompletionsAreFilteredByReadGrants drives the disclosure hazard
// completion.go's own comment names: the values it offers are inserted
// literally, so they have to be names the caller could have read anyway.
// Reading the cache directly would make a dropdown an enumeration of
// everything.
func TestMCPCompletionsAreFilteredByReadGrants(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, nil)

	configName := sweepName("mcpcomplete-config")
	config := engineConfig(t, env, configName, nil)

	stack := fixtures.DeployStack(t, env, "mcpcomplete", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	service := stack + "_app"

	awaitCachedAs(t, proc, streamAll, "/configs/"+config)
	awaitCachedAs(t, proc, streamAll, "/services/"+serviceIdentAs(t, proc, streamAll, service))

	configRef := map[string]any{"type": "ref/resource", "uri": "cetacean://configs/{id}"}
	serviceRef := map[string]any{"type": "ref/resource", "uri": "cetacean://services/{id}"}

	blank := map[string]any{"name": "id", "value": ""}

	// Every combination of caller and type, so a withheld completion is the
	// grant's doing rather than an empty cluster: the same type the
	// services-only caller is refused, the all-reading caller is offered.
	for _, tc := range []struct {
		persona readPersona
		ref     map[string]any
		what    string
		want    string
	}{
		{streamAll, configRef, "config", configName},
		{streamAll, serviceRef, "service", service},
		{streamServices, serviceRef, "service", service},
		{streamServices, configRef, "config", ""},
		{streamNobody, serviceRef, "service", ""},
		{streamNobody, configRef, "config", ""},
	} {
		values := complete(t, proc, tc.persona, tc.ref, blank).Completion.Values

		if tc.want == "" {
			if len(values) != 0 {
				t.Errorf(
					"completing a %s as %s offered %v, though it may read none",
					tc.what, tc.persona.name, values,
				)
			}

			continue
		}

		if !slices.Contains(values, tc.want) {
			t.Errorf(
				"completing a %s as %s omitted %q: %v",
				tc.what, tc.persona.name, tc.want, values,
			)
		}
	}

	// Narrowing means what find's query means — a substring, since a Docker
	// name carries its stack as a prefix.
	narrowed := complete(t, proc, streamAll, serviceRef, map[string]any{
		"name": "id", "value": "_app",
	})
	if !slices.Contains(narrowed.Completion.Values, service) {
		t.Errorf("a substring of the name did not match it: %v", narrowed.Completion.Values)
	}

	if empty := complete(t, proc, streamAll, serviceRef, map[string]any{
		"name": "id", "value": "cetacean-e2e-no-such-service",
	}); len(empty.Completion.Values) != 0 {
		t.Errorf("a value matching nothing offered %v", empty.Completion.Values)
	}

	// A prompt argument is named after the resource type it takes, which is
	// the map describe already derives rather than a second table.
	prompt := complete(t, proc, streamAll,
		map[string]any{"type": "ref/prompt", "name": "diagnose_service"},
		map[string]any{"name": "service", "value": ""},
	)
	if !slices.Contains(prompt.Completion.Values, service) {
		t.Errorf("completing a prompt's service argument omitted %q: %v",
			service, prompt.Completion.Values)
	}
}

// ─── the tasks extension ────────────────────────────────────────────────

type mcpTask struct {
	TaskID        string `json:"taskId"`
	Status        string `json:"status"`
	StatusMessage string `json:"statusMessage"`
	TTL           *int64 `json:"ttl"`
}

// callAsTask issues a task-augmented tools/call and returns the task record
// the server answers with.
func callAsTask(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	tool string,
	arguments map[string]any,
	task map[string]any,
) mcpTask {
	t.Helper()

	params := map[string]any{"name": tool, "arguments": arguments}
	if task != nil {
		params["task"] = task
	}

	envelope, status := mcpAs(t, proc, persona, "tools/call", params)
	if envelope.Error != nil {
		t.Fatalf("tools/call %s as a task: %s", tool, *envelope.Error)
	}

	if status != http.StatusOK {
		t.Fatalf("tools/call %s as a task: status = %d", tool, status)
	}

	var result struct {
		Task mcpTask `json:"task"`
	}

	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode task: %v (%s)", err, envelope.Result)
	}

	if result.Task.TaskID == "" {
		t.Fatalf("a task-augmented call answered with no task: %s", envelope.Result)
	}

	return result.Task
}

// getTask reads a task record, reporting whether the server still holds it.
func getTask(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	id string,
) (mcpTask, bool) {
	t.Helper()

	envelope, _ := mcpAs(t, proc, persona, "tasks/get", map[string]any{"taskId": id})
	if envelope.Error != nil {
		return mcpTask{}, false
	}

	var task mcpTask
	if err := json.Unmarshal(envelope.Result, &task); err != nil {
		t.Fatalf("decode task: %v (%s)", err, envelope.Result)
	}

	return task, true
}

// awaitTerminalTask polls a task record until it reports a terminal status.
func awaitTerminalTask(t *testing.T, proc *sut.Process, id string) mcpTask {
	t.Helper()

	deadline := time.Now().Add(3 * time.Minute)

	var last mcpTask

	for time.Now().Before(deadline) {
		current, ok := getTask(t, proc, streamAll, id)
		if !ok {
			t.Fatalf("the task record went away before it reached a terminal state")
		}

		last = current

		switch current.Status {
		case "completed", "failed", "cancelled":
			return current
		}

		time.Sleep(2 * time.Second)
	}

	t.Fatalf("the task never reached a terminal state (last: %q)", last.Status)

	return last
}

// TestMCPTaskAugmentedMutationRunsToConvergence drives the four converging
// tools' reason for existing: a task-augmented scale returns immediately with
// a working task, and the task reaches completed only once the service has
// actually settled on the cluster.
//
// It never does. mcp-go runs a task-augmented tool on a goroutine holding the
// HTTP request context, which net/http cancels the moment the create-task
// response is written — so by the time the handler runs, its context is
// already dead. internal/mcp/tasks.go knows this and detaches the context for
// the convergence *wait* (awaitServiceConvergence's doc comment says so in as
// many words), but the Docker call that precedes the wait still takes ctx: the
// inspect opening internal/docker/client.go's ScaleService fails with
// "context canceled" before any write is issued.
//
// Quarantined per finding D-12: the drop is tolerated only when the task says
// cancelled for that exact reason and the cluster is provably untouched.
// Anything else — a failure with a different cause, or a cancellation that did
// change the cluster — fails.
func TestMCPTaskAugmentedMutationRunsToConvergence(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, nil)

	stack := fixtures.DeployStack(t, env, "mcptask", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	service := stack + "_app"
	awaitCachedAs(t, proc, streamAll, "/services/"+serviceIdentAs(t, proc, streamAll, service))

	task := callAsTask(t, proc, streamAll, "scale_service",
		map[string]any{"id": service, "replicas": 3},
		map[string]any{"ttl": 600000},
	)

	if task.Status != "working" {
		t.Errorf("a task-augmented mutation answered status %q, want working", task.Status)
	}

	settled := awaitTerminalTask(t, proc, task.TaskID)

	// The cluster decides, not the status: a task reporting completed while
	// the replicas do not exist is exactly what this extension exists to rule
	// out, and a task reporting cancelled while they do exist would mean the
	// mutation landed and the report is wrong.
	replicas := engineReplicas(t, env, service)

	if settled.Status == "completed" {
		if replicas != 3 {
			t.Fatalf(
				"the task reported convergence with %d replicas on the engine, want 3",
				replicas,
			)
		}

		if tasks := len(serviceTaskIDs(t, env, inspectService(t, env, service).ID)); tasks < 3 {
			t.Errorf("the task reported convergence with %d tasks on the engine, want 3", tasks)
		}

		return
	}

	dropped := settled.Status == "cancelled" &&
		strings.Contains(settled.StatusMessage, "context canceled")

	if !dropped {
		t.Fatalf(
			"the task settled at %q (%s), want completed",
			settled.Status, settled.StatusMessage,
		)
	}

	if replicas != 1 {
		t.Fatalf(
			"the task reported %q, yet the engine moved to %d replicas; the mutation "+
				"landed and the report is wrong, which is not the drop D-12 describes",
			settled.Status, replicas,
		)
	}

	t.Logf(
		"FINDING D-12: a task-augmented scale_service never reached the engine. "+
			"The task reported %q with %q and the service still holds %d replica(s). "+
			"mcp-go runs the handler on a goroutine holding the already-cancelled "+
			"HTTP request context, and internal/mcp/tasks.go detaches it for the "+
			"convergence wait but not for the Docker call before it, so "+
			"ScaleService's opening inspect fails outright. Every one of the four "+
			"converging tools takes this path, and the caller is told \"cancelled\" — "+
			"which reads as a cancellation they requested, not a mutation that "+
			"silently did not happen.",
		settled.Status, settled.StatusMessage, replicas,
	)
}

// engineReplicas reads a replicated service's desired replica count straight
// from the engine.
func engineReplicas(t *testing.T, env *harness.Env, name string) uint64 {
	t.Helper()

	svc := inspectService(t, env, name)
	if svc.Spec.Mode.Replicated == nil || svc.Spec.Mode.Replicated.Replicas == nil {
		t.Fatalf("service %s is not replicated: %+v", name, svc.Spec.Mode)
	}

	return *svc.Spec.Mode.Replicated.Replicas
}

// TestMCPTaskRetentionIsBounded drives what tasks.go documents mcp-go cannot
// do for itself: a call omitting params.task.ttl would otherwise pin its
// result for the life of the process, and one naming an enormous ttl would be
// taken at its word. Both are bounded by configuration, so the lane runs
// against a SUT whose bounds are seconds rather than the default quarter hour.
func TestMCPTaskRetentionIsBounded(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, map[string]string{
		"CETACEAN_MCP_TASK_TTL":     "5s",
		"CETACEAN_MCP_MAX_TASK_TTL": "5s",
	})

	stack := fixtures.DeployStack(t, env, "mcpttl", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	service := stack + "_app"
	awaitCachedAs(t, proc, streamAll, "/services/"+serviceIdentAs(t, proc, streamAll, service))

	t.Run("a requested ttl is clamped to the ceiling", func(t *testing.T) {
		task := callAsTask(t, proc, streamAll, "restart_service",
			map[string]any{"id": service},
			map[string]any{"ttl": 3600000},
		)

		if task.TTL == nil {
			t.Fatalf("the task carries no ttl at all: %+v", task)
		}

		if *task.TTL != 5000 {
			t.Errorf("ttl = %dms, want the configured ceiling of 5000ms", *task.TTL)
		}

		// What the mutation then does is D-12's business, driven by
		// TestMCPTaskAugmentedMutationRunsToConvergence. The clamp is decided
		// before the handler runs — installTaskTTLHook fills the field in on
		// AddBeforeCallTool — so it is observable whatever becomes of the call.
	})

	t.Run("an omitted ttl is filled in and released", func(t *testing.T) {
		task := callAsTask(t, proc, streamAll, "restart_service",
			map[string]any{"id": service},
			map[string]any{},
		)

		if task.TTL == nil || *task.TTL != 5000 {
			t.Fatalf("an omitted ttl was not filled in from configuration: %+v", task)
		}

		if _, ok := getTask(t, proc, streamAll, task.TaskID); !ok {
			t.Fatal("the task record was gone immediately, before its ttl could elapse")
		}

		deadline := time.Now().Add(2 * time.Minute)

		for time.Now().Before(deadline) {
			if _, ok := getTask(t, proc, streamAll, task.TaskID); !ok {
				return
			}

			time.Sleep(2 * time.Second)
		}

		t.Error("the task record outlived its ttl, so an omitted one leaks a result per call")
	})
}

// ─── the gate ───────────────────────────────────────────────────────────

// drivenNotificationTypes names the subscription filter fields this lane
// drives, and excusedNotificationTypes carries a reason for the rest. The
// inventory itself comes from the server: a subscriptions/listen request
// asking for everything is answered with the subset it actually established,
// after mcp-go intersects the request with the advertised capabilities.
var (
	drivenNotificationTypes = map[string]string{
		"resourceSubscriptions": "TestMCPSubscriptionsDeliverAndRespectReadGrants",
		"resourcesListChanged": "TestMCPListChangedIsWithheldFromACallerWithNoGrants " +
			"and TestMCPListChangedReachesAFilterOnlySubscriber",
	}

	excusedNotificationTypes = map[string]string{
		"toolsListChanged": "mcp-go establishes it because internal/mcp/server.go " +
			"declares WithToolCapabilities(true), but Cetacean never emits " +
			"notifications/tools/list_changed: the catalog is built once at startup and " +
			"filtered per identity at list time, so nothing changes it while a stream is " +
			"open, and there is no event to drive",
		"promptsListChanged": "same as toolsListChanged — the prompt catalog is static, " +
			"and internal/mcp/server.go declares WithPromptCapabilities(false) for it",
	}
)

// TestEveryEstablishedNotificationTypeIsDrivenOrExcused fails when the server
// starts establishing a notification type this lane neither drives nor
// excuses. Asking for every field and reading back what was established is
// what makes this an inventory rather than a restatement: a capability turned
// on in internal/mcp/server.go shows up here without anyone editing a list.
func TestEveryEstablishedNotificationTypeIsDrivenOrExcused(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := startMCPStream(t, env, nil)

	stack := fixtures.DeployStack(t, env, "mcpgate", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	id := serviceIdentAs(t, proc, streamAll, stack+"_app")
	awaitCachedAs(t, proc, streamAll, "/services/"+id)

	stream := openListen(t, proc, streamAll, map[string]any{
		"toolsListChanged":      true,
		"promptsListChanged":    true,
		"resourcesListChanged":  true,
		"resourceSubscriptions": []string{"cetacean://services/" + id},
	})

	established := stream.acknowledged

	if len(established) == 0 {
		t.Fatal("the server established nothing, so this gate would pass vacuously")
	}

	for _, field := range established {
		if _, ok := drivenNotificationTypes[field]; ok {
			continue
		}

		reason, excused := excusedNotificationTypes[field]
		if !excused {
			t.Errorf(
				"the server establishes %q but this lane neither drives it nor excuses it",
				field,
			)

			continue
		}

		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q has an empty excuse reason", field)
		}
	}

	for field := range drivenNotificationTypes {
		if !slices.Contains(established, field) {
			t.Errorf(
				"drivenNotificationTypes claims %q, which the server no longer establishes",
				field,
			)
		}
	}

	slices.Sort(established)
	t.Logf("established notification types: %s", strings.Join(established, ", "))
}
