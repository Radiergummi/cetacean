package attestations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radiergummi/cetacean/internal/imagecheck"
)

// fakeFetcher serves manifests and blobs from memory and counts each digest.
type fakeFetcher struct {
	mu        sync.Mutex
	manifests map[string]string // digest → body
	blobs     map[string][]byte
	failWith  map[string]error
	calls     map[string]int
	unbounded int           // fetches whose context had no deadline
	gate      chan struct{} // when set, every fetch waits for it to close
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{
		manifests: map[string]string{},
		blobs:     map[string][]byte{},
		failWith:  map[string]error{},
		calls:     map[string]int{},
	}
}

func (f *fakeFetcher) count(digest string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls[digest]
}

func (f *fakeFetcher) enter(ctx context.Context, digest string) error {
	if f.gate != nil {
		<-f.gate
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls[digest]++

	if _, ok := ctx.Deadline(); !ok {
		f.unbounded++
	}

	return f.failWith[digest]
}

func (f *fakeFetcher) Manifest(
	ctx context.Context,
	_ imagecheck.Reference,
	digest string,
) ([]byte, string, error) {
	if err := f.enter(ctx, digest); err != nil {
		return nil, "", err
	}

	body, ok := f.manifests[digest]
	if !ok {
		return nil, "", imagecheck.ErrNotFound
	}

	mediaType := ociManifestType
	if strings.Contains(body, `"manifests"`) {
		mediaType = ociIndexType
	}

	return []byte(body), mediaType, nil
}

func (f *fakeFetcher) Blob(
	ctx context.Context,
	_ imagecheck.Reference,
	digest string,
	limit int64,
) ([]byte, error) {
	if err := f.enter(ctx, digest); err != nil {
		return nil, err
	}

	body, ok := f.blobs[digest]
	if !ok {
		return nil, imagecheck.ErrNotFound
	}

	if int64(len(body)) > limit {
		return nil, imagecheck.ErrTooLarge
	}

	return body, nil
}

// withProbe loads the probe layout into f, and returns the image to ask for.
func withProbe(t *testing.T, f *fakeFetcher) string {
	t.Helper()

	for _, d := range []string{probeIndex, probeManifest} {
		f.manifests[d] = string(probeBlob(t, d))
	}

	f.manifests[probeAttestation] = string(probeBlob(t, probeAttestation))
	f.blobs[probeProvenance] = probeBlob(t, probeProvenance)

	return "registry.test/probe:1@" + probeIndex
}

var (
	syntheticAMD         = digestFor("amd")
	syntheticARM         = digestFor("arm")
	syntheticAttestation = digestFor("att")
	syntheticSBOM        = digestFor("sbom")
	syntheticProvenance  = digestFor("prov")
)

const (
	probeAttestation = "sha256:fbec505132e151e9e2e685331ff075fa55edc4e2915b600a06c436cdc2194df6"
	syntheticIndex   = "sha256:" + "9999999999999999999999999999999999999999999999999999999999999999"
	syntheticImage   = "registry.test/synthetic@" + syntheticIndex
)

// withSynthetic is an amd64 and an arm64 platform whose amd64 attestation
// carries an SBOM of sbomSize bytes, served from the syft fixture.
func withSynthetic(t *testing.T, f *fakeFetcher, sbomSize int64) {
	t.Helper()

	f.manifests[syntheticIndex] = `{"mediaType":"` + ociIndexType + `","manifests":[
		{"digest":"` + syntheticAMD + `","platform":{"os":"linux","architecture":"amd64"}},
		{"digest":"` + syntheticARM + `","platform":{"os":"linux","architecture":"arm64"}},
		{"digest":"` + syntheticAttestation + `","platform":{"os":"unknown","architecture":"unknown"},
		 "annotations":{"vnd.docker.reference.type":"attestation-manifest",
		                "vnd.docker.reference.digest":"` + syntheticAMD + `"}}]}`
	f.manifests[syntheticAttestation] = fmt.Sprintf(`{"layers":[
		{"digest":"%s","size":%d,
		 "annotations":{"in-toto.io/predicate-type":"https://spdx.dev/Document"}},
		{"digest":"%s","size":10,
		 "annotations":{"in-toto.io/predicate-type":"https://slsa.dev/provenance/v1"}}]}`,
		syntheticSBOM, sbomSize, syntheticProvenance)
	f.blobs[syntheticSBOM] = fixture(t, "syft-sbom.json")
	f.blobs[syntheticProvenance] = fixture(t, "syft-provenance.json")
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestStore(f *fakeFetcher) (*Store, *clock) {
	c := &clock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	s := NewStore(f)
	s.now = c.now

	return s, c
}

func TestImageUnpinned(t *testing.T) {
	f := newFakeFetcher()
	s, _ := newTestStore(f)

	if got := s.Image(context.Background(), "nginx:1"); got.Status != StatusUnpinned {
		t.Errorf("status = %s", got.Status)
	}

	if len(f.calls) != 0 {
		t.Errorf("fetched %v for an unpinned image", f.calls)
	}
}

func TestImageDigestOnlyIsPinned(t *testing.T) {
	f := newFakeFetcher()
	withProbe(t, f)
	s, _ := newTestStore(f)

	got := s.Image(context.Background(), "registry.test/probe@"+probeIndex)
	if got.Status != StatusAvailable || len(got.Platforms) != 1 {
		t.Fatalf("got %+v", got)
	}

	p := got.Platforms[0]
	if p.Provenance == nil || p.Provenance.Source == nil ||
		p.Provenance.Source.Commit != probeRevision {
		t.Errorf("provenance = %+v", p.Provenance)
	}
}

func TestImageNoneIsCachedForever(t *testing.T) {
	f := newFakeFetcher()
	withProbe(t, f)
	s, c := newTestStore(f)

	// The platform manifest asked for directly is not an index.
	image := "registry.test/probe@" + probeManifest
	if got := s.Image(context.Background(), image); got.Status != StatusNone {
		t.Fatalf("status = %s", got.Status)
	}

	c.advance(30 * 24 * time.Hour)
	s.Image(context.Background(), image)

	if n := f.count(probeManifest); n != 1 {
		t.Errorf("%d fetches; a content answer never expires", n)
	}
}

func TestImagePrivateRetriesAfterAnHour(t *testing.T) {
	f := newFakeFetcher()
	f.failWith[syntheticIndex] = imagecheck.ErrUnauthorized
	s, c := newTestStore(f)

	if got := s.Image(context.Background(), syntheticImage); got.Status != StatusPrivate {
		t.Fatalf("status = %s", got.Status)
	}

	c.advance(59 * time.Minute)
	s.Image(context.Background(), syntheticImage)

	if n := f.count(syntheticIndex); n != 1 {
		t.Errorf("%d fetches before the hour was up", n)
	}

	c.advance(2 * time.Minute)
	s.Image(context.Background(), syntheticImage)

	if n := f.count(syntheticIndex); n != 2 {
		t.Errorf("%d fetches after the hour", n)
	}
}

func TestImageMissing(t *testing.T) {
	f := newFakeFetcher()
	s, c := newTestStore(f)

	if got := s.Image(context.Background(), syntheticImage); got.Status != StatusMissing {
		t.Fatalf("status = %s", got.Status)
	}

	c.advance(61 * time.Minute)
	s.Image(context.Background(), syntheticImage)

	if n := f.count(syntheticIndex); n != 2 {
		t.Errorf("%d fetches; missing is retried hourly", n)
	}
}

func TestImageUnavailableRetriesAfterAMinute(t *testing.T) {
	f := newFakeFetcher()
	f.failWith[syntheticIndex] = errors.New("registry answered 503")
	s, c := newTestStore(f)

	got := s.Image(context.Background(), syntheticImage)
	if got.Status != StatusUnavailable || !got.RetryAfter.Equal(c.now().Add(time.Minute)) {
		t.Fatalf("got %+v", got)
	}

	c.advance(61 * time.Second)
	s.Image(context.Background(), syntheticImage)

	if n := f.count(syntheticIndex); n != 2 {
		t.Errorf("%d fetches; unavailable is retried after a minute", n)
	}
}

func TestImageConcurrentViewersFetchOnce(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	f.gate = make(chan struct{})
	s, _ := newTestStore(f)

	var wg sync.WaitGroup

	var available atomic.Int64

	for range 10 {
		wg.Go(func() {
			if s.Image(context.Background(), syntheticImage).Status == StatusAvailable {
				available.Add(1)
			}
		})
	}

	time.Sleep(50 * time.Millisecond)
	close(f.gate)
	wg.Wait()

	if n := f.count(syntheticIndex); n != 1 {
		t.Errorf("%d index fetches for ten viewers", n)
	}

	if available.Load() != 10 {
		t.Errorf("%d of 10 viewers got the attestations", available.Load())
	}
}

func TestImageCancelledCallerDoesNotPoison(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	f.gate = make(chan struct{})
	s, _ := newTestStore(f)

	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan Image)

	go func() { first <- s.Image(ctx, syntheticImage) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-first
	close(f.gate)

	if got := s.Image(context.Background(), syntheticImage); got.Status != StatusAvailable {
		t.Errorf("status = %s after another caller gave up", got.Status)
	}
}

func TestSBOM(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	s, _ := newTestStore(f)

	sbom, raw, err := s.SBOM(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "amd64"})
	if err != nil || len(sbom.Packages) != 6 || len(raw) == 0 {
		t.Fatalf("SBOM = %v, %d bytes, %v", sbom, len(raw), err)
	}

	_, _, err = s.SBOM(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "arm64"})
	if !errors.Is(err, ErrNoSBOM) {
		t.Errorf("arm64: err = %v, want ErrNoSBOM", err)
	}
}

