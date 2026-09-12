//go:build e2e

package e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/volume"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// Resource lifecycle drivers for the write sweep: creating and removing the
// data resources, and removing the four resource types Cetacean can remove but
// not create. Registered in drivenWriteRoutes, so both of the sweep's
// top-level tests pick them up. Every case verifies through the engine, never
// through Cetacean's own response -- a handler that answers 201 or 204 without
// touching the cluster must not pass.

// sweepRunID distinguishes the resources these drivers create from those of
// any earlier run: the harness containers outlive a `go test` invocation, so a
// run that failed before cleanup leaves resources a fixed name would 409 on.
var sweepRunID = func() string {
	raw := make([]byte, 4)
	if _, err := rand.Read(raw); err != nil {
		panic("write sweep: crypto/rand failure: " + err.Error())
	}

	return hex.EncodeToString(raw)
}()

func sweepName(prefix string) string {
	return prefix + "-" + sweepRunID
}

// ─── engine-side fixtures ───────────────────────────────────────────────

// engineConfig creates a config directly on the engine and returns its ID,
// registering a cleanup that tolerates the case having already removed it.
// Removal cases need a resource Cetacean did not create, so that the case
// proves removal rather than merely undoing its own creation.
func engineConfig(t *testing.T, env *harness.Env, name string, labels map[string]string) string {
	t.Helper()

	created, err := env.Docker.ConfigCreate(context.Background(), swarm.ConfigSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        []byte("cetacean-e2e-config"),
	})
	if err != nil {
		t.Fatalf("ConfigCreate %s: %v", name, err)
	}

	t.Cleanup(func() {
		if err := env.Docker.ConfigRemove(context.Background(), created.ID); err != nil &&
			!cerrdefs.IsNotFound(err) {
			t.Errorf("cleanup: ConfigRemove %s: %v", name, err)
		}
	})

	return created.ID
}

func engineSecret(t *testing.T, env *harness.Env, name string, labels map[string]string) string {
	t.Helper()

	created, err := env.Docker.SecretCreate(context.Background(), swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name, Labels: labels},
		Data:        []byte("cetacean-e2e-secret"),
	})
	if err != nil {
		t.Fatalf("SecretCreate %s: %v", name, err)
	}

	t.Cleanup(func() {
		if err := env.Docker.SecretRemove(context.Background(), created.ID); err != nil &&
			!cerrdefs.IsNotFound(err) {
			t.Errorf("cleanup: SecretRemove %s: %v", name, err)
		}
	})

	return created.ID
}

func engineNetwork(t *testing.T, env *harness.Env, name string) string {
	t.Helper()

	created, err := env.Docker.NetworkCreate(context.Background(), name, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
	})
	if err != nil {
		t.Fatalf("NetworkCreate %s: %v", name, err)
	}

	t.Cleanup(func() {
		if err := env.Docker.NetworkRemove(context.Background(), created.ID); err != nil &&
			!cerrdefs.IsNotFound(err) {
			t.Errorf("cleanup: NetworkRemove %s: %v", name, err)
		}
	})

	return created.ID
}

func engineVolume(t *testing.T, env *harness.Env, name string) {
	t.Helper()

	if _, err := env.Docker.VolumeCreate(context.Background(), volume.CreateOptions{
		Name: name,
	}); err != nil {
		t.Fatalf("VolumeCreate %s: %v", name, err)
	}

	t.Cleanup(func() {
		if err := env.Docker.VolumeRemove(context.Background(), name, false); err != nil &&
			!cerrdefs.IsNotFound(err) {
			t.Errorf("cleanup: VolumeRemove %s: %v", name, err)
		}
	})
}

// cleanupNamedConfig registers a cleanup for a config this file creates
// *through Cetacean*, where the test never learns an engine ID up front.
func cleanupNamedConfig(t *testing.T, env *harness.Env, name string) {
	t.Helper()

	t.Cleanup(func() {
		if err := env.Docker.ConfigRemove(context.Background(), name); err != nil &&
			!cerrdefs.IsNotFound(err) {
			t.Errorf("cleanup: ConfigRemove %s: %v", name, err)
		}
	})
}

