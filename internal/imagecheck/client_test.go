package imagecheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/spec"
)

const (
	indexDigest    = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	manifestDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// fakeRegistry answers manifest requests the way Docker Hub does: a Bearer
// challenge first, a pull token from its realm, and an index digest to a
// client that accepts one.
type fakeRegistry struct {
	*httptest.Server

	private     bool   // the token service hands out tokens that pull nothing
	omitDigest  bool   // HEAD answers carry no Docker-Content-Digest
	statedOnGet string // the Docker-Content-Digest a GET states
	body        string // what a GET sends
	open        bool   // no auth at all
	hang        bool   // never answers
	realm       string // the realm challenges name, when not the registry's own /token

	tokenRequests    atomic.Int64
	manifestRequests atomic.Int64
	lastAccept       atomic.Value
	lastScope        atomic.Value
}

const issuedToken = "anonymous-pull-token"

func newFakeRegistry(t *testing.T, configure func(*fakeRegistry)) *fakeRegistry {
	t.Helper()

	r := &fakeRegistry{}
	if configure != nil {
		configure(r)
	}

	r.Server = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.Close)

	return r
}

func (r *fakeRegistry) serve(w http.ResponseWriter, req *http.Request) {
	if r.hang {
		<-req.Context().Done()

		return
	}

	if req.URL.Path == "/token" {
		r.tokenRequests.Add(1)
		r.lastScope.Store(req.URL.Query().Get("scope"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"` + issuedToken + `","expires_in":300}`))

		return
	}

	if !r.open && (r.private || req.Header.Get("Authorization") != "Bearer "+issuedToken) {
		realm := r.realm
		if realm == "" {
			realm = r.URL + "/token"
		}

		w.Header().Set("WWW-Authenticate",
			`Bearer realm="`+realm+`",service="registry.test"`)
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	if !strings.HasPrefix(req.URL.Path, "/v2/library/app/manifests/") {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	r.manifestRequests.Add(1)
	accept := req.Header.Get("Accept")
	r.lastAccept.Store(accept)

	if req.Method == http.MethodGet {
		if r.statedOnGet != "" {
			w.Header().Set("Docker-Content-Digest", r.statedOnGet)
		}

		_, _ = w.Write([]byte(r.body))

		return
	}

	if r.omitDigest {
		return
	}

	if strings.Contains(accept, "application/vnd.oci.image.index.v1+json") {
		w.Header().Set("Docker-Content-Digest", indexDigest)
	} else {
		w.Header().Set("Docker-Content-Digest", manifestDigest)
	}
}

func (r *fakeRegistry) ref(tag string) Reference {
	return Reference{
		Host:       strings.TrimPrefix(r.URL, "https://"),
		Repository: "library/app",
		Tag:        tag,
	}
}

func (r *fakeRegistry) client() *Client {
	return NewClient(r.Client())
}

func TestResolveAnswersTheTokenChallenge(t *testing.T) {
	registry := newFakeRegistry(t, nil)
	client := registry.client()

	digest, err := client.Resolve(context.Background(), registry.ref("1"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if digest != indexDigest {
		t.Errorf("digest = %q, want the index digest", digest)
	}

	if got := registry.lastScope.Load(); got != "repository:library/app:pull" {
		t.Errorf("token scope = %v", got)
	}

	if _, err := client.Resolve(context.Background(), registry.ref("1")); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}

	if n := registry.tokenRequests.Load(); n != 1 {
		t.Errorf("%d token requests; the token should have been reused", n)
	}
}

// Asked with only a platform manifest type, a multi-platform tag would answer
// with one platform's digest, which never equals the index digest Swarm pins.
func TestResolveAcceptsIndexesFirst(t *testing.T) {
	spec.Satisfies(t, "oci/distribution-spec/client-sends-accept")

	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.open = true })

	digest, err := registry.client().Resolve(context.Background(), registry.ref("1"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if digest != indexDigest {
		t.Errorf("digest = %q, want %q", digest, indexDigest)
	}

	accept, _ := registry.lastAccept.Load().(string)
	for _, mediaType := range []string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	} {
		if !strings.Contains(accept, mediaType) {
			t.Errorf("Accept %q does not name %s", accept, mediaType)
		}
	}
}

func TestResolveReportsAMissingTag(t *testing.T) {
	registry := newFakeRegistry(t, nil)
	ref := registry.ref("1")
	ref.Repository = "library/other"

	if _, err := registry.client().
		Resolve(context.Background(), ref); !errors.Is(
		err,
		ErrNotFound,
	) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestResolveReportsAPrivateImage(t *testing.T) {
	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.private = true })

	_, err := registry.client().Resolve(context.Background(), registry.ref("1"))
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestResolveRefusesAPlainTextRealm(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://auth.test/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	ref := Reference{Host: strings.TrimPrefix(server.URL, "https://"), Repository: "a", Tag: "1"}

	_, err := NewClient(server.Client()).Resolve(context.Background(), ref)
	if err == nil || errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v; a plain-text realm must not be asked for a token", err)
	}
}

func TestResolveTimesOut(t *testing.T) {
	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.hang = true })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := registry.client().Resolve(ctx, registry.ref("1")); err == nil {
		t.Error("Resolve returned no error from a registry that never answered")
	}
}

func TestResolveHashesTheBodyWithoutADigestHeader(t *testing.T) {
	const body = `{"schemaVersion":2}`

	sum := sha256.Sum256([]byte(body))
	want := "sha256:" + hex.EncodeToString(sum[:])

	registry := newFakeRegistry(t, func(r *fakeRegistry) {
		r.omitDigest, r.body = true, body
	})

	digest, err := registry.client().Resolve(context.Background(), registry.ref("1"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if digest != want {
		t.Errorf("digest = %q, want %q", digest, want)
	}
}

func TestResolveVerifiesTheDigestItUses(t *testing.T) {
	spec.Satisfies(t, "oci/distribution-spec/a-used-digest-matches-the-manifest")

	registry := newFakeRegistry(t, func(r *fakeRegistry) {
		r.omitDigest, r.body, r.statedOnGet = true, `{"schemaVersion":2}`, manifestDigest
	})

	if _, err := registry.client().Resolve(context.Background(), registry.ref("1")); err == nil {
		t.Error("a stated digest the body does not hash to was used")
	}
}
