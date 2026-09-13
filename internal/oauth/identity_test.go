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
	}, s.cfg.MCP.AccessTokenTTL)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	got, err := s.Identify(token)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	// Exact, because the empty fields carry meaning: a grant keyed on email
	// matches a session and not a token, since a token has no email to match.
	want := &auth.Identity{
		Subject:  "alice@example.com",
		Groups:   []string{"ops", "sre"},
		Provider: ProviderName,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("identity = %+v, want %+v", got, want)
	}
}

// A resource server decides whether a token was ours at all by classifying the
// failure, so the verifier's error must arrive unwrapped.
func TestIdentifyReturnsTheVerifierErrorUnwrapped(t *testing.T) {
	s := newTestServer(t)

	if _, err := s.Identify("not-a-jwt"); !errors.Is(err, ErrMalformedToken) {
		t.Errorf("err = %v, want ErrMalformedToken", err)
	}
}
