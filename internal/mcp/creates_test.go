package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/config"
)

// TestCreateSecretReturnsItsIdentity covers the first of the three calls a
// rotation needs. Swarm secrets are immutable, so "update the secret of foo"
// is create-new, repoint the services, drop the old — and Cetacean could
// previously only do the third step, which made the whole sequence impossible.
func TestCreateSecretReturnsItsIdentity(t *testing.T) {
	writeClient := &fakeWriteClient{
		createSecretFn: func(_ context.Context, spec swarm.SecretSpec) (string, error) {
			return "newsecretid", nil
		},
	}

	handler := newToolTestServer(t, cache.New(nil), writeClient, config.OpsConfiguration).Handler()

	result := callTool(t, handler,
		`{"name":"create_secret","arguments":{"name":"db_password_v2","data":"hunter2"}}`)

	var got createResult
	if err := json.Unmarshal(result.StructuredContent, &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, result.StructuredContent)
	}

	if got.ID != "newsecretid" {
		t.Errorf("id = %q, want newsecretid — the caller needs it to reference the secret", got.ID)
	}
	if got.Name != "db_password_v2" {
		t.Errorf("name = %q, want db_password_v2", got.Name)
	}
	if got.Type != "secret" {
		t.Errorf("type = %q, want secret", got.Type)
	}
}

// The payload must reach Docker as bytes. Getting the direction wrong writes a
// secret whose value is the *encoding* of the value, and nothing detects that
// until something fails to authenticate at runtime.
func TestCreateSecretDecodesBase64Data(t *testing.T) {
	var spec swarm.SecretSpec

	writeClient := &fakeWriteClient{
		createSecretFn: func(_ context.Context, s swarm.SecretSpec) (string, error) {
			spec = s

			return "id1", nil
		},
	}

	handler := newToolTestServer(t, cache.New(nil), writeClient, config.OpsConfiguration).Handler()

	// "hunter2", base64-encoded.
	callTool(t, handler, `{"name":"create_secret","arguments":`+
		`{"name":"db_password_v2","data":"aHVudGVyMg==","encoding":"base64"}}`)

	if string(spec.Data) != "hunter2" {
		t.Errorf("data = %q, want the decoded payload", spec.Data)
	}
}

// Without an explicit encoding the payload is taken literally, so a value that
// merely looks like base64 is not silently decoded into something else.
func TestCreateSecretTakesDataLiterallyByDefault(t *testing.T) {
	var spec swarm.SecretSpec

	writeClient := &fakeWriteClient{
		createSecretFn: func(_ context.Context, s swarm.SecretSpec) (string, error) {
			spec = s

			return "id1", nil
		},
	}

	handler := newToolTestServer(t, cache.New(nil), writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler, `{"name":"create_secret","arguments":`+
		`{"name":"looks_encoded","data":"aHVudGVyMg=="}}`)

	if string(spec.Data) != "aHVudGVyMg==" {
		t.Errorf("data = %q, want the literal string with no encoding declared", spec.Data)
	}
}

// A secret's value must never come back out, on any path.
func TestCreateSecretNeverEchoesTheData(t *testing.T) {
	writeClient := &fakeWriteClient{
		createSecretFn: func(_ context.Context, _ swarm.SecretSpec) (string, error) {
			return "id1", nil
		},
	}

	handler := newToolTestServer(t, cache.New(nil), writeClient, config.OpsConfiguration).Handler()

	result := callTool(t, handler,
		`{"name":"create_secret","arguments":{"name":"db_password_v2","data":"hunter2"}}`)

	body, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(body), "hunter2") {
		t.Errorf("the reply echoes the secret payload: %s", body)
	}
}

// Base64 that will not decode must be refused before the write, naming the
// encoding the caller declared — otherwise the secret is written as garbage.
func TestCreateSecretRejectsMalformedBase64(t *testing.T) {
	handler := newToolTestServer(
		t,
		cache.New(nil),
		&fakeWriteClient{},
		config.OpsConfiguration,
	).Handler()

	callToolExpectingError(t, handler,
		`{"name":"create_secret","arguments":`+
			`{"name":"x","data":"not!valid!base64","encoding":"base64"}}`)
}

