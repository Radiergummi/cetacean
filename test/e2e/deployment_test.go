//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives four things a real deployment changes that no other lane
// varies: serving under a base path, terminating TLS in the binary, the MCP
// Origin guard, and persisting the cache to disk. Every other lane runs at
// the root of a plain HTTP listener with snapshots off. Reserves port 19018
// (see README.md's reserved-ports table).

const deploymentPort = 19018

// deploymentBasePath is the prefix a reverse proxy would mount the dashboard
// under. It is deliberately two segments deep: a single-segment base collides
// with the router's own top-level paths, so a middleware that stripped the
// wrong amount could still appear to work.
const deploymentBasePath = "/ops/cetacean"

// ─── base path ──────────────────────────────────────────────────────────

// TestBasePathMovesTheWholeSurface drives CETACEAN_BASE_PATH end to end. The
// setting is one line of configuration and it moves every URL the server
// answers on, every URL it emits, and the document the SPA serves — so it is
// exactly the kind of change that half-works.
func TestBasePathMovesTheWholeSurface(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       deploymentPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": "0",
			"CETACEAN_BASE_PATH":        deploymentBasePath,
		},
	})

	stack := fixtures.DeployStack(t, env, "basepath", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})

	t.Run("the unprefixed path is gone", func(t *testing.T) {
		// Not merely unhandled: a 200 here would mean the dashboard is served
		// twice, and a proxy stripping the prefix would reach a second copy of
		// the surface that emits the wrong links.
		out := precondRequest(t, proc, http.MethodGet, "/services",
			map[string]string{"Accept": "application/json"}, "", "")

		if out.status != http.StatusNotFound {
			t.Errorf("GET /services without the base path: status = %d, want 404", out.status)
		}
	})

	t.Run("the prefixed path serves the resource", func(t *testing.T) {
		out := precondRequest(t, proc, http.MethodGet, deploymentBasePath+"/services",
			map[string]string{"Accept": "application/json"}, "", "")

		if out.status != http.StatusOK {
			t.Fatalf("status = %d (body: %.200s)", out.status, out.body)
		}

		var body struct {
			Context string `json:"@context"`
			Items   []struct {
				AtID string `json:"@id"`
			} `json:"items"`
		}

		if err := json.Unmarshal([]byte(out.body), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}

		// A collection carries no @id of its own — CollectionResponse has no
		// such field — but its @context is a URL the client fetches, and it
		// is prefixed correctly. That is what makes the items beside it a
		// contradiction rather than a convention.
		assertPrefixed(t, "@context", body.Context)

		if len(body.Items) == 0 {
			t.Fatal("the listing is empty, so no item identifier was checked")
		}

		// RFC 8288, and the header this very response also carries: the
		// Link-Template names the same shape of URL and is prefixed.
		for _, template := range out.header.Values("Link-Template") {
			assertPrefixed(t, "Link-Template", linkTarget(template))
		}

		// RFC 8631 self-discovery: the links a client follows to find the
		// spec and the context document move with everything else.
		for _, link := range out.header.Values("Link") {
			for part := range strings.SplitSeq(link, ",") {
				href := linkTarget(part)
				if href == "" || !strings.HasPrefix(href, "/") {
					continue
				}

				assertPrefixed(t, "Link "+part, href)
			}
		}

		// Every identifier the server emits has to be reachable as written,
		// which is what makes the prefixed @context and Link-Template above
		// more than decoration: a client following @id must not leave the
		// deployment.
		for _, item := range body.Items {
			assertPrefixed(t, "item @id", item.AtID)

			served := precondRequest(t, proc, http.MethodGet, item.AtID,
				map[string]string{"Accept": "application/json"}, "", "")
			if served.status != http.StatusOK {
				t.Errorf(
					"item @id %q answered %d rather than serving the resource",
					item.AtID, served.status,
				)
			}
		}
	})

	t.Run("a task names its parents reachably", func(t *testing.T) {
		// The same rule in a second shape: a task detail cross-references its
		// service and node by @id, beside the document's own prefixed @id.
		list := precondRequest(t, proc, http.MethodGet, deploymentBasePath+"/tasks",
			map[string]string{"Accept": "application/json"}, "", "")

		if list.status != http.StatusOK {
			t.Fatalf("GET /tasks: status = %d", list.status)
		}

		var listing struct {
			Items []struct {
				ID string `json:"ID"`
			} `json:"items"`
		}

		if err := json.Unmarshal([]byte(list.body), &listing); err != nil {
			t.Fatalf("decode /tasks: %v", err)
		}

		if len(listing.Items) == 0 {
			t.Skip("no tasks to address")
		}

		detail := precondRequest(t, proc, http.MethodGet,
			deploymentBasePath+"/tasks/"+listing.Items[0].ID,
			map[string]string{"Accept": "application/json"}, "", "")

		if detail.status != http.StatusOK {
			t.Fatalf("GET a task: status = %d (body: %.200s)", detail.status, detail.body)
		}

		var task struct {
			AtID    string `json:"@id"`
			Service struct {
				AtID string `json:"@id"`
			} `json:"service"`
			Node struct {
				AtID string `json:"@id"`
			} `json:"node"`
		}

		if err := json.Unmarshal([]byte(detail.body), &task); err != nil {
			t.Fatalf("decode the task: %v", err)
		}

		// The document's own identity is built by NewDetailResponse and is
		// prefixed; the two cross-references beside it are not, which is the
		// same contradiction inside a smaller document.
		assertPrefixed(t, "task @id", task.AtID)

		for what, ref := range map[string]string{
			"service cross-reference": task.Service.AtID,
			"node cross-reference":    task.Node.AtID,
		} {
			assertPrefixed(t, what, ref)

			served := precondRequest(t, proc, http.MethodGet, ref,
				map[string]string{"Accept": "application/json"}, "", "")
			if served.status != http.StatusOK {
				t.Errorf(
					"the %s %q answered %d rather than serving the resource",
					what, ref, served.status,
				)
			}
		}
	})

	t.Run("the SPA is told where it lives", func(t *testing.T) {
		out := precondRequest(t, proc, http.MethodGet, deploymentBasePath+"/services",
			map[string]string{"Accept": "text/html"}, "", "")

		if out.status != http.StatusOK {
			t.Fatalf("status = %d", out.status)
		}

		// Without this the browser resolves every asset against the origin
		// root and the dashboard loads nothing.
		want := `<base href="` + deploymentBasePath + `/">`
		if !strings.Contains(out.body, want) {
			t.Errorf("the served document does not carry %s", want)
		}
	})

	t.Run("a trailing slash redirects within the base path", func(t *testing.T) {
		out := precondRequest(t, proc, http.MethodGet, deploymentBasePath+"/services/",
			map[string]string{"Accept": "application/json"}, "", "")

		if out.status != http.StatusMovedPermanently {
			t.Fatalf("status = %d, want 301", out.status)
		}

		// A redirect that drops the prefix bounces the client out of the
		// deployment, which is the failure this case exists for.
		assertPrefixed(t, "Location", out.header.Get("Location"))
	})

	t.Run("a name redirect keeps the base path", func(t *testing.T) {
		// internal/api/canonical.go answers a name-addressed path with a 307
		// to the ID-addressed one. That Location is built by the server, so it
		// is a second place the prefix has to be applied.
		out := precondRequest(t, proc, http.MethodGet,
			deploymentBasePath+"/services/"+stack+"_app",
			map[string]string{"Accept": "application/json"}, "", "")

		if out.status != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307 (body: %.200s)", out.status, out.body)
		}

		assertPrefixed(t, "Location", out.header.Get("Location"))

		// And the target it names has to actually serve the resource.
		followed := precondRequest(t, proc, http.MethodGet, out.header.Get("Location"),
			map[string]string{"Accept": "application/json"}, "", "")

		if followed.status != http.StatusOK {
			t.Errorf("following the redirect: status = %d", followed.status)
		}
	})

	t.Run("the meta endpoints move too", func(t *testing.T) {
		// /-/health is what an orchestrator probes. If it stayed at the root
		// while everything else moved, a deployment would look healthy from
		// outside the proxy and be unreachable through it.
		out := precondRequest(t, proc, http.MethodGet, deploymentBasePath+"/-/health", nil, "", "")
		if out.status != http.StatusOK {
			t.Errorf("GET %s/-/health: status = %d, want 200", deploymentBasePath, out.status)
		}
	})
}

