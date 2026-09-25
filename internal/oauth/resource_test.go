package oauth

import (
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/config"
	"github.com/radiergummi/cetacean/internal/spec"
)

// The first configured resource is the default, so the order a deployment lists
// them in decides which audience an unindicated grant is bound to and which
// document a refusal points at. Nothing but this pins it: swapping the two
// appends that build the set would compile, pass every other test, and quietly
// move every such grant to the other resource.
func TestTheFirstResourceIsTheDefault(t *testing.T) {
	const issuer = "https://cetacean.test"

	newServerWith := func(resources ...Resource) *Server {
		return NewServer(ServerConfig{
			Issuer:     issuer,
			Resources:  resources,
			OAuth:      config.OAuthConfig{AccessTokenTTL: time.Hour},
			SigningKey: []byte("test-signing-key-32bytes-padded!!"),
		})
	}

	root := Resource{Path: "", Realm: "cetacean"}
	sub := Resource{Path: "/sub", Realm: "cetacean-sub"}

	for _, c := range []struct {
		name      string
		resources []Resource
		want      string
	}{
		{"the root listed first", []Resource{root, sub}, issuer},
		{"a sub-resource listed first", []Resource{sub, root}, issuer + "/sub"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newServerWith(c.resources...)

			got, err := s.resources.effectiveResource(nil, false)
			if err != nil {
				t.Fatalf("effectiveResource: %v", err)
			}
			if got != c.want {
				t.Errorf("unindicated request resolved to %q, want %q", got, c.want)
			}
		})
	}
}

// A server handed no resources at all protects the deployment root, and the
// default is settled once in NewServer rather than re-derived by each reader.
func TestNoConfiguredResourcesMeansTheDeploymentRoot(t *testing.T) {
	s := NewServer(ServerConfig{
		Issuer:     "https://cetacean.test",
		OAuth:      config.OAuthConfig{AccessTokenTTL: time.Hour},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	})

	if got := s.ResourceIdentifiers(); len(got) != 1 || got[0] != "https://cetacean.test" {
		t.Errorf("identifiers = %v, want [https://cetacean.test]", got)
	}
}

// NewServer normalizes the paths it was configured with. It takes the config by
// value, but a struct copy shares the slice's backing array, so normalizing in
// place rewrites the []Resource the caller still holds — and a caller reusing
// one across two servers would never see it happen.
func TestNewServerLeavesTheCallersResourcesAlone(t *testing.T) {
	resources := []Resource{{Path: "sub/", Realm: "cetacean-sub"}}

	NewServer(ServerConfig{
		Issuer:     "https://cetacean.test",
		Resources:  resources,
		OAuth:      config.OAuthConfig{AccessTokenTTL: time.Hour},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	})

	if resources[0].Path != "sub/" {
		t.Errorf("Path = %q, want it as the caller wrote it", resources[0].Path)
	}
}

// The property resource indicators exist for: an identifier is matched whole,
// so a token minted for one resource is refused at another even when one path
// sits beneath the other. Nothing else pins it — every other Identify test
// presents a token at the resource it was minted for, which passes whether
// identifiers are compared whole or by prefix.
func TestATokenDoesNotReachAResourceItWasNotMintedFor(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc9700/tokens-audience-restricted")

	const issuer = "https://cetacean.test"

	root := Resource{Path: "", Realm: "cetacean"}
	sub := Resource{Path: "/sub", Realm: "cetacean-sub"}

	s := NewServer(ServerConfig{
		Issuer:     issuer,
		Resources:  []Resource{root, sub},
		OAuth:      config.OAuthConfig{AccessTokenTTL: time.Hour},
		SigningKey: []byte("test-signing-key-32bytes-padded!!"),
	})

	rootID := s.cfg.identifierOf(root)
	subID := s.cfg.identifierOf(sub)

	if rootID == subID {
		t.Fatalf("both resources resolved to %q, so this proves nothing", rootID)
	}

	mint := func(t *testing.T, audience string) string {
		t.Helper()

		token, err := s.tokenIssuer.IssueAccessToken(
			AccessTokenClaims{Subject: "alice@example.com", ClientID: "test-client"},
			audience,
			s.cfg.OAuth.AccessTokenTTL,
		)
		if err != nil {
			t.Fatalf("IssueAccessToken for %s: %v", audience, err)
		}

		return token
	}

	for _, c := range []struct {
		name             string
		mintedFor, shown string
	}{
		{"the root's token at the resource beneath it", rootID, subID},
		{"a sub-resource's token at the root above it", subID, rootID},
	} {
		t.Run(c.name, func(t *testing.T) {
			token := mint(t, c.mintedFor)

			if _, err := s.Identify(token, c.mintedFor); err != nil {
				t.Fatalf("the token was refused at its own resource: %v", err)
			}

			if _, err := s.Identify(token, c.shown); err == nil {
				t.Errorf("a token audienced for %s was accepted at %s", c.mintedFor, c.shown)
			}
		})
	}
}

// RFC 8707 binds a token to one resource, so an identifier that merely extends
// a configured one is a different resource and must be refused. Matching by
// prefix here is the audience confusion the parameter exists to prevent.
func TestAnIdentifierExtendingAConfiguredResourceIsRefused(t *testing.T) {
	spec.Satisfies(t, "oauth/rfc8707/single-resource-per-token")

	s := newTestServer(t)

	for _, suffix := range []string{
		"/not-a-resource",
		"-suffixed",
		"/../elsewhere",
	} {
		t.Run(suffix, func(t *testing.T) {
			raw := s.resources.fallback + suffix

			got, err := s.resources.effectiveResource([]string{raw}, false)
			if err == nil {
				t.Fatalf("%q was accepted and resolved to %q", raw, got)
			}
		})
	}
}
