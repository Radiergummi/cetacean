package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// attachmentTestCache seeds a service plus the secret and config the tests
// repoint it at.
func attachmentTestCache(t *testing.T) *cache.Cache {
	t.Helper()

	c := cache.New(nil)
	c.SetService(swarm.Service{
		ID:   "svc1",
		Meta: swarm.Meta{Version: swarm.Version{Index: 41}},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "web"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "nginx:alpine"},
			},
		},
	})
	c.SetSecret(swarm.Secret{
		ID:   "s2",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db_password_v2"}},
	})
	c.SetConfig(swarm.Config{
		ID:   "c1",
		Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx_conf"}},
	})

	return c
}

// TestUpdateServiceSecretsRepointsAService is the second of the three calls a
// rotation needs, and the one that made create_secret worth adding.
func TestUpdateServiceSecretsRepointsAService(t *testing.T) {
	var got []*swarm.SecretReference

	c := attachmentTestCache(t)
	writeClient := &fakeWriteClient{
		updateServiceSecretsFn: func(
			_ context.Context, _ string, refs []*swarm.SecretReference,
		) (swarm.Service, error) {
			got = refs

			updated, _ := c.GetService("svc1")
			updated.Spec.TaskTemplate.ContainerSpec.Secrets = refs

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	result := callTool(t, handler, `{"name":"update_service_secrets","arguments":`+
		`{"id":"web","secrets":[{"name":"db_password_v2"}]}}`)

	if len(got) != 1 {
		t.Fatalf("refs = %d, want 1", len(got))
	}
	if got[0].SecretName != "db_password_v2" {
		t.Errorf("SecretName = %q", got[0].SecretName)
	}

	// The ID is resolved from the name: a model holds names — every listing
	// and every completion offers names — while Docker wants both and rejects
	// a pair that disagrees.
	if got[0].SecretID != "s2" {
		t.Errorf("SecretID = %q, want the resolved s2", got[0].SecretID)
	}

	// File.Name defaults to /run/secrets/<name>, the path REST's
	// PATCH /services/{id}/secrets defaults to, so a secret attached through
	// either transport lands in the same place.
	if got[0].File == nil || got[0].File.Name != "/run/secrets/db_password_v2" {
		t.Errorf("File = %+v, want Name defaulted to /run/secrets/<name>", got[0].File)
	}

	var compact serviceUpdateResult
	if err := json.Unmarshal(result.StructuredContent, &compact); err != nil {
		t.Fatalf("decode: %v", err)
	}

	names, _ := compact.Details["secretNames"].([]any)
	if len(names) != 1 || names[0] != "db_password_v2" {
		t.Errorf("details = %+v, want the secret it now receives", compact.Details)
	}
}

// An explicit target overrides the default mount name.
func TestUpdateServiceSecretsHonoursAnExplicitTarget(t *testing.T) {
	var got []*swarm.SecretReference

	c := attachmentTestCache(t)
	writeClient := &fakeWriteClient{
		updateServiceSecretsFn: func(
			_ context.Context, _ string, refs []*swarm.SecretReference,
		) (swarm.Service, error) {
			got = refs
			updated, _ := c.GetService("svc1")

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"update_service_secrets","arguments":`+
		`{"id":"web","secrets":[{"name":"db_password_v2","target":"db_password"}]}}`)

	if got[0].File.Name != "db_password" {
		t.Errorf("File.Name = %q, want the caller's target", got[0].File.Name)
	}
}

// A secret that does not exist is refused before the write, naming it —
// Docker's own error for a bad reference is opaque, and the fix (create it
// first, or list what exists) is not discoverable from it.
func TestUpdateServiceSecretsRejectsAnUnknownSecret(t *testing.T) {
	c := attachmentTestCache(t)

	handler := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsConfiguration).Handler()

	raw := callToolExpectingError(t, handler,
		`{"name":"update_service_secrets","arguments":`+
			`{"id":"web","secrets":[{"name":"nosuch"}]}}`)

	if !strings.Contains(raw, "nosuch") {
		t.Errorf("the error does not name the missing secret: %s", raw)
	}
}

// Replacement, not merge: the caller passes the complete list, and an empty
// one detaches everything. Stating it in the description is not enough — the
// behaviour is pinned here, because getting it wrong silently strips a
// container's credentials.
func TestUpdateServiceSecretsReplacesWholesale(t *testing.T) {
	var called bool
	var got []*swarm.SecretReference

	c := attachmentTestCache(t)
	writeClient := &fakeWriteClient{
		updateServiceSecretsFn: func(
			_ context.Context, _ string, refs []*swarm.SecretReference,
		) (swarm.Service, error) {
			called = true
			got = refs
			updated, _ := c.GetService("svc1")

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"update_service_secrets","arguments":`+
		`{"id":"web","secrets":[]}}`)

	if !called {
		t.Fatal("the write was skipped; an empty list must still detach")
	}
	if len(got) != 0 {
		t.Errorf("refs = %v, want none", got)
	}
}

func TestUpdateServiceConfigsRepointsAService(t *testing.T) {
	var got []*swarm.ConfigReference

	c := attachmentTestCache(t)
	writeClient := &fakeWriteClient{
		updateServiceConfigsFn: func(
			_ context.Context, _ string, refs []*swarm.ConfigReference,
		) (swarm.Service, error) {
			got = refs

			updated, _ := c.GetService("svc1")
			updated.Spec.TaskTemplate.ContainerSpec.Configs = refs

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"update_service_configs","arguments":`+
		`{"id":"web","configs":[{"name":"nginx_conf","target":"/etc/nginx/nginx.conf"}]}}`)

	if len(got) != 1 {
		t.Fatalf("refs = %d, want 1", len(got))
	}
	if got[0].ConfigID != "c1" {
		t.Errorf("ConfigID = %q, want the resolved c1", got[0].ConfigID)
	}
	if got[0].File == nil || got[0].File.Name != "/etc/nginx/nginx.conf" {
		t.Errorf("File = %+v, want the caller's target path", got[0].File)
	}
}

// TestUpdateServiceConfigsDefaultsToTheRESTPath pins the half of the mount
// rule that was wrong: a config with no target defaulted to the bare name,
// which Swarm reads as relative to the container's working directory, while
// REST's PATCH /services/{id}/configs defaults to /<name>. The same config
// attached over the two transports mounted at two different paths.
func TestUpdateServiceConfigsDefaultsToTheRESTPath(t *testing.T) {
	var got []*swarm.ConfigReference

	c := attachmentTestCache(t)
	writeClient := &fakeWriteClient{
		updateServiceConfigsFn: func(
			_ context.Context, _ string, refs []*swarm.ConfigReference,
		) (swarm.Service, error) {
			got = refs

			updated, _ := c.GetService("svc1")

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"update_service_configs","arguments":`+
		`{"id":"web","configs":[{"name":"nginx_conf"}]}}`)

	if len(got) != 1 || got[0].File == nil {
		t.Fatalf("refs = %+v, want one with a file target", got)
	}
	if got[0].File.Name != "/nginx_conf" {
		t.Errorf("File.Name = %q, want /<name>", got[0].File.Name)
	}
}

// Both editors sit at the configuration level, matching the REST route for the
// same operation. This is a decision that was deliberately taken against the
// spec, so it is guarded rather than left to drift back.
func TestAttachmentEditorsMatchTheRESTTier(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	found := map[string]bool{}

	for _, def := range srv.toolCatalog() {
		switch def.tool.Name {
		case "update_service_secrets", "update_service_configs":
			found[def.tool.Name] = true

			if def.tier != config.OpsConfiguration {
				t.Errorf(
					"%s tier = %v, want OpsConfiguration to match the REST route for the same operation",
					def.tool.Name,
					def.tier,
				)
			}
		}
	}

	for _, name := range []string{"update_service_secrets", "update_service_configs"} {
		if !found[name] {
			t.Errorf("%s is not registered", name)
		}
	}
}

// update_service_mounts sits at the configuration level, matching the REST
// route, and the tier is therefore *not* where the danger of a host bind is
// communicated. Binding /var/run/docker.sock into a container is a root shell
// on the host and control of the whole cluster; the tool does not refuse it —
// an operator at this level may legitimately want it, and Cetacean does not
// decide for the caller — so the description is the only place a model reads
// what it is about to do.
func TestUpdateServiceMountsMatchesTheRESTTier(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	for _, def := range srv.toolCatalog() {
		if def.tool.Name != "update_service_mounts" {
			continue
		}

		if def.tier != config.OpsConfiguration {
			t.Errorf(
				"tier = %v, want OpsConfiguration to match the REST route for the same operation",
				def.tier,
			)
		}

		description := strings.ToLower(def.tool.Description)
		for _, warning := range []string{"host", "docker socket"} {
			if !strings.Contains(description, warning) {
				t.Errorf(
					"the description does not warn about %q: %s",
					warning,
					def.tool.Description,
				)
			}
		}

		return
	}

	t.Fatal("update_service_mounts is not registered")
}

func TestUpdateServiceMountsReplacesTheSet(t *testing.T) {
	var got []mount.Mount

	c := attachmentTestCache(t)
	writeClient := &fakeWriteClient{
		updateServiceMountsFn: func(
			_ context.Context, _ string, mounts []mount.Mount,
		) (swarm.Service, error) {
			got = mounts

			updated, _ := c.GetService("svc1")
			updated.Spec.TaskTemplate.ContainerSpec.Mounts = mounts

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	result := callTool(t, handler, `{"name":"update_service_mounts","arguments":`+
		`{"id":"web","mounts":[{"type":"volume","source":"data","target":"/var/lib/data"}]}}`)

	if len(got) != 1 {
		t.Fatalf("mounts = %d, want 1", len(got))
	}
	if got[0].Type != mount.TypeVolume || got[0].Source != "data" ||
		got[0].Target != "/var/lib/data" {
		t.Errorf("mount = %+v", got[0])
	}

	// The answer names what is mounted now, so a caller can confirm the
	// replacement without a second read.
	var compact serviceUpdateResult
	if err := json.Unmarshal(result.StructuredContent, &compact); err != nil {
		t.Fatalf("decode: %v", err)
	}

	targets, _ := compact.Details["mountTargets"].([]any)
	if len(targets) != 1 || targets[0] != "/var/lib/data" {
		t.Errorf("details = %+v, want the target it now mounts", compact.Details)
	}
}

// Replacement, not merge — the rule every wholesale section here follows. An
// empty list unmounts everything, which is how a bind is removed.
func TestUpdateServiceMountsReplacesWholesale(t *testing.T) {
	var called bool
	var got []mount.Mount

	c := attachmentTestCache(t)
	writeClient := &fakeWriteClient{
		updateServiceMountsFn: func(
			_ context.Context, _ string, mounts []mount.Mount,
		) (swarm.Service, error) {
			called = true
			got = mounts

			updated, _ := c.GetService("svc1")

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"update_service_mounts","arguments":{"id":"web","mounts":[]}}`)

	if !called {
		t.Fatal("the write was skipped; an empty list must still unmount everything")
	}
	if len(got) != 0 {
		t.Errorf("mounts = %v, want none", got)
	}
}

// An unknown mount type is named rather than passed to Docker, which answers
// with an error that does not say what the three valid types are.
func TestUpdateServiceMountsRejectsAnUnknownType(t *testing.T) {
	c := attachmentTestCache(t)

	handler := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsConfiguration).Handler()

	raw := callToolExpectingError(t, handler,
		`{"name":"update_service_mounts","arguments":`+
			`{"id":"web","mounts":[{"type":"magic","target":"/x"}]}}`)

	if !strings.Contains(raw, "magic") {
		t.Errorf("the error does not name the bad type: %s", raw)
	}
}

// A mount with no target has nowhere to land. Docker rejects it too, but only
// after the rolling deploy has been requested.
func TestUpdateServiceMountsRequiresATarget(t *testing.T) {
	c := attachmentTestCache(t)

	handler := newToolTestServer(t, c, &fakeWriteClient{}, config.OpsConfiguration).Handler()

	callToolExpectingError(t, handler,
		`{"name":"update_service_mounts","arguments":`+
			`{"id":"web","mounts":[{"type":"volume","source":"data"}]}}`)
}
