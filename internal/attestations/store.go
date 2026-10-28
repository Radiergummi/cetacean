package attestations

import (
	"container/list"
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/radiergummi/cetacean/internal/imagecheck"
)

const (
	cacheBudget       = 64 << 20
	maxProvenanceSize = 4 << 20
	maxSBOMSize       = 32 << 20
	operationTimeout  = 30 * time.Second

	// A repository can become public, and a deleted image be pushed again.
	refusedRetry = time.Hour
	failedRetry  = time.Minute
)

var (
	ErrNoSBOM          = errors.New("no SBOM for this platform")
	ErrUnknownPlatform = errors.New("platform not in the image")
)

// UnavailableError is a registry that could not be asked, until RetryAfter.
type UnavailableError struct{ RetryAfter time.Time }

func (e *UnavailableError) Error() string { return "registry unavailable" }

// ImageStatusError is an image whose attestations cannot be read at all.
type ImageStatusError struct{ Status Status }

func (e *ImageStatusError) Error() string { return "image attestations " + string(e.Status) }

// Fetcher reads manifests and blobs by digest; *imagecheck.Client is one.
type Fetcher interface {
	Manifest(ctx context.Context, ref imagecheck.Reference, digest string) ([]byte, string, error)
	Blob(ctx context.Context, ref imagecheck.Reference, digest string, limit int64) ([]byte, error)
}

// Image is what an image's registry says about its attestations.
type Image struct {
	Status     Status
	RetryAfter time.Time
	Platforms  []ImagePlatform
}

// ImagePlatform is one platform of an image with its provenance parsed.
type ImagePlatform struct {
	Platform       Platform
	ManifestDigest string
	Provenance     *Provenance
	SBOM           *Descriptor
	SBOMTooLarge   bool
}

// Err is the image's status as an error when its attestations cannot be read.
func (img Image) Err() error {
	switch img.Status {
	case StatusAvailable:
		return nil
	case StatusUnavailable:
		return &UnavailableError{RetryAfter: img.RetryAfter}
	default:
		return &ImageStatusError{img.Status}
	}
}

// Store fetches attestations on demand and keeps them by digest. Content
// never changes under a digest, so answers about it never expire; failures
// do. A nil Store is the feature switched off.
type Store struct {
	fetch  Fetcher
	now    func() time.Time
	budget int64
	flight singleflight.Group

	mu      sync.Mutex
	lru     *list.List
	entries map[string]*list.Element
	used    int64
}

type cached struct {
	key     string
	value   any
	cost    int64
	expires time.Time // zero for content, which never expires
}

// imageEntry keeps the provenance blobs beside the parsed image, by platform
// manifest digest, for the Dockerfile view to join.
type imageEntry struct {
	image      Image
	provenance map[string][]byte
}

type sbomEntry struct {
	sbom *SBOM
	raw  []byte
}

func NewStore(f Fetcher) *Store {
	return &Store{
		fetch:   f,
		now:     time.Now,
		budget:  cacheBudget,
		lru:     list.New(),
		entries: map[string]*list.Element{},
	}
}

// Image reports the attestations of image, fetching what is not cached.
func (s *Store) Image(ctx context.Context, image string) Image {
	if s == nil {
		return Image{Status: StatusDisabled}
	}

	entry, _ := s.lookup(ctx, image)

	return entry.image
}

// SBOM returns the package table and the SPDX document of one platform.
func (s *Store) SBOM(ctx context.Context, image string, p Platform) (*SBOM, []byte, error) {
	entry, ref, err := s.available(ctx, image)
	if err != nil {
		return nil, nil, err
	}

	platform, err := find(entry.image, p)
	if err != nil {
		return nil, nil, err
	}

	switch {
	case platform.SBOM == nil:
		return nil, nil, ErrNoSBOM
	case platform.SBOMTooLarge:
		return nil, nil, imagecheck.ErrTooLarge
	}

	digest := platform.SBOM.Digest

	value, err := s.cachedOrFetch(ctx, "sbom:"+digest, func(ctx context.Context) (cached, error) {
		body, err := s.fetch.Blob(ctx, ref, digest, maxSBOMSize)
		if err != nil {
			return cached{}, err
		}

		sbom, raw, err := ParseSBOM(body)
		if err != nil {
			return cached{}, err
		}

		// The table and the document are each about the size of the body.
		return cached{value: sbomEntry{sbom, raw}, cost: 2 * int64(len(body))}, nil
	})
	if err != nil {
		return nil, nil, s.fetchError(err)
	}

	e := value.(sbomEntry)

	return e.sbom, e.raw, nil
}