func TestSBOMTooLargeNoDownload(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 33<<20)
	s, _ := newTestStore(f)

	img := s.Image(context.Background(), syntheticImage)
	if !img.Platforms[0].SBOMTooLarge {
		t.Errorf("platform = %+v", img.Platforms[0])
	}

	_, _, err := s.SBOM(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "amd64"})
	if !errors.Is(err, imagecheck.ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}

	if n := f.count(syntheticSBOM); n != 0 {
		t.Errorf("an SBOM over the cap was downloaded %d times", n)
	}
}

func TestSBOMUnknownPlatform(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	s, _ := newTestStore(f)

	_, _, err := s.SBOM(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "s390x"})
	if !errors.Is(err, ErrUnknownPlatform) {
		t.Errorf("err = %v, want ErrUnknownPlatform", err)
	}
}

func TestSBOMOfAnImageWithoutAttestations(t *testing.T) {
	f := newFakeFetcher()
	f.failWith[syntheticIndex] = imagecheck.ErrUnauthorized
	s, _ := newTestStore(f)

	_, _, err := s.SBOM(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "amd64"})

	var status *ImageStatusError
	if !errors.As(err, &status) || status.Status != StatusPrivate {
		t.Errorf("err = %v, want the image's status", err)
	}

	f.failWith[syntheticIndex] = errors.New("503")
	s, _ = newTestStore(f)

	_, _, err = s.SBOM(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "amd64"})

	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || unavailable.RetryAfter.IsZero() {
		t.Errorf("err = %v, want UnavailableError", err)
	}
}

