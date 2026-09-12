//go:build e2e

package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
	"github.com/radiergummi/cetacean/test/e2e/sut"
)

// This file drives every GET/HEAD route contract.Routes() reports against a real
// headers-auth SUT with a file ACL policy, asserting each route × persona is
// served, empty-filtered or refused as the policy implies. drivenReadRoutes and
// excusedReadRoutes must between them name every such route. Port 19007.

const readSweepPort = 19007

// readPersona is one identity this file drives requests as. The four named
// personas mirror compose.dev-auth.yaml's ACL demo exactly; anonymous is a
// fifth, a subject in no group at all, for the default-deny case.
type readPersona struct {
	name   string
	user   string
	groups string // empty for anonymous
}

var readPersonas = []readPersona{
	{name: "ops", user: "ops@example.com", groups: "ops"},
	{name: "viewers", user: "viewers@example.com", groups: "viewers"},
	{name: "frontend", user: "frontend@example.com", groups: "frontend"},
	{name: "oncall", user: "oncall@example.com", groups: "oncall"},
	{name: "anonymous", user: "nobody@example.com", groups: ""},
}

// None of the four named personas is read-scoped to a subset of the fixture:
// ops, viewers and frontend read everything, and oncall reads every type it
// holds any grant on. The fixture stacks are never named "frontend-*", so
// frontend's write grant never applies here.

// readSweepPolicy is compose.dev-auth.yaml's acl-policy config block, copied
// verbatim rather than declared a second time, so this lane's four personas
// are guaranteed to be the same grants as the dashboard's own dev
// environment.
const readSweepPolicy = `grants:
  - resources: ["*"]
    audience: ["group:ops"]
    permissions: ["read", "write"]

  - resources: ["*"]
    audience: ["group:viewers"]
    permissions: ["read"]

  - resources: ["stack:frontend-*"]
    audience: ["group:frontend"]
    permissions: ["read", "write"]
  - resources: ["*"]
    audience: ["group:frontend"]
    permissions: ["read"]

  - resources: ["service:*", "task:*"]
    audience: ["group:oncall"]
    permissions: ["read", "write"]
  - resources: ["node:*", "swarm:*", "config:*", "secret:*", "network:*", "volume:*"]
    audience: ["group:oncall"]
    permissions: ["read"]
`

// startReadSweep brings up a headers-auth SUT with readSweepPolicy at
// operations level 2 — matching compose.dev-auth.yaml's own ACL demo and
// acl_test.go's two cases, so a finding here describes the same server
// configuration as those.
func startReadSweep(t *testing.T, env *harness.Env) *sut.Process {
	t.Helper()

	policy := filepath.Join(t.TempDir(), "acl.yaml")
	if err := os.WriteFile(policy, []byte(readSweepPolicy), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	return sut.Start(t, sut.Config{
		Port:       readSweepPort,
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
}

// readAs issues one GET as persona with the given Accept header (defaulting
// to application/json) and returns the response; the caller closes the body.
func readAs(
	t *testing.T,
	proc *sut.Process,
	persona readPersona,
	path, accept string,
) *http.Response {
	t.Helper()

	if accept == "" {
		accept = "application/json"
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proc.BaseURL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Accept", accept)
	req.Header.Set("X-Auth-User", persona.user)
	if persona.groups != "" {
		req.Header.Set("X-Auth-Groups", persona.groups)
	}

	resp, err := proc.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s as %s: %v", path, persona.name, err)
	}

	return resp
}

// assertStatus fails the test unless resp carries the given status, closing
// the body either way. On mismatch it includes the body so a wrong error code
// is diagnosable without rerunning.
func assertStatus(t *testing.T, resp *http.Response, path, persona string, want int) {
	t.Helper()
	defer resp.Body.Close()

	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf(
			"GET %s as %s: status = %d, want %d: %s",
			path, persona, resp.StatusCode, want, body,
		)
	}
}

// assertForbiddenWithCode asserts a 403 naming the given RFC 9457 problem
// code (ACL001 for a read denial, ACL002 for a write-gated read like
// GET /swarm/unlock-key, OPS001 for an operations-tier gate).
func assertForbiddenWithCode(t *testing.T, resp *http.Response, path, persona, code string) {
	t.Helper()

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Errorf(
			"GET %s as %s: status = %d, want 403: %s",
			path, persona, resp.StatusCode, body,
		)
		return
	}

	if pType := problemType(t, resp); !strings.Contains(pType, code) {
		t.Errorf(
			"GET %s as %s: problem type = %q, want it to name %s",
			path, persona, pType, code,
		)
	}
}

// collectionEnvelope decodes just enough of a CollectionResponse to check
// whether a list came back served (non-empty) or empty-filtered (zero items,
// still 200) without committing to any one resource type's shape.
type collectionEnvelope struct {
	Total int               `json:"total"`
	Items []json.RawMessage `json:"items"`
}

func decodeCollection(t *testing.T, resp *http.Response, path, persona string) collectionEnvelope {
	t.Helper()
	defer resp.Body.Close()

	var env collectionEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("GET %s as %s: decode: %v", path, persona, err)
	}

	if env.Total != len(env.Items) {
		t.Errorf(
			"GET %s as %s: total = %d but items has %d entries",
			path, persona, env.Total, len(env.Items),
		)
	}

	return env
}

