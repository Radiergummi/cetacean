package oauth

import (
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/config"
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

			got, err := s.resources.effectiveResource("", false)
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
