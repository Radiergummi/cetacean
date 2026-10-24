package auth_test

import (
	"testing"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/auth"
)

// Someone who registers another person's address at the provider, which marks
// it unverified, must not receive that person's `user:` grants.
func TestAnUnverifiedEmailClaimDoesNotMatchAUserGrant(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{{
		Resources:   []string{"*"},
		Audience:    []string{"user:alice@corp.com"},
		Permissions: []string{"write"},
	}}})

	claims := map[string]any{
		"sub":            "attacker-1",
		"email":          "alice@corp.com",
		"email_verified": false,
	}
	if e.Can(auth.ClaimsToIdentity(claims), "read", "service:web") {
		t.Error("an unverified email claim matched the grant")
	}

	claims["email_verified"] = true
	if !e.Can(auth.ClaimsToIdentity(claims), "write", "service:web") {
		t.Error("a verified email claim did not match the grant")
	}

	delete(claims, "email_verified")
	if !e.Can(auth.ClaimsToIdentity(claims), "write", "service:web") {
		t.Error("an email claim without email_verified did not match the grant")
	}
}