// ─── baseline identifiers ───────────────────────────────────────────────

// baselineIDs are the fixture's Docker-assigned identifiers, resolved once
// (as the "ops" persona, which reads everything) and reused across every
// persona check so a driven route always addresses the same concrete
// resource.
type baselineIDs struct {
	serviceID  string
	nodeID     string
	taskID     string
	configID   string
	secretID   string
	networkID  string
	volumeName string
	stackName  string
}

var opsPersona = readPersonas[0]

func resolveBaselineIDs(t *testing.T, proc *sut.Process) baselineIDs {
	t.Helper()

	var ids baselineIDs

	ids.serviceID = readListFindID(t, proc, "/services", "shop_web")
	ids.nodeID = readListFindNodeID(t, proc)
	ids.configID = readListFindID(t, proc, "/configs", "shop-config")
	ids.secretID = readListFindID(t, proc, "/secrets", "shop-secret")
	ids.networkID = readListFindID(t, proc, "/networks", "shop-net")
	ids.volumeName = fixtures.UsedVolume
	ids.stackName = fixtures.StackShop

	// Tasks have no name; find one belonging to the resolved service via its
	// EnrichedTask.ServiceName field.
	ids.taskID = readListFindTaskID(t, proc, "shop_web")

	if ids.serviceID == "" || ids.nodeID == "" || ids.configID == "" ||
		ids.secretID == "" || ids.networkID == "" || ids.taskID == "" {
		t.Fatalf("could not resolve every baseline identifier: %+v", ids)
	}

	return ids
}

func readListFindID(t *testing.T, proc *sut.Process, path, name string) string {
	t.Helper()

	resp := readAs(t, proc, opsPersona, path, "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s as ops: status = %d, want 200", path, resp.StatusCode)
	}

	// Services, configs and secrets carry their name nested under Spec.Name
	// (swarm.Annotations); network.Summary carries it at the top level
	// instead (Docker's SDK type has no Spec at all) -- so both are decoded
	// and whichever is non-empty wins.
	var body struct {
		Items []struct {
			ID   string `json:"ID"`
			Name string `json:"Name"`
			Spec struct {
				Name string `json:"Name"`
			} `json:"Spec"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}

	for _, item := range body.Items {
		if item.Spec.Name == name || item.Name == name {
			return item.ID
		}
	}

	t.Fatalf("%s: %q not found among %d items", path, name, len(body.Items))

	return ""
}

func readListFindNodeID(t *testing.T, proc *sut.Process) string {
	t.Helper()

	resp := readAs(t, proc, opsPersona, "/nodes", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /nodes as ops: status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Items []struct {
			ID string `json:"ID"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /nodes: %v", err)
	}

	if len(body.Items) == 0 {
		t.Fatal("/nodes as ops returned no nodes")
	}

	return body.Items[0].ID
}