func TestDockerfileProbe(t *testing.T) {
	f := newFakeFetcher()
	image := withProbe(t, f)
	s, _ := newTestStore(f)

	d, err := s.Dockerfile(
		context.Background(),
		image,
		Platform{OS: "linux", Architecture: "arm64"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(d.Unattributed) != 0 || d.BaseSize != 4092319 {
		t.Errorf("unattributed %v, base %d", d.Unattributed, d.BaseSize)
	}

	if _, err := s.Dockerfile(context.Background(), image,
		Platform{OS: "linux", Architecture: "arm64"}); err != nil {
		t.Fatal(err)
	}

	if n := f.count(probeManifest); n != 1 {
		t.Errorf("the platform manifest was fetched %d times", n)
	}
}

func TestDockerfileWithoutSource(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	s, _ := newTestStore(f)

	_, err := s.Dockerfile(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "arm64"})
	if !errors.Is(err, ErrNoSource) {
		t.Errorf("arm64 has no provenance: err = %v, want ErrNoSource", err)
	}
}

func TestEvictsByBytes(t *testing.T) {
	f := newFakeFetcher()
	withProbe(t, f)
	withSynthetic(t, f, 100)
	s, _ := newTestStore(f)
	s.manifests.budget = 1 << 10

	s.Image(context.Background(), syntheticImage)
	s.Image(context.Background(), "registry.test/probe@"+probeIndex)
	s.Image(context.Background(), syntheticImage)

	if n := f.count(syntheticIndex); n != 2 {
		t.Errorf("%d fetches; the first image should have been evicted", n)
	}
}

// A multi-platform index pushed without attestations has nothing to show on
// any platform; that is "none", not a platform picker full of blanks.
func TestImageIndexWithoutAttestationsIsNone(t *testing.T) {
	f := newFakeFetcher()
	f.manifests[syntheticIndex] = `{"mediaType":"` + ociIndexType + `","manifests":[
		{"digest":"` + syntheticAMD + `","platform":{"os":"linux","architecture":"amd64"}},
		{"digest":"` + syntheticARM + `","platform":{"os":"linux","architecture":"arm64"}}]}`
	s, _ := newTestStore(f)

	if got := s.Image(context.Background(), syntheticImage); got.Status != StatusNone {
		t.Errorf("status = %s, want none", got.Status)
	}
}

// A refusal is about a repository, not a digest: the same image mirrored
// publicly elsewhere must not inherit a private copy's answer.
func TestRefusalIsCachedPerRepository(t *testing.T) {
	f := newFakeFetcher()
	f.failWith[syntheticIndex] = imagecheck.ErrUnauthorized
	s, _ := newTestStore(f)

	s.Image(context.Background(), "private.test/app@"+syntheticIndex)
	s.Image(context.Background(), "public.test/app@"+syntheticIndex)

	if n := f.count(syntheticIndex); n != 2 {
		t.Errorf("%d fetches; the second repository was answered from the first's refusal", n)
	}
}

func TestNilStoreIsDisabled(t *testing.T) {
	var s *Store

	if got := s.Image(context.Background(), syntheticImage); got.Status != StatusDisabled {
		t.Errorf("status = %s", got.Status)
	}
}

// The client has no timeout of its own, so the store bounds every fetch.
func TestEveryFetchHasADeadline(t *testing.T) {
	f := newFakeFetcher()
	image := withProbe(t, f)
	withSynthetic(t, f, 100)
	s, _ := newTestStore(f)
	ctx := context.Background()
	amd64 := Platform{OS: "linux", Architecture: "amd64"}

	if _, _, err := s.SBOM(ctx, syntheticImage, amd64); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Dockerfile(
		ctx,
		image,
		Platform{OS: "linux", Architecture: "arm64"},
	); err != nil {
		t.Fatal(err)
	}

	if f.unbounded != 0 {
		t.Errorf("%d fetches without a deadline", f.unbounded)
	}
}

// An SBOM that does not parse never will; it is not the registry's fault,
// and it is not downloaded again.
func TestSBOMUnreadableIsCached(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	f.blobs[syntheticSBOM] = []byte(`{"predicateType":"https://spdx.dev/Document","predicate":[]}`)
	s, c := newTestStore(f)
	amd64 := Platform{OS: "linux", Architecture: "amd64"}

	for range 2 {
		_, _, err := s.SBOM(context.Background(), syntheticImage, amd64)

		var unavailable *UnavailableError
		if !errors.Is(err, ErrUnreadable) || errors.As(err, &unavailable) {
			t.Fatalf("err = %v, want ErrUnreadable", err)
		}

		c.advance(time.Hour)
	}

	if n := f.count(syntheticSBOM); n != 1 {
		t.Errorf("an unreadable SBOM was downloaded %d times", n)
	}
}

func TestSBOMLargerThanItsDescriptorIsCached(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	f.failWith[syntheticSBOM] = imagecheck.ErrTooLarge
	s, _ := newTestStore(f)
	amd64 := Platform{OS: "linux", Architecture: "amd64"}

	for range 2 {
		_, _, err := s.SBOM(context.Background(), syntheticImage, amd64)

		var tooLarge *TooLargeError
		if !errors.As(err, &tooLarge) || tooLarge.Error() != "the SBOM is larger than 32 MiB" {
			t.Fatalf("err = %v, want TooLargeError", err)
		}
	}

	if n := f.count(syntheticSBOM); n != 1 {
		t.Errorf("an SBOM over the cap was downloaded %d times", n)
	}
}

func TestDockerfileManifestTooLarge(t *testing.T) {
	f := newFakeFetcher()
	image := withProbe(t, f)
	f.failWith[probeManifest] = imagecheck.ErrTooLarge
	s, _ := newTestStore(f)

	_, err := s.Dockerfile(
		context.Background(),
		image,
		Platform{OS: "linux", Architecture: "arm64"},
	)

	var tooLarge *TooLargeError
	if !errors.As(err, &tooLarge) ||
		tooLarge.Error() != "the platform manifest is larger than 4 MiB" {
		t.Errorf("err = %v, want the manifest named", err)
	}
}

// Re-reading an image costs manifest pulls, a blob costs none, so however
// large an SBOM is it must not push images out.
func TestBlobsDoNotEvictImages(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)
	s, _ := newTestStore(f)
	s.blobs.budget = 1

	s.Image(context.Background(), syntheticImage)

	if _, _, err := s.SBOM(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "amd64"}); err != nil {
		t.Fatal(err)
	}

	s.Image(context.Background(), syntheticImage)

	if n := f.count(syntheticIndex); n != 1 {
		t.Errorf("%d index fetches; the SBOM evicted the image", n)
	}
}

