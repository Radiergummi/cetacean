package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/swarm"
	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/acl"
	"github.com/radiergummi/cetacean/internal/attestations"
	"github.com/radiergummi/cetacean/internal/cache"
)

const attestedImage = "app:1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var (
	attestedAMD64 = attestations.Platform{OS: "linux", Architecture: "amd64"}
	attestedARM64 = attestations.Platform{OS: "linux", Architecture: "arm64"}
)

// fakeAttestationSource answers for every image with the same platforms.
type fakeAttestationSource struct {
	image      attestations.Image
	sbomErr    error
	calls      int
	dockerfile *attestations.Dockerfile
	dfErr      error
}

func (f *fakeAttestationSource) Image(context.Context, string) attestations.Image {
	f.calls++

	return f.image
}

func (f *fakeAttestationSource) SBOM(
	_ context.Context,
	_ string,
	p attestations.Platform,
) (*attestations.SBOM, []byte, error) {
	f.calls++

	if f.sbomErr != nil {
		return nil, nil, f.sbomErr
	}

	return &attestations.SBOM{
		Generators: []string{"Tool: syft"},
		Packages: []attestations.Package{{
			Name: "pkg-" + p.Architecture, Version: "1.0", Ecosystem: "golang",
			PURL: "pkg:golang/pkg@1.0", FoundIn: "go module information: /bin/app",
		}},
		Ecosystems: map[string]int{"golang": 1},
	}, []byte(`{"spdxVersion":"SPDX-2.3","name":"` + p.Architecture + `"}`), nil
}

func (f *fakeAttestationSource) Dockerfile(
	context.Context,
	string,
	attestations.Platform,
) (*attestations.Dockerfile, error) {
	f.calls++

	return f.dockerfile, f.dfErr
}

func twoPlatformImage() attestations.Image {
	return attestations.Image{
		Status: attestations.StatusAvailable,
		Platforms: []attestations.ImagePlatform{
			{
				Platform:       attestedAMD64,
				ManifestDigest: "sha256:amd",
				SBOM:           &attestations.Descriptor{Digest: "sha256:s1", Size: 10},
			},
			{
				Platform:       attestedARM64,
				ManifestDigest: "sha256:arm",
				SBOM:           &attestations.Descriptor{Digest: "sha256:s2", Size: 10},
			},
		},
	}
}

func withAttestations(src *fakeAttestationSource) testHandlersOption {
	return func(cfg *testHandlersConfig) { cfg.attestations = src }
}

// attestedCluster is one service whose one task runs on an aarch64 node.
func attestedCluster() *cache.Cache {
	c := cache.New(nil)
	spec := swarm.TaskSpec{ContainerSpec: &swarm.ContainerSpec{Image: attestedImage}}

	c.SetService(swarm.Service{
		ID:   "svc1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "web"}, TaskTemplate: spec},
	})
	c.SetNode(swarm.Node{
		ID: "node1",
		Description: swarm.NodeDescription{
			Platform: swarm.Platform{OS: "linux", Architecture: "aarch64"},
		},
	})
	c.SetTask(swarm.Task{
		ID: "task1", ServiceID: "svc1", NodeID: "node1", Spec: spec,
		Status: swarm.TaskStatus{State: swarm.TaskStateRunning},
	})

	return c
}

func getAttested(t *testing.T, h http.Handler, path, accept string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	return w
}

func problemCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()

	var p struct {
		Type string `json:"type"`
	}

	_ = json.Unmarshal(w.Body.Bytes(), &p)

	return p.Type[strings.LastIndexByte(p.Type, '/')+1:]
}

func TestProvenanceRefusesHTML(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	if w := getAttested(
		t,
		h,
		"/services/svc1/provenance",
		"text/html",
	); w.Code != http.StatusNotAcceptable {
		t.Errorf("status = %d, want 406", w.Code)
	}
}

