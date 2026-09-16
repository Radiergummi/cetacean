package oauth

import (
	"errors"
	"reflect"
	"testing"

	"github.com/radiergummi/cetacean/internal/auth"
)

func TestIdentifyCarriesTheClaimsAndNothingElse(t *testing.T) {
	s := newTestServer(t)

	token, err := s.tokenIssuer.IssueAccessToken(AccessTokenClaims{
		Subject:  "alice@example.com",
		Groups:   []string{"ops", "sre"},
		ClientID: "test-client",
	}, s.resources.fallback, s.cfg.OAuth.AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	got, err := s.Identify(token, s.resources.fallback)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	// Exact, because nothing may be invented: a claim the token does not carry
	// must arrive empty rather than be guessed at from another one. The
	// provider label is spelled out rather than taken from the constant: it
	// reaches an auth.Identity that other code may compare, so changing it
	// should be a visible decision.
	want := &auth.Identity{
		Subject:  "alice@example.com",
		Groups:   []string{"ops", "sre"},
		Provider: "oauth",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("identity = %+v, want %+v", got, want)
	}
}

// A resource server decides whether a token was ours at all by classifying the
// failure, so the verifier's error must arrive unwrapped.
func TestIdentifyReturnsTheVerifierErrorUnwrapped(t *testing.T) {
	s := newTestServer(t)

	if _, err := s.Identify("not-a-jwt", s.resources.fallback); !errors.Is(
		err,
		ErrMalformedToken,
	) {
		t.Errorf("err = %v, want ErrMalformedToken", err)
	}
}
