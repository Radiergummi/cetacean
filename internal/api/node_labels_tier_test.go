package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// Relabelling a node is a tier 2 operation on both transports. MCP's
// update_node_labels is registered at OpsConfiguration and kept separate from
// update_node precisely so a deployment can let an agent relabel a node
// without also letting it demote a manager (docs/mcp-tools.mdx, and the
// update_node description itself). REST gated the same edit at tier 3, so the
// dashboard answered OPS001 for an edit MCP performed — the transport drift
// internal/cluster exists to prevent.
//
// The reciprocal test is TestNodeLabelEditorMatchesTheRESTTier in
// internal/mcp. The
// two packages are deliberately decoupled — neither imports the other — so
// this is two tests naming one rule rather than one test driving both.

// patchNodeLabels sends a merge patch to the node labels endpoint at the
// given operations level and returns the response.
func patchNodeLabels(t testing.TB, level config.OperationsLevel) *httptest.ResponseRecorder {
	t.Helper()

	router := newSeededTestRouter(t, withOpsLevel(level))

	req := httptest.NewRequest(
		"PATCH",
		"/nodes/node1/labels",
		strings.NewReader(`{"zone":"eu-central"}`),
	)
	req.Header.Set("Content-Type", "application/merge-patch+json")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	return rec
}

// TestPatchNodeLabelsIsAdmittedAtTierTwo fails while the REST route is gated
// at tier 3: the edit MCP performs at level 2 comes back as OPS001.
func TestPatchNodeLabelsIsAdmittedAtTierTwo(t *testing.T) {
	rec := patchNodeLabels(t, config.OpsConfiguration)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("status = 403 at operations level 2, want the edit admitted; body: %s",
			rec.Body.String())
	}
}

// TestPatchNodeLabelsIsRefusedBelowTierTwo fails if the move over-corrects
// and drops the gate a tier lower than MCP's.
func TestPatchNodeLabelsIsRefusedBelowTierTwo(t *testing.T) {
	rec := patchNodeLabels(t, config.OpsOperational)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d at operations level 1, want 403; body: %s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OPS001") {
		t.Errorf("refusal is not OPS001: %s", rec.Body.String())
	}
}

// TestAllowHeaderOffersNodePatchAtTierTwo fails while allow.go still calls a
// node PATCH impactful: the Allow header is what the dashboard gates its
// affordances on, so an out-of-date entry hides the labels editor on an API
// that would now accept the edit. The tier is stated in two places — the
// route's chain and resourceWriteMethods — and only a test spanning both
// catches one moving without the other.
//
// It calls setAllow directly, as the rest of allow_test.go does: the table
// is all that decides this header, so a seeded router would add a cache, a
// write client and a plugin client that no assertion here reads.
func TestAllowHeaderOffersNodePatchAtTierTwo(t *testing.T) {
	evaluator := acl.NewEvaluator()
	evaluator.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{Resources: []string{"*"}, Audience: []string{"*"}, Permissions: []string{"write"}},
	}})

	h := newTestHandlers(t, withACL(evaluator), withOpsLevel(config.OpsConfiguration))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nodes/node1", nil)
	r = r.WithContext(auth.ContextWithIdentity(r.Context(), &auth.Identity{Subject: "alice"}))

	h.setAllow(w, r, "node", "node1")

	if allow := w.Header().Get("Allow"); !strings.Contains(allow, "PATCH") {
		t.Errorf("Allow = %q at operations level 2, want PATCH offered — "+
			"the route admits it", allow)
	}
}