// Dockerfile joins one platform's layers with the Dockerfile its provenance
// embeds.
func (s *Store) Dockerfile(ctx context.Context, image string, p Platform) (*Dockerfile, error) {
	entry, ref, err := s.available(ctx, image)
	if err != nil {
		return nil, err
	}

	platform, err := find(entry.image, p)
	if err != nil {
		return nil, err
	}

	statement, ok := entry.provenance[platform.ManifestDigest]
	if !ok || platform.Provenance == nil || !platform.Provenance.HasSource {
		return nil, ErrNoSource
	}

	digest := platform.ManifestDigest

	// Both inputs are immutable under the digest, so the join is cached whole
	// rather than the provenance being parsed again on every request.
	value, err := s.cachedOrFetch(
		ctx,
		"dockerfile:"+digest,
		func(ctx context.Context) (cached, error) {
			body, _, err := s.fetch.Manifest(ctx, ref, digest)
			if err != nil {
				return cached{}, err
			}

			m, err := parseManifest(body)
			if err != nil {
				return cached{}, err
			}

			d, err := JoinDockerfile(statement, m.Layers)
			if err != nil {
				return cached{}, err
			}

			return cached{value: d, cost: int64(len(body) + len(statement))}, nil
		},
	)
	if err != nil {
		return nil, s.fetchError(err)
	}

	return value.(*Dockerfile), nil
}

func (s *Store) available(
	ctx context.Context,
	image string,
) (imageEntry, imagecheck.Reference, error) {
	if s == nil {
		return imageEntry{}, imagecheck.Reference{}, &ImageStatusError{StatusDisabled}
	}

	entry, ref := s.lookup(ctx, image)

	return entry, ref, entry.image.Err()
}

func find(image Image, p Platform) (ImagePlatform, error) {
	for _, candidate := range image.Platforms {
		if candidate.Platform == p {
			return candidate, nil
		}
	}

	return ImagePlatform{}, ErrUnknownPlatform
}

func (s *Store) lookup(ctx context.Context, image string) (imageEntry, imagecheck.Reference) {
	ref, digest, ok := imagecheck.ParseDigested(image)
	if !ok {
		return imageEntry{image: Image{Status: StatusUnpinned}}, ref
	}

	// Keyed by repository too: a refusal is about the repository, and a
	// public mirror of a private image must not inherit its answer.
	key := "image:" + ref.Host + "/" + ref.Repository + "@" + digest

	value, err := s.cachedOrFetch(ctx, key, func(ctx context.Context) (cached, error) {
		return s.fetchImage(ctx, ref, digest), nil
	})
	if err != nil {
		// Only the caller giving up gets here; the fetch carries on for others.
		return imageEntry{image: Image{Status: StatusUnavailable, RetryAfter: s.now()}}, ref
	}

	return value.(imageEntry), ref
}

