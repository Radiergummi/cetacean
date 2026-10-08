//go:build e2e

package e2e_test

import (
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/test/e2e/fixtures"
	"github.com/radiergummi/cetacean/test/e2e/harness"
)

// This file is the OAuth lane's self-enforcing gate: every endpoint the server
// advertises must be driven by a named case. It reads the server's own
// discovery documents, so a path computed per resource is covered like a
// literal one — the earlier version parsed RegisterRoutes for string literals
// and went blind the moment a path stopped being one.
//
// A route mounted but advertised nowhere is outside what this can see. No
// conformant client can reach one, so adding it deliberately means adding the
// case that drives it here too.

// drivenOAuthEndpoints maps "METHOD path" to the case that exercises it.
var drivenOAuthEndpoints = map[string]string{
	"GET /oauth/jwks": "TestMCPOAuthPublishesItsVerificationKey — fetched at the advertised " +
		"jwks_uri without credentials, asserted to carry an ES256 key and not its private half.",

	"POST /oauth/register": "TestMCPOAuthFlow/register and its four rejection cases; " +
		"TestMCPOAuthDCRRateLimit drives the per-IP limit; TestMCPOAuthWithoutDCROrCIMD " +
		"asserts the endpoint is absent when DCR is off.",

	"GET /oauth/authorize": "TestMCPOAuthFlow/consent_page and every flow that reaches a " +
		"code, plus the unauthenticated, unregistered-redirect, non-S256 and " +
		"missing-resource refusals.",

	"POST /oauth/authorize": "TestMCPOAuthFlow/approve, /deny and the two CSRF cases.",

	"POST /oauth/token": "TestMCPOAuthFlow/token_exchange, /pkce_mismatch, /code_is_single_use, " +
		"/refresh_rotates and TestMCPOAuthStateSurvivesARestart.",

	"POST /oauth/revoke": "TestMCPOAuthFlow/revocation_ends_the_grant.",
}

// drivenOAuthDocuments are the discovery documents, which advertise the rest
// rather than being advertised themselves. A client reaches each by the
// convention its RFC lays down, which is the chain discoverOAuth follows.
var drivenOAuthDocuments = map[string]string{
	"GET /.well-known/oauth-authorization-server": "TestMCPOAuthFlow/discovery — reached via " +
		"the PRM's authorization_servers, the last link of the chain a real client walks.",

	"GET /.well-known/openid-configuration": "TestMCPOAuthFlow/discovery — asserted to serve " +
		"the identical document as the RFC 8414 path it aliases.",

	"GET /.well-known/oauth-protected-resource": "TestMCPOAuthFlow/discovery — named by the " +
		"401's resource_metadata parameter; the first document discoverOAuth fetches.",
}

// pathOf reduces an advertised absolute URL to the path a driven entry names.
func pathOf(t *testing.T, raw string) string {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("advertised endpoint %q does not parse: %v", raw, err)
	}

	return parsed.Path
}

func TestEveryAdvertisedOAuthEndpointIsDriven(t *testing.T) {
	env := harness.Up(t)
	env.SwarmInit(t)
	fixtures.DeployBaseline(t, env)

	proc := startOAuth(t, env, t.TempDir(), nil)
	discovery := discoverOAuth(t, proc)

	// What the server tells a client to use. An empty one is not advertised by
	// this deployment and so is nothing to drive.
	advertised := map[string]string{
		"authorization_endpoint": discovery.authorizeURL,
		"token_endpoint":         discovery.tokenURL,
		"revocation_endpoint":    discovery.revokeURL,
		"registration_endpoint":  discovery.registerURL,
		"jwks_uri":               discovery.jwksURL,
	}

	drivenPaths := map[string]bool{}
	for endpoint := range drivenOAuthEndpoints {
		_, path, found := strings.Cut(endpoint, " ")
		if !found {
			t.Fatalf("drivenOAuthEndpoints key %q is not \"METHOD path\"", endpoint)
		}

		drivenPaths[path] = true
	}

	for field, raw := range advertised {
		if raw == "" {
			continue
		}

		if path := pathOf(t, raw); !drivenPaths[path] {
			t.Errorf(
				"%s advertises %s, which no entry in drivenOAuthEndpoints names; "+
					"add the case that drives it",
				field, path,
			)
		}
	}

	// The reverse direction: an entry naming a path the server no longer
	// advertises is describing coverage of something clients cannot reach.
	advertisedPaths := map[string]bool{}
	for _, raw := range advertised {
		if raw != "" {
			advertisedPaths[pathOf(t, raw)] = true
		}
	}

	for endpoint := range drivenOAuthEndpoints {
		_, path, _ := strings.Cut(endpoint, " ")

		if !advertisedPaths[path] {
			t.Errorf(
				"drivenOAuthEndpoints has a stale entry %q: the server advertises no "+
					"such endpoint",
				endpoint,
			)
		}
	}

	for endpoint, reason := range drivenOAuthDocuments {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s is listed as driven with no case named", endpoint)
		}
	}

	names := make([]string, 0, len(advertised))
	for field, raw := range advertised {
		if raw != "" {
			names = append(names, field)
		}
	}

	sort.Strings(names)
	t.Logf("advertised and driven: %s", strings.Join(names, ", "))
}