func cleanupNamedSecret(t *testing.T, env *harness.Env, name string) {
	t.Helper()

	t.Cleanup(func() {
		if err := env.Docker.SecretRemove(context.Background(), name); err != nil &&
			!cerrdefs.IsNotFound(err) {
			t.Errorf("cleanup: SecretRemove %s: %v", name, err)
		}
	})
}

// ─── cache synchronisation ──────────────────────────────────────────────

// awaitCached blocks until Cetacean serves path with 200. Every remove handler
// resolves its target through the cache (lookupOr404), which the watcher fills
// asynchronously, so a resource created directly on the engine is briefly
// invisible and a DELETE in that window answers 404.
func awaitCached(t *testing.T, proc *sut.Process, path string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		resp := sweepRequest(t, proc, http.MethodGet, path, "", nil)
		status := resp.StatusCode
		resp.Body.Close()

		if status == http.StatusOK {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("Cetacean never saw %s (last status %d)", path, status)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// awaitGone blocks until the engine reports name absent, using inspect rather
// than a list so the check names exactly the resource under test.
func awaitGone(t *testing.T, what, name string, inspect func() error) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		err := inspect()
		if err != nil && cerrdefs.IsNotFound(err) {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("the engine still holds %s %s after removal (last error: %v)",
				what, name, err)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// ─── configs ────────────────────────────────────────────────────────────

func driveConfigCreate(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-config-create")
	cleanupNamedConfig(t, env, name)

	payload := []byte("cetacean-e2e-created")

	body, err := json.Marshal(map[string]string{
		"name": name,
		"data": base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := sweepRequest(t, proc, http.MethodPost, "/configs", "application/json", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /configs: status = %d, want 201", resp.StatusCode)
	}

	// RFC 9110 §10.2.2: a 201 names what it created. Without it a client has
	// to guess the resource's URL from a name it happened to choose.
	if resp.Header.Get("Location") == "" {
		t.Error("201 carries no Location header")
	}

	created, _, err := env.Docker.ConfigInspectWithRaw(context.Background(), name)
	if err != nil {
		t.Fatalf("the engine does not hold config %s after a 201: %v", name, err)
	}

	// The payload is the point of a config, and it travels base64-encoded in
	// both directions — a create that stored the encoded form rather than the
	// bytes would still answer 201.
	if string(created.Spec.Data) != string(payload) {
		t.Errorf("engine config data = %q, want %q", created.Spec.Data, payload)
	}
}

func driveConfigRemoval(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-config-remove")
	id := engineConfig(t, env, name, nil)

	awaitCached(t, proc, "/configs/"+id)

	resp := sweepRequest(t, proc, http.MethodDelete, "/configs/"+id, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /configs/%s: status = %d, want 204", id, resp.StatusCode)
	}

	awaitGone(t, "config", name, func() error {
		_, _, err := env.Docker.ConfigInspectWithRaw(context.Background(), id)

		return err
	})
}

func driveConfigLabels(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-config-labels")
	id := engineConfig(t, env, name, map[string]string{"keep": "me"})

	awaitCached(t, proc, "/configs/"+id)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/configs/"+id+"/labels",
		"application/merge-patch+json", []byte(`{"sweep-label":"yes"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH config labels: status = %d, want 200", resp.StatusCode)
	}

	updated, _, err := env.Docker.ConfigInspectWithRaw(context.Background(), id)
	if err != nil {
		t.Fatalf("ConfigInspectWithRaw %s: %v", id, err)
	}

	if updated.Spec.Labels["sweep-label"] != "yes" {
		t.Errorf("engine labels = %v, want sweep-label=yes", updated.Spec.Labels)
	}

	// A merge patch adds; it does not replace the map. A handler that sent
	// the patch as the whole label set would drop this and still answer 200.
	if updated.Spec.Labels["keep"] != "me" {
		t.Errorf("engine labels = %v, want the pre-existing keep=me to survive a merge patch",
			updated.Spec.Labels)
	}
}

// ─── secrets ────────────────────────────────────────────────────────────

func driveSecretCreate(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-secret-create")
	cleanupNamedSecret(t, env, name)

	body, err := json.Marshal(map[string]string{
		"name": name,
		"data": base64.StdEncoding.EncodeToString([]byte("cetacean-e2e-created")),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp := sweepRequest(t, proc, http.MethodPost, "/secrets", "application/json", body)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /secrets: status = %d, want 201", resp.StatusCode)
	}

	if resp.Header.Get("Location") == "" {
		t.Error("201 carries no Location header")
	}

	// Unlike a config, the payload cannot be read back: the engine never
	// returns secret data, which is exactly the property that makes a secret
	// one. Existence under the requested name is what there is to verify.
	created, _, err := env.Docker.SecretInspectWithRaw(context.Background(), name)
	if err != nil {
		t.Fatalf("the engine does not hold secret %s after a 201: %v", name, err)
	}

	if created.Spec.Name != name {
		t.Errorf("engine secret name = %q, want %q", created.Spec.Name, name)
	}

	if len(created.Spec.Data) != 0 {
		t.Errorf(
			"the engine returned %d bytes of secret data on inspect; this test's "+
				"assumption that secret payloads are never readable no longer holds",
			len(created.Spec.Data),
		)
	}
}

func driveSecretRemoval(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-secret-remove")
	id := engineSecret(t, env, name, nil)

	awaitCached(t, proc, "/secrets/"+id)

	resp := sweepRequest(t, proc, http.MethodDelete, "/secrets/"+id, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /secrets/%s: status = %d, want 204", id, resp.StatusCode)
	}

	awaitGone(t, "secret", name, func() error {
		_, _, err := env.Docker.SecretInspectWithRaw(context.Background(), id)

		return err
	})
}

func driveSecretLabels(t *testing.T, env *harness.Env, proc *sut.Process) {
	name := sweepName("sweep-secret-labels")
	id := engineSecret(t, env, name, map[string]string{"keep": "me"})

	awaitCached(t, proc, "/secrets/"+id)

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/secrets/"+id+"/labels",
		"application/merge-patch+json", []byte(`{"sweep-label":"yes"}`),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH secret labels: status = %d, want 200", resp.StatusCode)
	}

	updated, _, err := env.Docker.SecretInspectWithRaw(context.Background(), id)
	if err != nil {
		t.Fatalf("SecretInspectWithRaw %s: %v", id, err)
	}

	if updated.Spec.Labels["sweep-label"] != "yes" {
		t.Errorf("engine labels = %v, want sweep-label=yes", updated.Spec.Labels)
	}

	if updated.Spec.Labels["keep"] != "me" {
		t.Errorf("engine labels = %v, want the pre-existing keep=me to survive a merge patch",
			updated.Spec.Labels)
	}
}

// ─── networks and volumes ───────────────────────────────────────────────

func driveNetworkRemoval(t *testing.T, env *harness.Env, proc *sut.Process) {
	// Attached to nothing: an overlay network with endpoints still on it is
	// refused by the engine, and this case is about removal succeeding, not
	// about the conflict path.
	name := sweepName("sweep-network-remove")
	id := engineNetwork(t, env, name)

	awaitCached(t, proc, "/networks/"+id)

	resp := sweepRequest(t, proc, http.MethodDelete, "/networks/"+id, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /networks/%s: status = %d, want 204", id, resp.StatusCode)
	}

	awaitGone(t, "network", name, func() error {
		_, err := env.Docker.NetworkInspect(
			context.Background(), id, network.InspectOptions{},
		)

		return err
	})
}

func driveVolumeRemoval(t *testing.T, env *harness.Env, proc *sut.Process) {
	// Volumes are keyed by name, not ID — the one resource type whose detail
	// route takes {name}. Addressing it by anything else is how that
	// convention gets broken without anyone noticing.
	name := sweepName("sweep-volume-remove")
	engineVolume(t, env, name)

	awaitCached(t, proc, "/volumes/"+name)

	resp := sweepRequest(t, proc, http.MethodDelete, "/volumes/"+name, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /volumes/%s: status = %d, want 204", name, resp.StatusCode)
	}

	awaitGone(t, "volume", name, func() error {
		_, err := env.Docker.VolumeInspect(context.Background(), name)

		return err
	})
}

// ─── services and stacks ────────────────────────────────────────────────

func driveServiceRemoval(t *testing.T, env *harness.Env, proc *sut.Process) {
	service := deployThrowawayService(t, env, "svcremove")
	id := serviceID(t, proc, service)

	resp := sweepRequest(t, proc, http.MethodDelete, "/services/"+id, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /services/%s: status = %d, want 204", id, resp.StatusCode)
	}

	awaitGone(t, "service", service, func() error {
		_, _, err := env.Docker.ServiceInspectWithRaw(
			context.Background(), id, swarm.ServiceInspectOptions{},
		)

		return err
	})
}

func driveStackRemoval(t *testing.T, env *harness.Env, proc *sut.Process) {
	// A stack is derived from labels rather than being a Docker primitive, so
	// removing one is Cetacean fanning out over the members it believes the
	// stack has. The count it reports and what the engine actually lost are
	// two different claims, and both are checked here.
	stack := fixtures.DeployStack(t, env, "stackremove", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	service := stack + "_app"
	awaitCached(t, proc, "/services/"+serviceID(t, proc, service))

	resp := sweepRequest(t, proc, http.MethodDelete, "/stacks/"+stack, "", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /stacks/%s: status = %d, want 200", stack, resp.StatusCode)
	}

	var removal struct {
		Removed struct {
			Services int `json:"services"`
			Networks int `json:"networks"`
			Configs  int `json:"configs"`
			Secrets  int `json:"secrets"`
		} `json:"removed"`
		Errors []struct {
			Resource string `json:"resource"`
			Name     string `json:"name"`
			Error    string `json:"error"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&removal); err != nil {
		t.Fatalf("decode stack removal response: %v", err)
	}

	if len(removal.Errors) > 0 {
		t.Errorf("stack removal reported errors: %+v", removal.Errors)
	}

	if removal.Removed.Services != 1 {
		t.Errorf("removed.services = %d, want 1", removal.Removed.Services)
	}

	awaitGone(t, "service", service, func() error {
		_, _, err := env.Docker.ServiceInspectWithRaw(
			context.Background(), service, swarm.ServiceInspectOptions{},
		)

		return err
	})
}

// ─── node labels ────────────────────────────────────────────────────────

func driveNodeLabels(t *testing.T, env *harness.Env, proc *sut.Process) {
	id := soleNodeID(t, env)

	label := sweepName("sweep-node-label")

	resp := sweepRequest(
		t, proc, http.MethodPatch, "/nodes/"+id+"/labels",
		"application/merge-patch+json",
		fmt.Appendf(nil, `{%q:"yes"}`, label),
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH node labels: status = %d, want 200", resp.StatusCode)
	}

	node, _, err := env.Docker.NodeInspectWithRaw(context.Background(), id)
	if err != nil {
		t.Fatalf("NodeInspectWithRaw %s: %v", id, err)
	}

	if node.Spec.Labels[label] != "yes" {
		t.Errorf("engine node labels = %v, want %s=yes", node.Spec.Labels, label)
	}

	// The node is shared with every other lane in this binary, so the label
	// is removed again rather than left on the cluster.
	t.Cleanup(func() {
		current, _, err := env.Docker.NodeInspectWithRaw(context.Background(), id)
		if err != nil {
			t.Errorf("cleanup: NodeInspectWithRaw %s: %v", id, err)

			return
		}

		delete(current.Spec.Labels, label)

		if err := env.Docker.NodeUpdate(
			context.Background(), id, current.Version, current.Spec,
		); err != nil {
			t.Errorf("cleanup: NodeUpdate %s: %v", id, err)
		}
	})
}

// soleNodeID returns the single node this environment's swarm has, failing if
// the assumption ever stops holding.
func soleNodeID(t *testing.T, env *harness.Env) string {
	t.Helper()

	nodes, err := env.Docker.NodeList(context.Background(), swarm.NodeListOptions{})
	if err != nil {
		t.Fatalf("NodeList: %v", err)
	}

	if len(nodes) != 1 {
		t.Fatalf("the environment's swarm has %d nodes; this lane assumes one", len(nodes))
	}

	return nodes[0].ID
}