func readListFindTaskID(t *testing.T, proc *sut.Process, serviceName string) string {
	t.Helper()

	resp := readAs(t, proc, opsPersona, "/tasks", "")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /tasks as ops: status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Items []struct {
			ID          string `json:"ID"`
			ServiceName string `json:"ServiceName"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /tasks: %v", err)
	}

	for _, item := range body.Items {
		if item.ServiceName == serviceName {
			return item.ID
		}
	}

	t.Fatalf("/tasks: no task belonging to %q found among %d items", serviceName, len(body.Items))

	return ""
}

// ─── generic drivers ────────────────────────────────────────────────────

type readDriveFunc func(t *testing.T, proc *sut.Process, ids baselineIDs)

// driveList checks a standard list endpoint: every granted persona must see at
// least one item and anonymous exactly zero — the filter denying everything,
// not the endpoint refusing. alsoEmpty names granted personas holding no grant
// on this type, which must see zero too.
func driveList(path string, alsoEmpty ...string) readDriveFunc {
	empty := map[string]bool{"anonymous": true}
	for _, name := range alsoEmpty {
		empty[name] = true
	}

	return func(t *testing.T, proc *sut.Process, _ baselineIDs) {
		for _, p := range readPersonas {
			t.Run(p.name, func(t *testing.T) {
				resp := readAs(t, proc, p, path, "")
				if resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
				}

				env := decodeCollection(t, resp, path, p.name)

				if empty[p.name] {
					if env.Total != 0 {
						t.Errorf(
							"%s sees %d items on %s; want 0 — a list endpoint returning "+
								"items a persona has no grant for is a real defect",
							p.name, env.Total, path,
						)
					}
					return
				}

				if env.Total == 0 {
					t.Errorf("%s as %s: 0 items, want at least the fixture's own", p.name, path)
				}
			})
		}
	}
}

// driveDetail checks a standard detail endpoint keyed by pathFn(ids):
// anonymous must be refused with ACL001 and every granted persona served.
// alsoDenied names granted personas expected to be refused too.
func driveDetail(pathFn func(baselineIDs) string, alsoDenied ...string) readDriveFunc {
	denied := map[string]bool{"anonymous": true}
	for _, name := range alsoDenied {
		denied[name] = true
	}

	return func(t *testing.T, proc *sut.Process, ids baselineIDs) {
		path := pathFn(ids)

		for _, p := range readPersonas {
			t.Run(p.name, func(t *testing.T) {
				resp := readAs(t, proc, p, path, "")
				defer resp.Body.Close() //nolint:bodyclose // also closed by whichever assert helper runs below

				if denied[p.name] {
					assertForbiddenWithCode(t, resp, path, p.name, "ACL001")
					return
				}

				assertStatus(t, resp, path, p.name, http.StatusOK)
			})
		}
	}
}

// driveSubresource checks a sub-collection or section GET gated on its
// parent resource's read ACL. Same boundary as driveDetail, kept separate
// because the routes it covers reach it through a different code path.
func driveSubresource(pathFn func(baselineIDs) string) readDriveFunc {
	return driveDetail(pathFn)
}

// driveGrantGate checks a cluster-wide endpoint gated by requireAnyGrant:
// anonymous is refused with ACL001 and no granted persona is, whatever else
// the response is (200, or 503 from an unconfigured Prometheus here).
func driveGrantGate(path, accept string) readDriveFunc {
	return func(t *testing.T, proc *sut.Process, _ baselineIDs) {
		for _, p := range readPersonas {
			t.Run(p.name, func(t *testing.T) {
				resp := readAs(t, proc, p, path, accept)

				if p.name == "anonymous" {
					assertForbiddenWithCode(t, resp, path, p.name, "ACL001")
					return
				}

				defer resp.Body.Close()
				if resp.StatusCode == http.StatusForbidden {
					body, _ := io.ReadAll(resp.Body)
					t.Errorf(
						"%s as %s (has a grant): refused with 403: %s",
						path, p.name, body,
					)
				}
			})
		}
	}
}

// ─── routes needing a specific, hand-derived assertion ─────────────────

// driveSwarm checks GET /swarm: gated by requireAnyGrant, but its body also
// redacts join tokens unless the caller holds write:swarm:cluster. Only ops
// holds it, so every other persona's tokens come back zeroed on a 200.
func driveSwarm(t *testing.T, proc *sut.Process, _ baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			resp := readAs(t, proc, p, "/swarm", "")

			if p.name == "anonymous" {
				assertForbiddenWithCode(t, resp, "/swarm", p.name, "ACL001")
				return
			}

			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
			}

			var body struct {
				Swarm struct {
					JoinTokens struct {
						Worker  string `json:"Worker"`
						Manager string `json:"Manager"`
					} `json:"JoinTokens"`
				} `json:"swarm"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode /swarm: %v", err)
			}

			tokensPresent := body.Swarm.JoinTokens.Worker != "" ||
				body.Swarm.JoinTokens.Manager != ""

			if p.name == "ops" {
				if !tokensPresent {
					t.Error(
						"ops (holds write:swarm:cluster) got zeroed join tokens, want the real ones",
					)
				}
				return
			}

			if tokensPresent {
				t.Errorf(
					"%s (no write:swarm:cluster grant) got non-empty join tokens: %+v",
					p.name, body.Swarm.JoinTokens,
				)
			}
		})
	}
}