// assertPrefixed requires a server-emitted path to carry the base path.
func assertPrefixed(t *testing.T, what, value string) {
	t.Helper()

	if value == "" {
		t.Errorf("%s is empty", what)

		return
	}

	if !strings.HasPrefix(value, deploymentBasePath+"/") {
		t.Errorf("%s = %q, which does not live under %s", what, value, deploymentBasePath)
	}
}

// linkTarget extracts the URI-reference from one RFC 8288 link value.
func linkTarget(value string) string {
	start := strings.Index(value, "<")
	end := strings.Index(value, ">")

	if start < 0 || end < start {
		return ""
	}

	return value[start+1 : end]
}

// ─── TLS termination ────────────────────────────────────────────────────

// TestBinaryTerminatesTLS drives CETACEAN_TLS_CERT/KEY on their own. The cert
// lane already terminates TLS, but only as mTLS in cert auth mode, where a
// failure could as easily be the client certificate's. This is the ordinary
// shape: HTTPS in front of an otherwise unauthenticated deployment.
func TestBinaryTerminatesTLS(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	proc := sut.Start(t, sut.Config{
		Port:       deploymentPort,
		DockerHost: env.DockerHost,
		TLS:        true,
		CACert:     filepath.Join(env.CertDir, "ca.pem"),
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",
			"CETACEAN_TLS_CERT":  filepath.Join(env.CertDir, "server.pem"),
			"CETACEAN_TLS_KEY":   filepath.Join(env.CertDir, "server-key.pem"),
		},
	})

	if !strings.HasPrefix(proc.BaseURL, "https://") {
		t.Fatalf("the SUT is addressed as %q, so this case would prove nothing", proc.BaseURL)
	}

	out := precondRequest(t, proc, http.MethodGet, "/services",
		map[string]string{"Accept": "application/json"}, "", "")

	if out.status != http.StatusOK {
		t.Fatalf("GET /services over TLS: status = %d (body: %.200s)", out.status, out.body)
	}

	// No client certificate was presented and none is configured, so the
	// connection must not be asking for one — a listener demanding mTLS here
	// would reject every ordinary browser.
	var identity struct {
		Provider string `json:"provider"`
	}

	whoami := precondRequest(t, proc, http.MethodGet, "/auth/whoami",
		map[string]string{"Accept": "application/json"}, "", "")

	if err := json.Unmarshal([]byte(whoami.body), &identity); err != nil {
		t.Fatalf("decode /auth/whoami: %v (%.200s)", err, whoami.body)
	}

	if identity.Provider != "none" {
		t.Errorf("provider = %q, want none: TLS must not change who the caller is",
			identity.Provider)
	}
}

