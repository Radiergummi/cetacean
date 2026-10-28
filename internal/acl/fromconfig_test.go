package acl

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/config"
)

const everyoneReads = `{"grants":[{"resources":["service:*"],"audience":["*"],"permissions":["read"]}]}`

// Without an identity to match, a policy has nothing to apply to.
func TestFromConfigBuildsNoEvaluatorWithoutAuth(t *testing.T) {
	e, stop, err := FromConfig("none", config.ACLConfig{Policy: everyoneReads}, nil)
	if err != nil || e != nil {
		t.Fatalf("FromConfig(none) = (%v, _, %v), want no evaluator and no error", e, err)
	}

	stop()
}

func TestFromConfigAppliesAnInlinePolicy(t *testing.T) {
	e, stop, err := FromConfig("headers", config.ACLConfig{Policy: everyoneReads}, nil)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	defer stop()

	if !e.Can(&auth.Identity{Subject: "alice"}, "read", "service:web") {
		t.Error("the inline policy's grant does not apply")
	}
}

func TestFromConfigRefuses(t *testing.T) {
	for name, tt := range map[string]struct {
		mode    string
		cfg     config.ACLConfig
		wantErr string
	}{
		"an unparsable policy": {"headers", config.ACLConfig{Policy: "{"}, "inline ACL policy"},
		"a missing policy file": {
			"headers", config.ACLConfig{PolicyFile: "/nonexistent/policy.json"}, "/nonexistent/policy.json",
		},
		"an unknown OIDC user claim": {
			"oidc", config.ACLConfig{OIDCUserClaims: []string{"shoe_size"}}, "acl.oidc_user_claims",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := FromConfig(tt.mode, tt.cfg, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("FromConfig() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}
