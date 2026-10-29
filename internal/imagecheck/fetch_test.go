package imagecheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radiergummi/cetacean/internal/spec"
)

func (r *fakeRegistry) serveBlob(w http.ResponseWriter, req *http.Request, digest string) {
	if r.blobsAt != "" {
		http.Redirect(w, req, r.blobsAt+"/cdn/blob", http.StatusTemporaryRedirect)

		return
	}

	body, ok := r.blobs[digest]
	if !ok {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	_, _ = w.Write([]byte(body))
}

func digestOf(body string) string {
	sum := sha256.Sum256([]byte(body))

	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestManifestReturnsTheBodyAndItsMediaType(t *testing.T) {
	spec.Satisfies(t, "oci/distribution-spec/a-digest-reference-is-verified")

	const body = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json"}`

	registry := newFakeRegistry(t, func(r *fakeRegistry) {
		r.body, r.contentType = body, "application/vnd.oci.image.index.v1+json"
	})

	got, mediaType, err := registry.client().
		Manifest(context.Background(), registry.ref(""), digestOf(body))
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}

	if string(got) != body {
		t.Errorf("body = %q", got)
	}

	if mediaType != "application/vnd.oci.image.index.v1+json" {
		t.Errorf("mediaType = %q", mediaType)
	}

	wrong := digestOf("something else")

	_, _, err = registry.client().Manifest(context.Background(), registry.ref(""), wrong)
	if err == nil || !strings.Contains(err.Error(), wrong) ||
		!strings.Contains(err.Error(), digestOf(body)) {
		t.Errorf("err = %v; want both digests named", err)
	}
}

func TestBlobVerifiesTheRequestedDigest(t *testing.T) {
	spec.Satisfies(t, "oci/distribution-spec/client-verifies-blob-digest")

	wanted := digestOf("the real blob")
	registry := newFakeRegistry(t, func(r *fakeRegistry) {
		r.blobs = map[string]string{
			wanted:                "a different blob",
			digestOf("the right"): "the right",
		}
	})

	if _, err := registry.client().
		Blob(context.Background(), registry.ref(""), wanted, 1<<10); err == nil {
		t.Error("a blob that does not hash to its digest was returned")
	}

	got, err := registry.client().
		Blob(context.Background(), registry.ref(""), digestOf("the right"), 1<<10)
	if err != nil || string(got) != "the right" {
		t.Errorf("Blob = %q, %v", got, err)
	}
}

func TestBlobRefusesMoreThanTheLimit(t *testing.T) {
	body := strings.Repeat("x", 2<<10)
	registry := newFakeRegistry(t, func(r *fakeRegistry) {
		r.blobs = map[string]string{digestOf(body): body}
	})

	_, err := registry.client().Blob(context.Background(), registry.ref(""), digestOf(body), 1<<10)
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

// Docker Hub answers a blob GET with a redirect to its CDN; the pull token is
// for the registry, and must not travel to another host with it.
func TestBlobFollowsARedirectWithoutTheToken(t *testing.T) {
	const body = "layer bytes"

	var authorization string

	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorization = req.Header.Get("Authorization")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(cdn.Close)

	registry := newFakeRegistry(t, func(r *fakeRegistry) { r.blobsAt = cdn.URL })

	got, err := registry.client().
		Blob(context.Background(), registry.ref(""), digestOf(body), 1<<10)
	if err != nil || string(got) != body {
		t.Fatalf("Blob = %q, %v", got, err)
	}

	if authorization != "" {
		t.Errorf("the CDN was sent Authorization %q", authorization)
	}
}

func TestBlobReportsMissingAndPrivate(t *testing.T) {
	registry := newFakeRegistry(t, nil)

	_, err := registry.client().Blob(context.Background(), registry.ref(""), digestA, 1<<10)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: err = %v, want ErrNotFound", err)
	}

	private := newFakeRegistry(t, func(r *fakeRegistry) { r.private = true })

	_, err = private.client().Blob(context.Background(), private.ref(""), digestA, 1<<10)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("private: err = %v, want ErrUnauthorized", err)
	}
}

// A digest read from an index is untrusted, and must not reach the URL.
func TestFetchRefusesWhatIsNotADigest(t *testing.T) {
	registry := newFakeRegistry(t, nil)

	for _, digest := range []string{"sha256:../../other/manifests/latest", "sha256:AB", ""} {
		if _, _, err := registry.client().
			Manifest(context.Background(), registry.ref(""), digest); err == nil {
			t.Errorf("manifest %q was fetched", digest)
		}

		if _, err := registry.client().
			Blob(context.Background(), registry.ref(""), digest, 1<<10); err == nil {
			t.Errorf("blob %q was fetched", digest)
		}
	}

	if n := registry.manifestRequests.Load(); n != 0 {
		t.Errorf("%d requests reached the registry", n)
	}
}
