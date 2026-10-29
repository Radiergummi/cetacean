package imagecheck

import (
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

const (
	digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestParsePinnedNormalisesLikeDocker(t *testing.T) {
	cases := []struct {
		image string
		want  Reference
	}{
		{"nginx:1.27@" + digestA, Reference{"registry-1.docker.io", "library/nginx", "1.27"}},
		{
			"docker.io/library/nginx:1.27@" + digestA,
			Reference{"registry-1.docker.io", "library/nginx", "1.27"},
		},
		{
			"index.docker.io/nginx:1.27@" + digestA,
			Reference{"registry-1.docker.io", "library/nginx", "1.27"},
		},
		{
			"grafana/grafana:11@" + digestA,
			Reference{"registry-1.docker.io", "grafana/grafana", "11"},
		},
		{"ghcr.io/org/app:v2@" + digestA, Reference{"ghcr.io", "org/app", "v2"}},
		{
			"registry.example.com:5000/team/app:latest@" + digestA,
			Reference{"registry.example.com:5000", "team/app", "latest"},
		},
		{"localhost/app:dev@" + digestA, Reference{"localhost", "app", "dev"}},
	}

	for _, tc := range cases {
		t.Run(tc.image, func(t *testing.T) {
			got, pinned, ok := ParsePinned(tc.image)
			if !ok {
				t.Fatal("not parsed")
			}

			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}

			if pinned != digestA {
				t.Errorf("pinned = %q", pinned)
			}
		})
	}
}

func TestParsePinnedSharesOneKeyAcrossSpellings(t *testing.T) {
	a, _, _ := ParsePinned("nginx:1@" + digestA)
	b, _, _ := ParsePinned("docker.io/library/nginx:1@" + digestB)

	if a.Key() != b.Key() {
		t.Errorf("%q and %q are one image", a.Key(), b.Key())
	}
}

// Only a tag is ever asked about, and only when there is a digest to hold it to.
func TestParsePinnedSkipsWhatCannotBeCompared(t *testing.T) {
	spec.Satisfies(t,
		"oci/distribution-spec/reference-is-a-digest-or-a-tag",
		"oci/distribution-spec/reference-has-no-other-format",
	)

	for _, image := range []string{
		"nginx@" + digestA, // pinned by digest only
		"nginx",            // no tag, no digest
		"nginx:1.27",       // a tag with nothing to compare against
		"registry.example.com:5000/app@" + digestA, // the port is not a tag
		"nginx:1.27@sha256:short",                  // not a digest
	} {
		if ref, _, ok := ParsePinned(image); ok {
			t.Errorf("%q parsed as %+v; want it skipped", image, ref)
		}
	}
}

func TestParsePinnedHoldsTheNameGrammar(t *testing.T) {
	spec.Satisfies(t, "oci/distribution-spec/name-matches-the-grammar")

	for _, image := range []string{
		"ghcr.io/Org/app:1@" + digestA,
		"ghcr.io/org/-app:1@" + digestA,
		"ghcr.io/org//app:1@" + digestA,
	} {
		if _, _, ok := ParsePinned(image); ok {
			t.Errorf("%q has a repository outside the grammar but parsed", image)
		}
	}
}

func TestParsePinnedHoldsTheTagGrammar(t *testing.T) {
	spec.Satisfies(t,
		"oci/distribution-spec/tag-matches-the-grammar",
		"oci/distribution-spec/tag-is-at-most-128-characters",
	)

	if _, _, ok := ParsePinned("app:" + strings.Repeat("a", 128) + "@" + digestA); !ok {
		t.Error("a 128-character tag was refused")
	}

	for _, tag := range []string{strings.Repeat("a", 129), ".hidden", "-dash"} {
		if _, _, ok := ParsePinned("app:" + tag + "@" + digestA); ok {
			t.Errorf("tag %q parsed", tag)
		}
	}
}

func TestParseDigestedAcceptsADigestWithOrWithoutATag(t *testing.T) {
	digest512 := "sha512:" + strings.Repeat("c", 128)

	cases := []struct {
		image  string
		want   Reference
		digest string
	}{
		{"nginx@" + digestA, Reference{"registry-1.docker.io", "library/nginx", ""}, digestA},
		{"ghcr.io/a/b:1@" + digest512, Reference{"ghcr.io", "a/b", "1"}, digest512},
		{
			"registry.example.com:5000/app@" + digestA,
			Reference{"registry.example.com:5000", "app", ""},
			digestA,
		},
	}

	for _, tc := range cases {
		got, digest, ok := ParseDigested(tc.image)
		if !ok || got != tc.want || digest != tc.digest {
			t.Errorf("%q: got %+v, %q, %v", tc.image, got, digest, ok)
		}
	}

	for _, image := range []string{"nginx:1", "nginx@md5:abc", "Nginx@" + digestA} {
		if ref, _, ok := ParseDigested(image); ok {
			t.Errorf("%q parsed as %+v", image, ref)
		}
	}
}