// ─── the MCP Origin guard ───────────────────────────────────────────────

// originGuardWarning is the startup warning main.go emits when the guard is
// active with nothing allowed, so every browser MCP client is refused and
// nothing else in the response would say why.
const originGuardWarning = "MCP Origin guard active with no allowlist"

// TestMCPOriginGuardRefusesAForgedOrigin drives the DNS-rebinding defence the
// Streamable HTTP transport requires and mcp-go does not implement. A browser
// on any page can POST to a localhost MCP endpoint; the Origin header is the
// only thing distinguishing that from the client the operator installed.
func TestMCPOriginGuardRefusesAForgedOrigin(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	t.Run("with no allowlist", func(t *testing.T) {
		proc := sut.Start(t, sut.Config{
			Port:       deploymentPort,
			DockerHost: env.DockerHost,
			Env: map[string]string{
				"CETACEAN_AUTH_MODE":        "none",
				"CETACEAN_OPERATIONS_LEVEL": "0",
				"CETACEAN_MCP":              "true",
			},
		})

		// The operator is warned, because with no allowlist every browser
		// client is refused and nothing else would say why.
		if !strings.Contains(proc.Logs(), originGuardWarning) {
			t.Error("the binary started with no allowlist and did not warn")
		}

		if status := mcpOriginStatus(t, proc, "https://evil.example.com"); status !=
			http.StatusForbidden {
			t.Errorf("a forged Origin: status = %d, want 403", status)
		}

		// A non-browser client sends no Origin at all and must be unaffected:
		// the guard is a browser defence, not an authentication check.
		if status := mcpOriginStatus(t, proc, ""); status == http.StatusForbidden {
			t.Errorf("a request with no Origin was refused 403")
		}
	})

	t.Run("with an allowlist", func(t *testing.T) {
		allowed := "https://inspector.example.com"

		proc := sut.Start(t, sut.Config{
			Port:       deploymentPort,
			DockerHost: env.DockerHost,
			Env: map[string]string{
				"CETACEAN_AUTH_MODE":        "none",
				"CETACEAN_OPERATIONS_LEVEL": "0",
				"CETACEAN_MCP":              "true",
				"CETACEAN_CORS_ORIGINS":     allowed,
			},
		})

		if strings.Contains(proc.Logs(), originGuardWarning) {
			t.Error("the binary warned about a missing allowlist though one was configured")
		}

		if status := mcpOriginStatus(t, proc, allowed); status == http.StatusForbidden {
			t.Errorf("the allowed Origin was refused 403")
		}

		// An allowlist is an exact match, not a prefix: a domain that merely
		// starts with an allowed one is a different origin.
		for _, forged := range []string{
			"https://inspector.example.com.evil.test",
			"https://evil.test",
			"http://inspector.example.com",
		} {
			if status := mcpOriginStatus(t, proc, forged); status != http.StatusForbidden {
				t.Errorf("Origin %q: status = %d, want 403", forged, status)
			}
		}
	})
}