func TestOnePlatformsProvenanceFailing(t *testing.T) {
	f := newFakeFetcher()
	withSynthetic(t, f, 100)

	armAttestation, armProvenance := digestFor("arm-att"), digestFor("arm-prov")
	f.manifests[syntheticIndex] = strings.Replace(f.manifests[syntheticIndex], "]}", `,
		{"digest":"`+armAttestation+`","platform":{"os":"unknown","architecture":"unknown"},
		 "annotations":{"vnd.docker.reference.type":"attestation-manifest",
		                "vnd.docker.reference.digest":"`+syntheticARM+`"}}]}`, 1)
	f.manifests[armAttestation] = `{"layers":[{"digest":"` + armProvenance + `","size":10,
		"annotations":{"in-toto.io/predicate-type":"https://slsa.dev/provenance/v1"}}]}`
	f.failWith[armProvenance] = errors.New("registry answered 503")
	s, c := newTestStore(f)

	got := s.Image(context.Background(), syntheticImage)
	if got.Status != StatusAvailable || len(got.Platforms) != 2 {
		t.Fatalf("got %+v", got)
	}

	if amd := got.Platforms[0]; amd.Provenance == nil || amd.ProvenanceStatus != "" {
		t.Errorf("amd64 = %+v", amd)
	}

	if arm := got.Platforms[1]; arm.Provenance != nil || arm.ProvenanceStatus != StatusUnavailable {
		t.Errorf("arm64 = %+v", arm)
	}

	if !got.RetryAfter.Equal(c.now().Add(time.Minute)) {
		t.Errorf("retryAfter = %v", got.RetryAfter)
	}

	_, err := s.Dockerfile(context.Background(), syntheticImage,
		Platform{OS: "linux", Architecture: "arm64"})

	if _, ok := errors.AsType[*UnavailableError](err); !ok {
		t.Errorf("arm64 Dockerfile: err = %v, want UnavailableError", err)
	}

	c.advance(61 * time.Second)
	s.Image(context.Background(), syntheticImage)

	if n := f.count(armProvenance); n != 2 {
		t.Errorf("%d fetches; a failed platform is retried after a minute", n)
	}
}
