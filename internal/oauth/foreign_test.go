package oauth

import (
	"errors"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
)

// Every way verification can fail, classified as somebody else's token or as
// ours failing to prove itself. The errors come out of real verification rather
// than being constructed here: the mapping is only worth anything if it matches
// what the verifier actually returns.
//
// Getting a row wrong is silent in both directions. A missing mark breaks bearer
// auth against the upstream IdP; a spurious one lets a token that failed our
// checks be re-judged by the provider on weaker evidence.
func TestForeignSeparatesOtherIssuersFromFailedProofs(t *testing.T) {
	s := newTestServer(t)
	audience := s.resources.fallback

	mint := func(issuer *TokenIssuer, aud string, ttl time.Duration) string {
		t.Helper()

		token, err := issuer.IssueAccessToken(
			AccessTokenClaims{Subject: "alice", ClientID: "client-1"},
			aud,
			ttl,
		)
		if err != nil {
			t.Fatalf("IssueAccessToken: %v", err)
		}

		return token
	}

	otherKey := mustTokenIssuer(t, []byte("another-root-32-bytes-of-padding"), s.cfg.issuerID())
	otherIssuer := mustTokenIssuer(t, []byte(testKey), "https://attacker.example")

	// want names the sentinel as well as the verdict, because the verdict alone
	// would still pass if two rows started reporting the same error.
	cases := []struct {
		name    string
		token   string
		want    error
		foreign bool
	}{
		{
			name:    "not a JWT at all, which is what an opaque provider token looks like",
			token:   "opaque-provider-token",
			want:    ErrMalformedToken,
			foreign: true,
		},
		{
			// Signed with a key that is not ours either, which is the ordinary
			// case: a foreign token fails both checks. It must report the issuer,
			// because that is the one a caller can route on.
			name:    "a JWT under another issuer",
			token:   mint(otherIssuer, audience, time.Hour),
			want:    ErrIssuerMismatch,
			foreign: true,
		},
		{
			name:    "our issuer, signed with a key that is not ours",
			token:   mint(otherKey, audience, time.Hour),
			want:    ErrInvalidSig,
			foreign: false,
		},
		{
			name:    "ours, expired",
			token:   mint(s.tokenIssuer, audience, -time.Hour),
			want:    ErrTokenExpired,
			foreign: false,
		},
		{
			name:    "ours, minted for a different resource",
			token:   mint(s.tokenIssuer, audience+"/elsewhere", time.Hour),
			want:    ErrAudienceMismatch,
			foreign: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := s.Identify(c.token, audience)
			if err == nil {
				t.Fatal("verification succeeded; this case must fail to be classified")
			}

			// The verifier's own error survives the wrap, so a caller that wants
			// the specific failure still reaches it.
			if !errors.Is(err, c.want) {
				t.Errorf("error = %v, want errors.Is(%v)", err, c.want)
			}

			if got := errors.Is(err, auth.ErrForeignToken); got != c.foreign {
				t.Errorf("errors.Is(%v, ErrForeignToken) = %v, want %v", err, got, c.foreign)
			}
		})
	}
}