// mcpOriginStatus POSTs a tools/list to /mcp with the given Origin (none when
// empty) and returns the status.
func mcpOriginStatus(t *testing.T, proc *sut.Process, origin string) int {
	t.Helper()

	payload := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientCapabilities":{}}}}`

	headers := map[string]string{
		"Accept":               "application/json, text/event-stream",
		"Mcp-Protocol-Version": "2026-07-28",
		"Mcp-Method":           "tools/list",
	}

	if origin != "" {
		headers["Origin"] = origin
	}

	return precondRequest(
		t, proc, http.MethodPost, "/mcp", headers, "application/json", payload,
	).status
}

// ─── snapshot persistence ───────────────────────────────────────────────

// TestSnapshotSurvivesARestart drives CETACEAN_SNAPSHOT, which sut.Config
// defaults to off. The snapshot covers the window before the first full sync
// completes: a restarted binary serves the cluster it last saw, not nothing.
func TestSnapshotSurvivesARestart(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	dataDir := t.TempDir()

	stack := fixtures.DeployStack(t, env, "snapshot", []fixtures.ServiceSpec{
		{Name: "app", Replicas: 1, Command: []string{"sleep infinity"}},
	})
	service := stack + "_app"

	first := startSnapshotSUT(t, env, dataDir)

	// The watcher writes the snapshot once the first full sync completes, so
	// the file cannot be there before the service is.
	awaitCached(t, first, "/services/"+serviceID(t, first, service))

	snapshot := filepath.Join(dataDir, "snapshot.json")
	awaitFile(t, snapshot)

	// The saved state has to describe the cluster, not merely exist.
	var saved struct {
		Version  int `json:"version"`
		Services []struct {
			Spec struct {
				Name string `json:"Name"`
			} `json:"Spec"`
		} `json:"services"`
	}

	raw, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatalf("read %s: %v", snapshot, err)
	}

	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("decode the snapshot: %v", err)
	}

	if saved.Version < 1 {
		t.Errorf("the snapshot carries version %d, which LoadFromDisk refuses", saved.Version)
	}

	if !containsServiceNamed(saved.Services, service) {
		t.Fatalf("the snapshot does not hold %s, so a restore would prove nothing", service)
	}

	// Stopped rather than left running: the second SUT reuses the port, and
	// sut.Start's waitPortFree would otherwise sit until it timed out.
	first.Stop()

	second := startSnapshotSUT(t, env, dataDir)

	if logs := second.Logs(); !strings.Contains(logs, "loaded snapshot from disk") {
		t.Errorf("the restarted binary did not report loading a snapshot")
	}

	// The restored cache must serve the service, which is the whole point:
	// the watcher's own sync would reach the same answer eventually, so this
	// is only meaningful because the log line above says where it came from.
	out := precondRequest(t, second, http.MethodGet, "/services/"+serviceID(t, second, service),
		map[string]string{"Accept": "application/json"}, "", "")

	if out.status != http.StatusOK {
		t.Errorf("the restarted binary does not serve %s: status = %d", service, out.status)
	}
}