func TestProvenanceBody(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/services/svc1/provenance", "application/json")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}

	var body struct {
		Type      string `json:"@type"`
		ID        string `json:"@id"`
		Status    string `json:"status"`
		Platforms []struct {
			Platform     string `json:"platform"`
			RunningTasks int    `json:"runningTasks"`
		} `json:"platforms"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body.Type != "ImageProvenance" || !strings.HasSuffix(body.ID, "/services/svc1/provenance") ||
		body.Status != "available" || len(body.Platforms) != 2 || body.Platforms[1].RunningTasks != 1 {
		t.Errorf("body = %+v", body)
	}

	if got := w.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q", got)
	}
}

func TestProvenanceACLBeforeFetch(t *testing.T) {
	e := acl.NewEvaluator()
	e.SetPolicy(&acl.Policy{Grants: []acl.Grant{
		{
			Resources: []string{
				"service:other",
			},
			Audience:    []string{"*"},
			Permissions: []string{"read"},
		},
	}})

	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src), withACL(e))

	for _, path := range []string{
		"/services/svc1/provenance", "/services/svc1/sbom", "/services/svc1/dockerfile",
		"/tasks/task1/provenance", "/tasks/task1/sbom",
	} {
		if w := getAttested(t, h, path, "application/json"); w.Code == http.StatusOK {
			t.Errorf("%s: a caller without read access got 200", path)
		}
	}

	if src.calls != 0 {
		t.Errorf("the source was asked %d times for resources the caller cannot read", src.calls)
	}
}

func TestProvenanceStrongETag(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/tasks/task1/provenance", "application/json")
	etag := w.Header().Get("ETag")

	if etag == "" || strings.HasPrefix(etag, "W/") {
		t.Fatalf("ETag = %q, want a strong one", etag)
	}

	req := httptest.NewRequest(http.MethodGet, "/tasks/task1/provenance", nil)
	req.Header.Set("If-None-Match", etag)

	again := httptest.NewRecorder()
	h.ServeHTTP(again, req)

	if again.Code != http.StatusNotModified {
		t.Errorf("status = %d, want 304", again.Code)
	}
}

func TestSBOMCSV(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/services/svc1/sbom.csv?platform=linux/amd64", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}

	header, _, _ := strings.Cut(w.Body.String(), "\r\n")
	if header != "name,version,ecosystem,license,purl,found_in" {
		t.Errorf("header = %q", header)
	}
}

func TestSBOMSPDX(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	want := `{"spdxVersion":"SPDX-2.3","name":"amd64"}`

	byAccept := getAttested(
		t,
		h,
		"/services/svc1/sbom?platform=linux/amd64",
		"application/spdx+json",
	)
	bySuffix := getAttested(t, h, "/services/svc1/sbom.spdx?platform=linux/amd64", "")

	for name, w := range map[string]*httptest.ResponseRecorder{"accept": byAccept, "suffix": bySuffix} {
		if w.Code != http.StatusOK || w.Body.String() != want ||
			!strings.HasPrefix(w.Header().Get("Content-Type"), "application/spdx+json") {
			t.Errorf("%s: %d %q %q", name, w.Code, w.Header().Get("Content-Type"), w.Body)
		}
	}
}

func TestSBOMPlatformRequired(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/services/svc1/sbom", "application/json")
	if w.Code != http.StatusBadRequest || problemCode(t, w) != "ATT003" {
		t.Errorf("status %d, code %s", w.Code, problemCode(t, w))
	}

	if !strings.Contains(w.Body.String(), "linux/arm64") {
		t.Errorf("the problem does not name the candidates: %s", w.Body)
	}
}

func TestSBOMErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{attestations.ErrNoSBOM, http.StatusNotFound, "ATT002"},
		{
			&attestations.TooLargeError{Content: "SBOM", Limit: 32 << 20},
			http.StatusNotFound,
			"ATT005",
		},
		{
			fmt.Errorf("%w: SPDX document: bad", attestations.ErrUnreadable),
			http.StatusBadGateway,
			"ATT008",
		},
		{
			// Retry-After is defined for 503 (RFC 9110 §10.2.3), not for 502.
			&attestations.UnavailableError{RetryAfter: time.Now().Add(time.Minute)},
			http.StatusServiceUnavailable,
			"ATT001",
		},
		{
			&attestations.ImageStatusError{Status: attestations.StatusPrivate},
			http.StatusNotFound,
			"ATT007",
		},
	}

	for _, tc := range cases {
		src := &fakeAttestationSource{image: twoPlatformImage(), sbomErr: tc.err}
		h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

		w := getAttested(t, h, "/services/svc1/sbom?platform=linux/amd64", "application/json")
		if w.Code != tc.status || problemCode(t, w) != tc.code {
			t.Errorf("%v: %d %s, want %d %s", tc.err, w.Code, problemCode(t, w), tc.status, tc.code)
		}

		if tc.code == "ATT001" && w.Header().Get("Retry-After") == "" {
			t.Error("ATT001 without Retry-After")
		}
	}
}

// A service's image changes with every deploy, so its SBOM URL is revalidated;
// a task's never does, so a browser may keep it.
func TestSBOMCachingFollowsTheImage(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	service := getAttested(t, h, "/services/svc1/sbom?platform=linux/amd64", "application/json")
	if got := service.Header().Get("Cache-Control"); strings.Contains(got, "max-age") {
		t.Errorf("service Cache-Control = %q; it must revalidate", got)
	}

	task := getAttested(t, h, "/tasks/task1/sbom", "application/json")
	if got := task.Header().Get("Cache-Control"); !strings.Contains(got, "max-age=3600") {
		t.Errorf("task Cache-Control = %q", got)
	}
}

func TestDockerfilePlain(t *testing.T) {
	src := &fakeAttestationSource{
		image:      twoPlatformImage(),
		dockerfile: &attestations.Dockerfile{Lines: []string{"FROM alpine", "RUN true"}},
	}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/tasks/task1/dockerfile.txt", "")
	if w.Code != http.StatusOK || w.Body.String() != "FROM alpine\nRUN true\n" ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("%d %q %q", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
}

func TestDockerfileNoSource(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage(), dfErr: attestations.ErrNoSource}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/services/svc1/dockerfile?platform=linux/amd64", "application/json")
	if w.Code != http.StatusNotFound || problemCode(t, w) != "ATT006" {
		t.Errorf("%d %s", w.Code, problemCode(t, w))
	}
}

// The Dockerfile view reads a manifest, not the SBOM; its size problem says so.
func TestDockerfileManifestTooLarge(t *testing.T) {
	src := &fakeAttestationSource{
		image: twoPlatformImage(),
		dfErr: &attestations.TooLargeError{Content: "platform manifest", Limit: 4 << 20},
	}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/services/svc1/dockerfile?platform=linux/amd64", "application/json")
	if w.Code != http.StatusNotFound || problemCode(t, w) != "ATT005" ||
		!strings.Contains(w.Body.String(), "the platform manifest is larger than 4 MiB") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestTaskSBOMUsesNodePlatform(t *testing.T) {
	src := &fakeAttestationSource{image: twoPlatformImage()}
	h := newTestRouterWithCache(t, attestedCluster(), withAttestations(src))

	w := getAttested(t, h, "/tasks/task1/sbom", "application/json")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pkg-arm64") {
		t.Errorf("%d %s", w.Code, w.Body)
	}

	w = getAttested(t, h, "/tasks/task1/sbom?platform=linux/amd64", "application/json")
	if problemCode(t, w) != "ATT004" {
		t.Errorf("a platform the node cannot run: %d %s", w.Code, problemCode(t, w))
	}
}

// text/plain is known now, for the Dockerfile; an endpoint that does not serve
// it still refuses it.
func TestPlainTextIsRefusedWhereNotServed(t *testing.T) {
	h := newTestRouterWithCache(t, attestedCluster())

	if w := getAttested(t, h, "/services", "text/plain"); w.Code != http.StatusNotAcceptable {
		t.Errorf("status = %d, want 406", w.Code)
	}
}

func TestSuffixesStayIdentifiersElsewhere(t *testing.T) {
	c := attestedCluster()
	h := newTestRouterWithCache(t, c)

	// A volume named backup.txt is that volume, not a text representation.
	if w := getAttested(
		t,
		h,
		"/volumes/backup.txt",
		"application/json",
	); w.Code == http.StatusNotAcceptable {
		t.Errorf("a .txt volume name was read as a suffix: %d", w.Code)
	}
}
