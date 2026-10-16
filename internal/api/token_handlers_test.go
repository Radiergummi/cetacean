package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

// stubRevoker answers as an authorization server holding no grants.
type stubRevoker struct{}

func (stubRevoker) RevokeSubject(string) int { return 0 }

// recordingRevoker remembers whom it was asked to revoke.
type recordingRevoker struct{ subjects []string }

func (r *recordingRevoker) RevokeSubject(subject string) int {
	r.subjects = append(r.subjects, subject)

	return 2
}

// Revoking a user's tokens is held to the bar reading the join tokens is: level
// 3 and write on swarm:cluster. The subject arrives percent-decoded.
func TestRevokeSubjectIsGatedLikeTheJoinTokens(t *testing.T) {
	revoke := func(t *testing.T, level config.OperationsLevel, grant string) (int, *recordingRevoker) {
		t.Helper()

		e := acl.NewEvaluator()
		e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
			Resources: []string{grant}, Audience: []string{"*"}, Permissions: []string{"write"},
		}}})
		revoker := &recordingRevoker{}
		router := newTestRouterWithConfig(t,
			[]routerOption{func(c *RouterConfig) { c.TokenRevoker = revoker }},
			withACL(e), withOpsLevel(level))

		req := httptest.NewRequest(http.MethodDelete, "/tokens/alice%40example.com", nil)
		req = req.WithContext(
			auth.ContextWithIdentity(req.Context(), &auth.Identity{Subject: "op"}),
		)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code == http.StatusNoContent && rec.Header().Get("Revoked-Grants") != "2" {
			t.Errorf("Revoked-Grants = %q, want 2", rec.Header().Get("Revoked-Grants"))
		}

		return rec.Code, revoker
	}

	if code, revoker := revoke(
		t,
		config.OpsImpactful,
		"swarm:cluster",
	); code != http.StatusNoContent ||
		len(revoker.subjects) != 1 ||
		revoker.subjects[0] != "alice@example.com" {
		t.Errorf(
			"level 3 with swarm:cluster: status %d, revoked %v; want 204 for alice@example.com",
			code,
			revoker.subjects,
		)
	}
	if code, revoker := revoke(
		t,
		config.OpsConfiguration,
		"swarm:cluster",
	); code == http.StatusNoContent ||
		len(revoker.subjects) != 0 {
		t.Errorf("level 2: status %d, revoked %v; want a refusal", code, revoker.subjects)
	}
	if code, revoker := revoke(t, config.OpsImpactful, "service:*"); code == http.StatusNoContent ||
		len(revoker.subjects) != 0 {
		t.Errorf(
			"no swarm:cluster grant: status %d, revoked %v; want a refusal",
			code,
			revoker.subjects,
		)
	}
}

// Without an authorization server there is nothing to revoke, and the route
// answers as for a resource that is not there.
func TestRevokeSubjectWithoutAnAuthorizationServerIsNotFound(t *testing.T) {
	router := newTestRouterWithConfig(t,
		[]routerOption{func(c *RouterConfig) { c.TokenRevoker = nil }},
		withOpsLevel(config.OpsImpactful))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/tokens/alice", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