func TestCreateConfigReturnsItsIdentity(t *testing.T) {
	var spec swarm.ConfigSpec

	writeClient := &fakeWriteClient{
		createConfigFn: func(_ context.Context, s swarm.ConfigSpec) (string, error) {
			spec = s

			return "newconfigid", nil
		},
	}

	handler := newToolTestServer(t, cache.New(nil), writeClient, config.OpsConfiguration).Handler()

	result := callTool(t, handler,
		`{"name":"create_config","arguments":{"name":"nginx_conf","data":"server {}",`+
			`"labels":{"com.docker.stack.namespace":"demo"}}}`)

	var got createResult
	if err := json.Unmarshal(result.StructuredContent, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Type != "config" {
		t.Errorf("type = %q, want config", got.Type)
	}
	if string(spec.Data) != "server {}" {
		t.Errorf("data = %q", spec.Data)
	}
	if spec.Labels["com.docker.stack.namespace"] != "demo" {
		t.Errorf("labels = %v, want the stack label to survive", spec.Labels)
	}
}

// Both creates sit at the configuration level, the same one the REST routes
// for these operations require — the operations level must mean one thing
// whichever transport an operator reaches for.
func TestCreateToolsSitAtTheConfigurationTier(t *testing.T) {
	srv := newResourceTestServer(t, cache.New(nil))

	found := map[string]bool{}

	for _, def := range srv.toolCatalog() {
		switch def.tool.Name {
		case "create_secret", "create_config":
			found[def.tool.Name] = true

			if def.tier != config.OpsConfiguration {
				t.Errorf(
					"%s tier = %v, want OpsConfiguration to match the REST route",
					def.tool.Name, def.tier,
				)
			}
		}
	}

	for _, name := range []string{"create_secret", "create_config"} {
		if !found[name] {
			t.Errorf("%s is not registered", name)
		}
	}
}

// TestCreatedSecretIsImmediatelyUsable is the sequence both tools' descriptions
// tell a caller to follow: create the replacement, then repoint the service at
// it. It failed against a live cluster — create_secret returned an ID and the
// very next call answered "no such secret" — because resolution reads the
// cache and the watcher fills that asynchronously, some hundreds of
// milliseconds later.
//
// A caller doing exactly what the tool says must not have to sleep and retry,
// so a create seeds the cache with what it just made.
func TestCreatedSecretIsImmediatelyUsable(t *testing.T) {
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

	writeClient := &fakeWriteClient{
		createSecretFn: func(_ context.Context, _ swarm.SecretSpec) (string, error) {
			return "freshid", nil
		},
		updateServiceSecretsFn: func(
			_ context.Context, _ string, refs []*swarm.SecretReference,
		) (swarm.Service, error) {
			updated, _ := c.GetService("svc1")
			updated.Spec.TaskTemplate.ContainerSpec.Secrets = refs

			return updated, nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler,
		`{"name":"create_secret","arguments":{"name":"rot_v1","data":"pw"}}`)

	// No sync, no sleep — the next call is the one an agent would make.
	callTool(t, handler, `{"name":"update_service_secrets","arguments":`+
		`{"id":"web","secrets":[{"name":"rot_v1"}]}}`)
}

// The seeded record must never carry the payload: the cache is read by every
// listing, and a secret's value has no business being in it.
func TestSeededSecretCarriesNoPayload(t *testing.T) {
	c := cache.New(nil)

	writeClient := &fakeWriteClient{
		createSecretFn: func(_ context.Context, _ swarm.SecretSpec) (string, error) {
			return "freshid", nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler,
		`{"name":"create_secret","arguments":{"name":"rot_v1","data":"hunter2"}}`)

	sec, ok := c.GetSecret("freshid")
	if !ok {
		t.Fatal("the created secret did not reach the cache")
	}
	if len(sec.Spec.Data) != 0 {
		t.Errorf("the cached secret carries its payload: %q", sec.Spec.Data)
	}
	if sec.Spec.Name != "rot_v1" {
		t.Errorf("cached name = %q, want rot_v1", sec.Spec.Name)
	}
}

// The same for configs, whose content is readable but still need not be
// duplicated into the cache by a create — the watcher will bring it.
func TestCreatedConfigIsImmediatelyResolvable(t *testing.T) {
	c := cache.New(nil)

	writeClient := &fakeWriteClient{
		createConfigFn: func(_ context.Context, _ swarm.ConfigSpec) (string, error) {
			return "cfgid", nil
		},
	}

	handler := newToolTestServer(t, c, writeClient, config.OpsConfiguration).Handler()

	callTool(t, handler,
		`{"name":"create_config","arguments":{"name":"nginx_conf","data":"server {}"}}`)

	if _, found, err := c.ResolveConfig("nginx_conf"); err != nil || !found {
		t.Errorf(
			"ResolveConfig(nginx_conf) found=%v err=%v, want it resolvable at once",
			found,
			err,
		)
	}
}