// driveSwarmUnlockKey checks the one read route whose authorization boundary is
// a write grant: requireWriteACL runs outside the tier check, so a caller
// without write:swarm:cluster gets ACL002 at any level, and ops — the only
// holder — is then refused by the tier gate, this lane running one below it.
func driveSwarmUnlockKey(t *testing.T, proc *sut.Process, _ baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			resp := readAs(t, proc, p, "/swarm/unlock-key", "")
			defer resp.Body.Close() //nolint:bodyclose // also closed by assertForbiddenWithCode below

			if p.name == "ops" {
				assertForbiddenWithCode(t, resp, "/swarm/unlock-key", p.name, "OPS001")
				return
			}

			assertForbiddenWithCode(t, resp, "/swarm/unlock-key", p.name, "ACL002")
		})
	}
}

// driveProfile checks GET /profile, the identity echo every persona —
// including anonymous, since headers mode always resolves a subject — is
// served. The boundary is in the body, not the status: anonymous's
// permissions map must be empty and every granted persona's must not.
func driveProfile(t *testing.T, proc *sut.Process, _ baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			resp := readAs(t, proc, p, "/profile", "")
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
			}

			// DetailResponse.MarshalJSON (internal/api/jsonld.go) inlines
			// extra's fields alongside @context/@id/@type: permissions sits
			// at the top level, with no nested "profile" wrapper key.
			var body struct {
				Subject     string              `json:"subject"`
				Permissions map[string][]string `json:"permissions"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode /profile: %v", err)
			}

			if p.name == "anonymous" {
				if len(body.Permissions) != 0 {
					t.Errorf(
						"anonymous's /profile permissions = %v, want empty",
						body.Permissions,
					)
				}
				return
			}

			if len(body.Permissions) == 0 {
				t.Errorf("%s's /profile permissions is empty, want at least one grant", p.name)
			}
		})
	}
}

// drivePlugins checks GET /plugins, and /swarm/plugins on the same handler:
// filtered like every other list endpoint and never gated by requireAnyGrant,
// so even anonymous gets 200. This engine has no plugins, so the assertion is
// that no persona is refused rather than that anything is listed.
func drivePlugins(t *testing.T, proc *sut.Process, _ baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			resp := readAs(t, proc, p, "/plugins", "")
			defer resp.Body.Close() //nolint:bodyclose // also closed by assertStatus below
			assertStatus(t, resp, "/plugins", p.name, http.StatusOK)
		})
	}
}

// driveSearch checks GET /search?q=shop: gated by requireAnyGrant like the
// rest of the cluster-wide group, and every granted persona reads
// everything under this policy, so every one of them must find at least one
// hit for a query that matches the fixture's own stack name.
func driveSearch(t *testing.T, proc *sut.Process, _ baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			resp := readAs(t, proc, p, "/search?q=shop&limit=0", "")

			if p.name == "anonymous" {
				assertForbiddenWithCode(t, resp, "/search", p.name, "ACL001")
				return
			}

			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
			}

			var body struct {
				Total int `json:"total"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode /search: %v", err)
			}

			if body.Total == 0 {
				t.Errorf(
					"%s: /search?q=shop found nothing, want at least the fixture's own stack",
					p.name,
				)
			}
		})
	}
}