// TestSnapshotIsNotWrittenWhenDisabled is the other half, and the one that
// keeps the harness default honest: every other lane relies on starting from
// an empty cache, which only holds while the setting genuinely suppresses the
// write.
func TestSnapshotIsNotWrittenWhenDisabled(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	dataDir := t.TempDir()

	proc := sut.Start(t, sut.Config{
		Port:       deploymentPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE": "none",

			// Only the data dir is named; CETACEAN_SNAPSHOT keeps sut's own
			// default of false.
			"CETACEAN_DATA_DIR": dataDir,
		},
	})

	// Wait for the sync that would have written it.
	awaitReady(t, proc)

	if _, err := os.Stat(filepath.Join(dataDir, "snapshot.json")); err == nil {
		t.Error("a snapshot was written with persistence disabled")
	} else if !os.IsNotExist(err) {
		t.Errorf("stat the snapshot: %v", err)
	}
}

func startSnapshotSUT(t *testing.T, env *harness.Env, dataDir string) *sut.Process {
	t.Helper()

	return sut.Start(t, sut.Config{
		Port:       deploymentPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":        "none",
			"CETACEAN_OPERATIONS_LEVEL": "0",
			"CETACEAN_SNAPSHOT":         "true",
			"CETACEAN_DATA_DIR":         dataDir,
		},
	})
}

func containsServiceNamed(services []struct {
	Spec struct {
		Name string `json:"Name"`
	} `json:"Spec"`
}, name string,
) bool {
	for _, svc := range services {
		if svc.Spec.Name == name {
			return true
		}
	}

	return false
}

// awaitFile blocks until path exists and is non-empty.
func awaitFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		info, err := os.Stat(path)
		if err == nil && info.Size() > 0 {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%s was never written (last error: %v)", path, err)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// awaitReady blocks until the binary reports its first full sync complete.
func awaitReady(t *testing.T, proc *sut.Process) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		out := precondRequest(t, proc, http.MethodGet, "/-/ready", nil, "", "")
		if out.status == http.StatusOK {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("the binary never became ready (last status %d)", out.status)
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// TestACLPolicyWithoutAuthIsCalledOut pins a configuration that looks
// deliberate and enforces nothing: auth mode "none" resolves every caller to
// the same anonymous identity, so the grants are ignored outright.
func TestACLPolicyWithoutAuthIsCalledOut(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)

	policy := filepath.Join(t.TempDir(), "policy.json")

	if err := os.WriteFile(policy, []byte(`{
		"grants": [
			{"resources": ["service:*"], "audience": ["user:*"], "permissions": ["read"]}
		]
	}`), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	proc := sut.Start(t, sut.Config{
		Port:       deploymentPort,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":       "none",
			"CETACEAN_ACL_POLICY_FILE": policy,
		},
	})

	if !strings.Contains(proc.Logs(), "no grant will be enforced") {
		t.Errorf(
			"a policy configured under auth mode none started without a word about it\nlogs:\n%s",
			proc.Logs(),
		)
	}
}