// fetchImage never fails: what went wrong is the image's status, cached for
// as long as that status lasts.
func (s *Store) fetchImage(ctx context.Context, ref imagecheck.Reference, digest string) cached {
	var cost int64

	read := func(digest string) ([]byte, error) {
		body, _, err := s.fetch.Manifest(ctx, ref, digest)
		cost += int64(len(body))

		return body, err
	}

	body, mediaType, err := s.fetch.Manifest(ctx, ref, digest)
	if err != nil {
		return s.failure(err)
	}

	cost += int64(len(body))

	attested, err := parseIndex(body, mediaType, read)
	if err != nil {
		return s.failure(err)
	}

	if !slices.ContainsFunc(attested, func(a Attested) bool {
		return a.Provenance != nil || a.SBOM != nil
	}) {
		return cached{value: imageEntry{image: Image{Status: StatusNone}}, cost: cost}
	}

	entry := imageEntry{
		image:      Image{Status: StatusAvailable},
		provenance: map[string][]byte{},
	}

	for _, a := range attested {
		platform := ImagePlatform{
			Platform:       a.Platform,
			ManifestDigest: a.ManifestDigest,
			SBOM:           a.SBOM,
			SBOMTooLarge:   a.SBOM != nil && a.SBOM.Size > maxSBOMSize,
		}

		if a.Provenance != nil && a.Provenance.Size <= maxProvenanceSize {
			statement, err := s.fetch.Blob(ctx, ref, a.Provenance.Digest, maxProvenanceSize)
			if err != nil {
				return s.failure(err)
			}

			// Only a statement embedding a Dockerfile is kept, for the join.
			if p, err := ParseProvenance(statement); err == nil {
				platform.Provenance = p

				if p.HasSource {
					entry.provenance[a.ManifestDigest] = statement
					cost += int64(len(statement))
				}
			} else {
				slog.Warn("unreadable provenance", "digest", a.Provenance.Digest, "error", err)
			}
		}

		entry.image.Platforms = append(entry.image.Platforms, platform)
	}

	return cached{value: entry, cost: cost}
}

func (s *Store) failure(err error) cached {
	status, retry := StatusUnavailable, failedRetry

	switch {
	case errors.Is(err, imagecheck.ErrUnauthorized):
		status, retry = StatusPrivate, refusedRetry
	case errors.Is(err, imagecheck.ErrNotFound):
		status, retry = StatusMissing, refusedRetry
	default:
		slog.Warn("image attestations unavailable", "error", err)
	}

	expires := s.now().Add(retry)
	image := Image{Status: status}

	if status == StatusUnavailable {
		image.RetryAfter = expires
	}

	return cached{value: imageEntry{image: image}, cost: 1, expires: expires}
}

func (s *Store) fetchError(err error) error {
	if errors.Is(err, imagecheck.ErrTooLarge) {
		return err
	}

	return &UnavailableError{RetryAfter: s.now().Add(failedRetry)}
}

// cachedOrFetch returns key's live entry, or fetches it once however many
// callers ask at the same time. The fetch is detached from any one caller, so
// one that gives up does not fail the others.
func (s *Store) cachedOrFetch(
	ctx context.Context,
	key string,
	fetch func(context.Context) (cached, error),
) (any, error) {
	if value, ok := s.get(key); ok {
		return value, nil
	}

	result := s.flight.DoChan(key, func() (any, error) {
		detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), operationTimeout)
		defer cancel()

		c, err := fetch(detached)
		if err != nil {
			return nil, err
		}

		c.key = key
		s.put(c)

		return c.value, nil
	})

	select {
	case r := <-result:
		return r.Val, r.Err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Store) get(key string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	element, ok := s.entries[key]
	if !ok {
		return nil, false
	}

	c := element.Value.(*cached)
	if !c.expires.IsZero() && !s.now().Before(c.expires) {
		s.remove(element)

		return nil, false
	}

	s.lru.MoveToFront(element)

	return c.value, true
}

// put stores c and evicts the least recently used entries past the budget,
// never c itself.
func (s *Store) put(c cached) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if old, ok := s.entries[c.key]; ok {
		s.remove(old)
	}

	s.entries[c.key] = s.lru.PushFront(&c)
	s.used += c.cost

	for s.used > s.budget && s.lru.Len() > 1 {
		s.remove(s.lru.Back())
	}
}

func (s *Store) remove(element *list.Element) {
	c := s.lru.Remove(element).(*cached)
	delete(s.entries, c.key)
	s.used -= c.cost
}