// driveStacksSummary checks GET /stacks/summary. HandleStackSummary calls
// requireAnyGrant before it lists anything, so anonymous is refused
// outright rather than served an empty list; every granted persona sees
// both fixture stacks.
func driveStacksSummary(t *testing.T, proc *sut.Process, _ baselineIDs) {
	for _, p := range readPersonas {
		t.Run(p.name, func(t *testing.T) {
			resp := readAs(t, proc, p, "/stacks/summary", "")

			if p.name == "anonymous" {
				assertForbiddenWithCode(t, resp, "/stacks/summary", p.name, "ACL001")
				return
			}

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
			}

			env := decodeCollection(t, resp, "/stacks/summary", p.name)

			// oncall holds no stack grant, so it clears the requireAnyGrant gate
			// above but the per-item acl.Filter that follows leaves it with none.
			if p.name == "oncall" {
				if env.Total != 0 {
					t.Errorf("oncall sees %d stack summaries, want 0 (no stack grant)", env.Total)
				}
				return
			}

			if env.Total < 2 {
				t.Errorf(
					"%s: /stacks/summary total = %d, want at least the fixture's 2 stacks",
					p.name,
					env.Total,
				)
			}
		})
	}
}

// ─── the sweep itself ───────────────────────────────────────────────────

// drivenReadRoutes is the single source of truth for what this file
// exercises; every other GET/HEAD route in the inventory must carry a
// reason in excusedReadRoutes instead.
var drivenReadRoutes = map[string]readDriveFunc{
	// Core list + detail, the eight primary resource types.
	"GET /services": driveList("/services"),
	"GET /services/{id}": driveDetail(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID },
	),
	"GET /nodes": driveList("/nodes"),
	"GET /nodes/{id}": driveDetail(
		func(ids baselineIDs) string { return "/nodes/" + ids.nodeID },
	),
	"GET /tasks": driveList("/tasks"),
	"GET /tasks/{id}": driveDetail(
		func(ids baselineIDs) string { return "/tasks/" + ids.taskID },
	),
	"GET /configs": driveList("/configs"),
	"GET /configs/{id}": driveDetail(
		func(ids baselineIDs) string { return "/configs/" + ids.configID },
	),
	"GET /secrets": driveList("/secrets"),
	"GET /secrets/{id}": driveDetail(
		func(ids baselineIDs) string { return "/secrets/" + ids.secretID },
	),
	"GET /networks": driveList("/networks"),
	"GET /networks/{id}": driveDetail(
		func(ids baselineIDs) string { return "/networks/" + ids.networkID },
	),
	"GET /volumes": driveList("/volumes"),
	"GET /volumes/{name}": driveDetail(
		func(ids baselineIDs) string { return "/volumes/" + ids.volumeName },
	),
	"GET /stacks": driveList("/stacks", "oncall"),
	"GET /stacks/{name}": driveDetail(
		func(ids baselineIDs) string { return "/stacks/" + ids.stackName },
		"oncall",
	),

	// Service subresources: all gated on the service's own read ACL
	// (lookupServiceACL), so they share driveDetail's boundary.
	"GET /services/{id}/configs": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/configs" },
	),
	"GET /services/{id}/container-config": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/container-config" },
	),
	"GET /services/{id}/endpoint-mode": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/endpoint-mode" },
	),
	"GET /services/{id}/env": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/env" },
	),
	"GET /services/{id}/healthcheck": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/healthcheck" },
	),
	"GET /services/{id}/labels": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/labels" },
	),
	"GET /services/{id}/log-driver": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/log-driver" },
	),
	"GET /services/{id}/logs": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/logs" },
	),
	"GET /services/{id}/mode": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/mode" },
	),
	"GET /services/{id}/mounts": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/mounts" },
	),
	"GET /services/{id}/networks": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/networks" },
	),
	"GET /services/{id}/placement": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/placement" },
	),
	"GET /services/{id}/ports": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/ports" },
	),
	"GET /services/{id}/resources": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/resources" },
	),
	"GET /services/{id}/rollback-policy": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/rollback-policy" },
	),
	"GET /services/{id}/secrets": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/secrets" },
	),
	"GET /services/{id}/tasks": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/tasks" },
	),
	"GET /services/{id}/update-policy": driveSubresource(
		func(ids baselineIDs) string { return "/services/" + ids.serviceID + "/update-policy" },
	),

	// Node subresources: gated on the node's own read ACL.
	"GET /nodes/{id}/labels": driveSubresource(
		func(ids baselineIDs) string { return "/nodes/" + ids.nodeID + "/labels" },
	),
	"GET /nodes/{id}/role": driveSubresource(
		func(ids baselineIDs) string { return "/nodes/" + ids.nodeID + "/role" },
	),
	"GET /nodes/{id}/tasks": driveSubresource(
		func(ids baselineIDs) string { return "/nodes/" + ids.nodeID + "/tasks" },
	),

	// Config/secret/task subresources.
	"GET /configs/{id}/labels": driveSubresource(
		func(ids baselineIDs) string { return "/configs/" + ids.configID + "/labels" },
	),
	"GET /secrets/{id}/labels": driveSubresource(
		func(ids baselineIDs) string { return "/secrets/" + ids.secretID + "/labels" },
	),
	"GET /tasks/{id}/logs": driveSubresource(
		func(ids baselineIDs) string { return "/tasks/" + ids.taskID + "/logs" },
	),

	// Cluster-wide endpoints gated by requireAnyGrant.
	"GET /cluster":               driveGrantGate("/cluster", ""),
	"GET /cluster/capacity":      driveGrantGate("/cluster/capacity", ""),
	"GET /cluster/metrics":       driveGrantGate("/cluster/metrics", ""),
	"GET /disk-usage":            driveGrantGate("/disk-usage", ""),
	"GET /history":               driveGrantGate("/history", ""),
	"GET /recommendations":       driveGrantGate("/recommendations", ""),
	"GET /metrics":               driveGrantGate("/metrics", ""),
	"GET /metrics/labels":        driveGrantGate("/metrics/labels", ""),
	"GET /metrics/labels/{name}": driveGrantGate("/metrics/labels/instance", ""),
	"GET /metrics/status":        driveGrantGate("/metrics/status", ""),
	"GET /topology":              driveGrantGate("/topology", "application/vnd.jgf+json"),

	// Endpoints needing a specific, hand-derived assertion beyond the
	// generic gate.
	"GET /swarm":            driveSwarm,
	"GET /swarm/unlock-key": driveSwarmUnlockKey,
	"GET /profile":          driveProfile,
	"GET /plugins":          drivePlugins,
	"GET /search":           driveSearch,
	"GET /stacks/summary":   driveStacksSummary,
}

