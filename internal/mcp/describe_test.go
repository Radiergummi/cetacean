package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// describeDigest calls the describe tool and decodes its result as a Digest,
// which is what every non-raw call returns.
func describeDigest(
	t *testing.T,
	ctx context.Context,
	srv *Server,
	args map[string]any,
) cluster.Digest {
	t.Helper()

	td, ok := srv.findTool("describe")
	if !ok {
		t.Fatal("describe not registered")
	}

	out, err := td.handler(ctx, newCallToolRequest("describe", args))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var got cluster.Digest
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	return got
}

// TestDescribeAndResourceReadAgree is the design's central claim in one test:
// a service that cannot schedule explains itself without a follow-up call, and
// the tool and the subscribable resource are the same digest — one builder,
// two transports, so they cannot drift.
func TestDescribeAndResourceReadAgree(t *testing.T) {
	// Replicas is what makes this the real demo_stuck: a service at 0/1. A
	// service that asks for no replicas is 0/0 and legitimately healthy, so
	// there would be no state for a reason to explain.
	replicas := uint64(1)

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "demo_stuck"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:alpine"},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		},
	})
	c.SetTask(swarm.Task{
		ID: "t1", ServiceID: "svc1", DesiredState: swarm.TaskStateRunning,
		Status: swarm.TaskStatus{
			State: swarm.TaskStatePending,
			Err:   "no suitable node (scheduling constraints not satisfied on 1 node)",
		},
	})

	srv := newLogTestServer(t, c, &fakeLogStreamer{})

	td, ok := srv.findTool("describe")
	if !ok {
		t.Fatal("describe not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("describe", map[string]any{
		"type": "service",
		"id":   "svc1",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var got cluster.Digest
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if got.Reason == "" {
		t.Error("digest carries no reason for a service that cannot schedule")
	}

	body, err := srv.readResource(context.Background(), "cetacean://services/svc1")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	var fromResource cluster.Digest
	if err := json.Unmarshal([]byte(body), &fromResource); err != nil {
		t.Fatalf("unmarshal resource body: %v: %s", err, body)
	}

	if fromResource.Reason != got.Reason || fromResource.ID != got.ID ||
		fromResource.State != got.State {
		t.Errorf("resource read and describe disagree:\n resource %+v\n tool     %+v",
			fromResource, got)
	}
}

// TestDescribeCoversEveryDigestibleType drives describe over one resource of
// every type it accepts, so a type added to the resource tree without a
// digest builder fails here rather than at a caller.
func TestDescribeCoversEveryDigestibleType(t *testing.T) {
	c := seededDescribeCache()
	srv := newResourceTestServer(t, c)

	cases := map[string]string{
		"service": "svc1",
		"node":    "node1",
		"task":    "task1",
		"stack":   "web",
		"config":  "cfg1",
		"secret":  "sec1",
		"network": "net1",
		"volume":  "data",
	}

	for singular := range describableResourceTypes {
		id, ok := cases[singular]
		if !ok {
			t.Fatalf("no fixture for describable type %q", singular)
		}

		digest := describeDigest(t, context.Background(), srv, map[string]any{
			"type": singular,
			"id":   id,
		})

		if digest.Type != singular {
			t.Errorf("%s: digest type = %q, want %q", singular, digest.Type, singular)
		}

		if digest.ID == "" || digest.Name == "" {
			t.Errorf("%s: digest missing identity: %+v", singular, digest)
		}

		if digest.Related == nil || digest.RecentFailures == nil {
			t.Errorf("%s: nil slice would marshal to null: %+v", singular, digest)
		}
	}
}

// TestDescribeResolvesRelatedNames pins the cross-references a digest is meant
// to save a caller a second call for: a task names its parent service and the
// node it runs on, and a config names the service that mounts it.
func TestDescribeResolvesRelatedNames(t *testing.T) {
	c := seededDescribeCache()
	srv := newResourceTestServer(t, c)

	task := describeDigest(t, context.Background(), srv, map[string]any{
		"type": "task",
		"id":   "task1",
	})

	if !hasRelated(task.Related, "service", "web") {
		t.Errorf("task digest does not name its parent service: %+v", task.Related)
	}

	if !hasRelated(task.Related, "node", "manager-1") {
		t.Errorf("task digest does not name its node: %+v", task.Related)
	}

	cfg := describeDigest(t, context.Background(), srv, map[string]any{
		"type": "config",
		"id":   "cfg1",
	})

	if !hasRelated(cfg.Related, "service", "web") {
		t.Errorf("config digest does not name the service using it: %+v", cfg.Related)
	}
}

// TestDescribeSecretOmitsData holds the one rule a compact shape must not
// relax: a secret's payload never leaves the process, not even in the digest
// that replaced the raw record.
func TestDescribeSecretOmitsData(t *testing.T) {
	c := cache.New(nil)
	c.SetSecret(swarm.Secret{
		ID: "sec1",
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: "db-password"},
			Data:        []byte("super-secret"),
		},
	})

	srv := newResourceTestServer(t, c)

	td, _ := srv.findTool("describe")

	out, err := td.handler(context.Background(), newCallToolRequest("describe", map[string]any{
		"type": "secret",
		"id":   "sec1",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if strings.Contains(out, "super-secret") {
		t.Errorf("secret data leaked into digest: %s", out)
	}

	if !strings.Contains(out, "db-password") {
		t.Errorf("digest missing secret name: %s", out)
	}
}

// TestDescribeRawReturnsTheUntouchedRecord covers both halves of raw mode: the
// Docker record comes back whole, under `raw`, and the digest is still there
// beside it — describe advertises an output schema, and a tool that does must
// return content conforming to it whatever its arguments.
func TestDescribeRawReturnsTheUntouchedRecord(t *testing.T) {
	c := seededDescribeCache()
	srv := newResourceTestServer(t, c)

	td, ok := srv.findTool("describe")
	if !ok {
		t.Fatal("describe not registered")
	}

	out, err := td.handler(context.Background(), newCallToolRequest("describe", map[string]any{
		"type": "service",
		"id":   "svc1",
		"raw":  true,
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	var result struct {
		ID   string          `json:"id"`
		Name string          `json:"name"`
		Type string          `json:"type"`
		Raw  json.RawMessage `json:"raw"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, out)
	}

	if result.ID != "svc1" || result.Type != "service" {
		t.Errorf("digest fields missing from a raw result: %s", out)
	}

	var svc swarm.Service
	if err := json.Unmarshal(result.Raw, &svc); err != nil {
		t.Fatalf("unmarshal raw: %v: %s", err, out)
	}

	if svc.Spec.Name != "web" {
		t.Errorf("raw record Spec.Name = %q, want web", svc.Spec.Name)
	}
}

func TestDescribeRejectsUnknownAndMissingArguments(t *testing.T) {
	srv := newResourceTestServer(t, seededDescribeCache())

	td, ok := srv.findTool("describe")
	if !ok {
		t.Fatal("describe not registered")
	}

	cases := map[string]map[string]any{
		"unknown type":      {"type": "banana", "id": "svc1"},
		"plural type":       {"type": "services", "id": "svc1"},
		"missing type":      {"id": "svc1"},
		"missing id":        {"type": "service"},
		"unknown resource":  {"type": "service", "id": "nope"},
		"logs subresource":  {"type": "service", "id": "svc1/logs"},
		"blank identifiers": {"type": "  ", "id": "  "},
	}

	for name, args := range cases {
		if _, err := td.handler(
			context.Background(),
			newCallToolRequest("describe", args),
		); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

// TestDescribeStackOmitsMemberSecretData holds the end of the chain that keeps
// secret payloads out of a stack read: a stack rolls up whole member records,
// so reading one by name must not be the way around the redaction the secret
// endpoints apply.
//
// Nothing in this package does that redacting, and deliberately so — the cache
// nils Spec.Data on every write path (cache.go's secret ResourceMap onSet,
// replaceSecrets, and the snapshot load), and GetStackDetail says in as many
// words that re-walking here would duplicate the contract and invite it to
// drift. This test is therefore the transport-side guard on that invariant: it
// fails if a future change lets a payload reach the cache, and it covers both
// the digest and the raw record, which is the shape describe added.
func TestDescribeStackOmitsMemberSecretData(t *testing.T) {
	const payload = "super-secret"

	// Spec.Data marshals as base64, so a leak looks like the encoded form —
	// searching for the plaintext alone would pass on a response that carries
	// the whole secret.
	encoded := base64.StdEncoding.EncodeToString([]byte(payload))

	c := seededDescribeCache()

	sec, _ := c.GetSecret("sec1")
	sec.Spec.Data = []byte(payload)
	c.SetSecret(sec)

	srv := newResourceTestServer(t, c)

	td, ok := srv.findTool("describe")
	if !ok {
		t.Fatal("describe not registered")
	}

	bodies := map[string]string{}

	for _, raw := range []bool{false, true} {
		out, err := td.handler(context.Background(), newCallToolRequest("describe", map[string]any{
			"type": "stack",
			"id":   "web",
			"raw":  raw,
		}))
		if err != nil {
			t.Fatalf("raw=%v: handler: %v", raw, err)
		}

		bodies[fmt.Sprintf("describe raw=%v", raw)] = out
	}

	body, err := srv.readResource(context.Background(), "cetacean://stacks/web")
	if err != nil {
		t.Fatalf("readResource: %v", err)
	}

	bodies["resources/read"] = body

	for source, out := range bodies {
		if strings.Contains(out, payload) || strings.Contains(out, encoded) {
			t.Errorf("%s leaked the member secret's data: %s", source, out)
		}
	}

	// The raw record must still carry the secret — redacted, not dropped. It
	// sits under `raw`, beside the digest, so that a raw describe still
	// conforms to the output schema the tool advertises.
	var rawStack struct {
		Raw cache.StackDetail `json:"raw"`
	}
	if err := json.Unmarshal([]byte(bodies["describe raw=true"]), &rawStack); err != nil {
		t.Fatalf("unmarshal raw stack: %v", err)
	}

	detail := rawStack.Raw

	if len(detail.Secrets) != 1 {
		t.Fatalf("expected the stack to still list its secret, got %d", len(detail.Secrets))
	}

	if detail.Secrets[0].Spec.Data != nil {
		t.Errorf("expected Spec.Data to be nil, got %v", detail.Secrets[0].Spec.Data)
	}
}

// TestDescribeStackUnderStackOnlyGrant holds what the unfiltered stack members
// rest on: an ACL stack grant reaches the member types (acl.impliedTypes),
// including the task:<id> resources readableTasksMatching keys on. Were that to
// regress, a stack digest would quietly report zero running replicas rather
// than fail.
func TestDescribeStackUnderStackOnlyGrant(t *testing.T) {
	c := seededDescribeCache()

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("stack:web"))

	// The resolver is what lets a stack grant reach its members at all —
	// main.go wires the cache in as one, and without it the evaluator cannot
	// tell that task1 belongs to a service in this stack.
	e.SetResolver(c)

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	digest := describeDigest(t, ctxWithIdentity(), srv, map[string]any{
		"type": "stack",
		"id":   "web",
	})

	if digest.State != "running" {
		t.Errorf("state = %q, want running", digest.State)
	}

	if got := digest.Details["runningReplicas"]; got != float64(1) {
		t.Errorf("runningReplicas = %v, want 1 — the stack's tasks were filtered away", got)
	}

	if !hasRelated(digest.Related, "service", "web") {
		t.Errorf("stack digest does not list its member service: %+v", digest.Related)
	}
}

// TestDescribeTaskFallsBackWhenParentIsUnreadable covers the reason TaskDigest
// takes pointers: a caller with a task grant but no service grant must still
// get a usable digest, naming the parent by ID rather than leaking its name.
func TestDescribeTaskFallsBackWhenParentIsUnreadable(t *testing.T) {
	c := seededDescribeCache()

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("task:*"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	digest := describeDigest(t, ctxWithIdentity(), srv, map[string]any{
		"type": "task",
		"id":   "task1",
	})

	if hasRelated(digest.Related, "service", "web") {
		t.Errorf("task digest named a service the caller may not read: %+v", digest.Related)
	}

	if !hasRelated(digest.Related, "service", "svc1") {
		t.Errorf("task digest lost its parent reference entirely: %+v", digest.Related)
	}
}

// TestDescribeServiceOmitsUnreadableNetworkNames is the same rule for the
// service digest, which resolves network attachment IDs to names: an
// unfiltered network slice would let a caller without a network grant learn
// overlay names from a service they can read.
func TestDescribeServiceOmitsUnreadableNetworkNames(t *testing.T) {
	c := seededDescribeCache()

	e := acl.NewEvaluator()
	e.SetPolicy(readOnlyPolicy("service:web"))

	srv := newResourceTestServer(t, c, func(o *Options) { o.ACL = e })

	digest := describeDigest(t, ctxWithIdentity(), srv, map[string]any{
		"type": "service",
		"id":   "svc1",
	})

	if hasRelated(digest.Related, "network", "frontend") {
		t.Errorf("service digest named a network the caller may not read: %+v", digest.Related)
	}
}

func hasRelated(related []cluster.Related, resourceType, name string) bool {
	return slices.ContainsFunc(related, func(r cluster.Related) bool {
		return r.Type == resourceType && r.Name == name
	})
}

// seededDescribeCache holds one resource of every describable type, wired
// together — the task belongs to the service, the config is mounted by it,
// everything carries the stack label — so a digest's cross-references have
// something real to resolve.
func seededDescribeCache() *cache.Cache {
	labels := map[string]string{"com.docker.stack.namespace": "web"}

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID: "svc1",
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web", Labels: labels},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image:   "nginx:alpine",
					Configs: []*swarm.ConfigReference{{ConfigID: "cfg1", ConfigName: "nginx-conf"}},
					Secrets: []*swarm.SecretReference{
						{SecretID: "sec1", SecretName: "db-password"},
					},
					Mounts: []mount.Mount{
						{Type: mount.TypeVolume, Source: "data", Target: "/data"},
					},
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{{Target: "net1"}},
		},
	})
	c.SetNode(swarm.Node{
		ID:          "node1",
		Spec:        swarm.NodeSpec{Role: swarm.NodeRoleManager},
		Description: swarm.NodeDescription{Hostname: "manager-1"},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
	})
	c.SetTask(swarm.Task{
		ID:           "task1",
		ServiceID:    "svc1",
		NodeID:       "node1",
		DesiredState: swarm.TaskStateRunning,
		Status:       swarm.TaskStatus{State: swarm.TaskStateRunning},
	})
	c.SetConfig(swarm.Config{
		ID:   "cfg1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx-conf", Labels: labels}},
	})
	c.SetSecret(swarm.Secret{
		ID:   "sec1",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db-password", Labels: labels}},
	})
	c.SetNetwork(network.Summary{
		ID:     "net1",
		Name:   "frontend",
		Driver: "overlay",
		Scope:  "swarm",
		Labels: labels,
	})
	c.SetVolume(volume.Volume{Name: "data", Driver: "local", Labels: labels})

	return c
}
