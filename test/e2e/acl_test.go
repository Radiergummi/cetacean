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

const readOnlyPolicy = `grants:
  - resources: ["*"]
    audience: ["group:viewers"]
    permissions: ["read"]
`

const writePolicy = `grants:
  - resources: ["*"]
    audience: ["group:viewers"]
    permissions: ["read", "write"]
`

// A grant listing only "read" must gate writes; adding "write" to the same
// audience, without a restart, must lift the gate. This is fsnotify's only
// honest test: a unit test can prove the parser works, not that the watcher
// is wired to the evaluator that answers real requests.
//
// The Allow header is asserted on a DETAIL endpoint (not a list endpoint).
// /services always answers "GET, HEAD" regardless of ACL or operations
// level -- "service" is absent from listCreateMethods in
// internal/api/allow.go, so POST never appears there. A detail endpoint's
// PUT/PATCH availability does depend on write permission, so it is the only
// place this behavior is observable.
func TestACLPolicyHotReload(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(readOnlyPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	proc := sut.Start(t, sut.Config{
		Port:       19005,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_ACL_POLICY_FILE":      policy,
			"CETACEAN_OPERATIONS_LEVEL":     "2",
		},
	})

	id := viewerServiceID(t, proc, "shop_web")

	allowBefore := viewerAllowHeader(t, proc, "/services/"+id)
	if !strings.Contains(allowBefore, "GET") {
		t.Fatalf("Allow = %q, want it to contain GET", allowBefore)
	}

	if strings.Contains(allowBefore, "PUT") {
		t.Fatalf("Allow = %q under a read-only policy, want no PUT", allowBefore)
	}

	if err := os.WriteFile(policy, []byte(writePolicy), 0o644); err != nil {
		t.Fatalf("rewrite policy: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		allowAfter := viewerAllowHeader(t, proc, "/services/"+id)
		if strings.Contains(allowAfter, "PUT") && strings.Contains(allowAfter, "GET") {
			return
		}

		time.Sleep(250 * time.Millisecond)
	}

	t.Errorf("policy change did not take effect within 10s; Allow is still %q",
		viewerAllowHeader(t, proc, "/services/"+id))
}

// A grant limited to one stack must hide the other stack's services from the
// listing, not merely refuse writes on them -- and it must still show the
// services it does grant, so a filter that returns nothing at all cannot
// pass by accident.
func TestACLFiltersListings(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	scoped := `grants:
  - resources: ["stack:` + fixtures.StackShop + `"]
    audience: ["group:shoponly"]
    permissions: ["read"]
`

	if err := os.WriteFile(policy, []byte(scoped), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	proc := sut.Start(t, sut.Config{
		Port:       19005,
		DockerHost: env.DockerHost,
		Env: map[string]string{
			"CETACEAN_AUTH_MODE":            "headers",
			"CETACEAN_AUTH_HEADERS_SUBJECT": "X-Auth-User",
			"CETACEAN_AUTH_HEADERS_GROUPS":  "X-Auth-Groups",
			"CETACEAN_TRUSTED_PROXIES":      "127.0.0.1/32",
			"CETACEAN_ACL_POLICY_FILE":      policy,
		},
	})

	var body struct {
		Items []struct {
			Spec struct {
				Name   string            `json:"Name"`
				Labels map[string]string `json:"Labels"`
			} `json:"Spec"`
		} `json:"items"`
	}

	//nolint:bodyclose // closed in getJSONAs
	resp := getJSONAs(
		t,
		proc,
		"/services",
		"shop@example.com",
		"shoponly",
		&body,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /services: status = %d, want 200", resp.StatusCode)
	}

	if len(body.Items) == 0 {
		t.Fatal("no services visible; the shop grant should expose some")
	}

	sawShopWeb := false
	for _, item := range body.Items {
		if ns := item.Spec.Labels["com.docker.stack.namespace"]; ns != fixtures.StackShop {
			t.Errorf(
				"service %q from stack %q is visible under a shop-only grant",
				item.Spec.Name,
				ns,
			)
		}

		if item.Spec.Name == "shop_web" {
			sawShopWeb = true
		}
	}

	// The two-sided half of the assertion: an empty or wrongly-scoped filter
	// could still pass the loop above if it happened to admit zero items, or
	// items outside "shop" that don't collide with a real stack name. Naming
	// a known shop-stack service confirms the grant is doing more than
	// denying by default.
	if !sawShopWeb {
		t.Error("shop_web missing from a shop-only grant's listing")
	}
}

// getJSONAs performs a GET as a headers-auth identity, decoding the JSON body
// into into when the response is 200. It mirrors getJSON (api_test.go), which
// cannot be reused here because it never sets the auth headers headers mode
// requires.
func getJSONAs(
	t *testing.T,
	proc *sut.Process,
	path, user, groups string,
	into any,
) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-User", user)
	req.Header.Set("X-Auth-Groups", groups)

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	if into != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}

	return resp
}

// viewerServiceID resolves a service's ID by name, authenticated as the
// "viewers" group used throughout TestACLPolicyHotReload.
func viewerServiceID(t *testing.T, proc *sut.Process, name string) string {
	t.Helper()

	var body struct {
		Items []struct {
			ID   string `json:"ID"`
			Spec struct {
				Name string `json:"Name"`
			} `json:"Spec"`
		} `json:"items"`
	}

	//nolint:bodyclose // closed in getJSONAs
	resp := getJSONAs(
		t,
		proc,
		"/services",
		"viewer@example.com",
		"viewers",
		&body,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /services: status = %d, want 200", resp.StatusCode)
	}

	for _, item := range body.Items {
		if item.Spec.Name == name {
			return item.ID
		}
	}

	t.Fatalf("service %q not found in /services", name)

	return ""
}

// viewerAllowHeader reads the Allow header off a detail endpoint,
// authenticated as the "viewers" group.
func viewerAllowHeader(t *testing.T, proc *sut.Process, path string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Auth-User", "viewer@example.com")
	req.Header.Set("X-Auth-Groups", "viewers")

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	return resp.Header.Get("Allow")
}