// excusedReadRoutes carries a reason for every GET/HEAD route this file does
// not drive. "gap: " marks a route out of scope for this slice, not yet
// covered. Every other reason is meant to hold permanently.
//
//nolint:gosec // G101: keys are route patterns, not credentials.
var excusedReadRoutes = map[string]string{
	// Auth-exempt by router design ("/-/*", "/api*", "/assets/*", "/auth/*"):
	// these never reach the ACL evaluator, so there is no boundary to assert.
	"GET /-/docker-latest-version": "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /-/health":                "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /-/licenses":              "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /-/licenses/texts/{id}":   "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /-/metrics":               "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /-/notices":               "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /-/ready":                 "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /-/sbom.cdx":              "auth-exempt (/-/*): serves the same response to every persona, no ACL involved",
	"GET /api":                     "auth-exempt (/api*): serves the same response to every persona, no ACL involved",
	"GET /api/context.jsonld":      "auth-exempt (/api*): serves the same response to every persona, no ACL involved",
	"GET /api/errors":              "auth-exempt (/api*): serves the same response to every persona, no ACL involved",
	"GET /api/errors/{code}":       "auth-exempt (/api*): serves the same response to every persona, no ACL involved",
	"GET /api/scalar.js":           "auth-exempt (/api*): serves the same response to every persona, no ACL involved",
	"GET /auth/whoami": "auth-exempt (/auth/*): echoes whatever identity the provider resolved for the " +
		"caller rather than making an authorization decision; not part of the boundary this lane checks",

	// Removed projections: always 410 API012, for every persona, no ACL
	// involved.
	"GET /topology/networks": "removed projection (410 API012, unconditionally) — see internal/api/router.go's " +
		"comment; GET /topology serves the whole graph now",
	"GET /topology/placement": "removed projection (410 API012, unconditionally) — see internal/api/router.go's " +
		"comment; GET /topology serves the whole graph now",

	// Handler dedup / no usable fixture.
	"GET /swarm/plugins": "same handler as GET /plugins (HandleListPlugins), already driven there",
	"GET /plugins/{name}": "this environment's DinD engine has no plugins installed, so any name 404s " +
		"before the ACL check on a specific plugin (plugin:name) is ever reached; the write sweep excuses " +
		"the whole /plugins family for the same reason",

	// Genuinely out of scope for a request/response sweep.
	"GET /events": "SSE-only ACL filtering (aclMatchWrap) needs a streaming client to observe, not a " +
		"plain GET — a JSON Accept header hits the switch's default case and serves the SPA regardless of " +
		"persona. Out of scope for this request/response sweep, and driven instead by " +
		"TestEventsStreamAppliesTheACL in sse_acl_test.go, which holds two subscribers open while the " +
		"cluster changes underneath them",
}

// TestReadSweepEnforcesTheACLBoundary drives every route in
// drivenReadRoutes against a real cluster and personas mirroring
// compose.dev-auth.yaml.
func TestReadSweepEnforcesTheACLBoundary(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startReadSweep(t, env)
	ids := resolveBaselineIDs(t, proc)

	keys := make([]string, 0, len(drivenReadRoutes))
	for route := range drivenReadRoutes {
		keys = append(keys, route)
	}
	slices.Sort(keys)

	for _, route := range keys {
		drive := drivenReadRoutes[route]
		t.Run(route, func(t *testing.T) {
			drive(t, proc, ids)
		})
	}
}

// TestEveryReadRouteIsDrivenOrExcused requires every GET/HEAD route
// contract.Routes() reports to appear in drivenReadRoutes or
// excusedReadRoutes. Keys are bare "GET ..." because ServeMux answers HEAD
// off the GET registration; there is no separate HEAD route to enumerate.
func TestEveryReadRouteIsDrivenOrExcused(t *testing.T) {
	routes := contractRoutes(t)

	live := make(map[string]bool, len(routes))
	var gaps []string

	for _, route := range routes {
		if route.Method != http.MethodGet && route.Method != http.MethodHead {
			continue
		}

		key := route.String()
		live[key] = true

		if _, ok := drivenReadRoutes[key]; ok {
			continue
		}

		reason, ok := excusedReadRoutes[key]
		if !ok {
			t.Errorf(
				"%s is neither driven nor excused; add coverage in drivenReadRoutes "+
					"or a reason in excusedReadRoutes",
				key,
			)
			continue
		}

		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s has an empty excuse reason", key)
		}

		if strings.HasPrefix(reason, "gap:") {
			gaps = append(gaps, key)
		}
	}

	for key := range drivenReadRoutes {
		if !live[key] {
			t.Errorf("drivenReadRoutes has a stale entry %q: no such GET/HEAD route in the "+
				"current inventory", key)
		}
	}

	for key := range excusedReadRoutes {
		if !live[key] {
			t.Errorf("excusedReadRoutes has a stale entry %q: no such GET/HEAD route in the "+
				"current inventory", key)
		}
	}

	slices.Sort(gaps)
	t.Logf(
		"read sweep: %d driven, %d excused (%d of them gaps)\ngaps:\n  %s",
		len(drivenReadRoutes), len(excusedReadRoutes), len(gaps), strings.Join(gaps, "\n  "),
	)
}
