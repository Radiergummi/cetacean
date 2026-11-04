package cluster

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
)

// The limits an annotation is held to.
const (
	MaxAnnotationText      = 1000
	MaxAnnotationTags      = 10
	MaxAnnotationResources = 20
)

var annotationTag = regexp.MustCompile(`^[a-z0-9._-]{1,40}$`)

// The refusals an annotation write answers besides the cache's own.
var (
	ErrAnnotationDenied       = errors.New("not allowed to change this annotation")
	ErrAnnotationPrecondition = errors.New("the annotation changed since its version was read")
)

// AnnotationInvalidError is a refused request body, naming the field at fault.
type AnnotationInvalidError struct {
	Field, Reason string
}

func (e *AnnotationInvalidError) Error() string { return e.Field + ": " + e.Reason }

func invalidAnnotation(field, format string, args ...any) error {
	return &AnnotationInvalidError{Field: field, Reason: fmt.Sprintf(format, args...)}
}

// AnnotationCaller is who reads or writes an annotation. A cluster-wide one
// is read with any grant, as a gap is, and written only with write on "*".
type AnnotationCaller struct {
	CanRead, CanWrite func(resource string) bool
	AnyGrant          bool
	WritesEverything  bool
}

// AnnotationRef names a resource by type and by ID or name.
type AnnotationRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// AnnotationInput is a new annotation. A zero Timestamp is now, a zero Until
// a point.
type AnnotationInput struct {
	Timestamp time.Time
	Until     time.Time
	Text      string
	Tags      []string
	Resources []AnnotationRef
}

// AnnotationPatch is an edit; a nil field is left as it is. Until set to the
// zero time makes a range a point.
type AnnotationPatch struct {
	Timestamp *time.Time
	Until     *time.Time
	Text      *string
	Tags      *[]string
	Resources *[]AnnotationRef
}

// Annotations validates, authorizes and records annotations for both transports.
type Annotations struct {
	cache *cache.Cache
}

// NewAnnotations returns the use case over c.
func NewAnnotations(c *cache.Cache) *Annotations {
	return &Annotations{cache: c}
}

// Enabled reports whether writes have a store to go to.
func (s *Annotations) Enabled() bool { return s != nil && s.cache.AnnotationsOn() }

// Get returns an annotation the caller may read, its removed resources marked;
// one they may not read is not found.
func (s *Annotations) Get(c AnnotationCaller, id uint64) (cache.Annotation, error) {
	a, err := s.get(c, id)

	return s.cache.WithRemovals(a), err
}

// get is Get unmarked, as a write must start from: the marks are not stored.
func (s *Annotations) get(c AnnotationCaller, id uint64) (cache.Annotation, error) {
	a, ok := s.cache.Annotation(id)
	if !ok || !c.MayRead(a) {
		return cache.Annotation{}, cache.ErrAnnotationNotFound
	}

	return a, nil
}

// MayEdit reports whether the caller may edit or delete a; the author may
// while they can still read it, anyone else only if they could create it.
func (s *Annotations) MayEdit(ctx context.Context, c AnnotationCaller, a cache.Annotation) bool {
	if authored(ctx, a) && c.MayRead(a) {
		return true
	}

	return c.mayCreate(a.Resources)
}

// Create validates, authorizes and records a new annotation, attributed to
// the actor on ctx.
func (s *Annotations) Create(
	ctx context.Context,
	c AnnotationCaller,
	in AnnotationInput,
) (cache.Annotation, error) {
	if !s.Enabled() {
		return cache.Annotation{}, cache.ErrAnnotationsOff
	}

	if in.Timestamp.IsZero() {
		in.Timestamp = time.Now()
	}

	resources, err := s.resolve(in.Resources, nil, c.CanRead)
	if err != nil {
		return cache.Annotation{}, err
	}

	a := cache.Annotation{
		Timestamp: in.Timestamp,
		Until:     in.Until,
		Text:      in.Text,
		Tags:      in.Tags,
		Resources: resources,
	}
	if err := validateAnnotation(a); err != nil {
		return cache.Annotation{}, err
	}

	if !c.mayCreate(resources) {
		return cache.Annotation{}, ErrAnnotationDenied
	}

	if actor, ok := cache.ActorFromContext(ctx); ok {
		a.Actor = &actor
	}

	created, err := s.cache.CreateAnnotation(ctx, a)

	return s.cache.WithRemovals(created), err
}

// Update applies patch to an annotation the caller may edit. matches, when
// set, is the caller's precondition on the version it last read.
func (s *Annotations) Update(
	ctx context.Context,
	c AnnotationCaller,
	id uint64,
	patch AnnotationPatch,
	matches func(cache.Annotation) bool,
) (cache.Annotation, error) {
	for {
		current, err := s.editable(ctx, c, id, matches)
		if err != nil {
			return cache.Annotation{}, err
		}

		next, err := s.patched(c, current, patch)
		if err != nil {
			return cache.Annotation{}, err
		}

		if patch.Resources != nil && !c.mayCreate(next.Resources) {
			return cache.Annotation{}, ErrAnnotationDenied
		}

		// RFC 7396: a patch that changes nothing leaves the target as it is.
		if sameAnnotation(current, next) {
			return s.cache.WithRemovals(current), nil
		}

		updated, err := s.cache.UpdateAnnotation(ctx, id, func(a *cache.Annotation) error {
			if a.Version != current.Version {
				return errAnnotationMoved
			}

			a.Timestamp, a.Until, a.Text = next.Timestamp, next.Until, next.Text
			a.Tags, a.Resources = next.Tags, next.Resources
			a.Editor = nil
			if actor, ok := cache.ActorFromContext(ctx); ok {
				a.Editor = &actor
			}

			return nil
		})
		if errors.Is(err, errAnnotationMoved) {
			continue
		}

		return s.cache.WithRemovals(updated), err
	}
}

// sameAnnotation reports whether b says what a does, in every field an edit sets.
func sameAnnotation(a, b cache.Annotation) bool {
	return a.Timestamp.Equal(b.Timestamp) && a.Until.Equal(b.Until) && a.Text == b.Text &&
		slices.Equal(a.Tags, b.Tags) && slices.Equal(a.Resources, b.Resources)
}

// Delete removes an annotation the caller may edit.
func (s *Annotations) Delete(
	ctx context.Context,
	c AnnotationCaller,
	id uint64,
	matches func(cache.Annotation) bool,
) error {
	for {
		current, err := s.editable(ctx, c, id, matches)
		if err != nil {
			return err
		}

		err = s.cache.DeleteAnnotation(ctx, id, func(a cache.Annotation) error {
			if a.Version != current.Version {
				return errAnnotationMoved
			}

			return nil
		})
		if errors.Is(err, errAnnotationMoved) {
			continue
		}

		return err
	}
}

// errAnnotationMoved is a write that lost a race with another; the checks run
// outside the cache's lock, so the write is retried against the new version.
var errAnnotationMoved = errors.New("annotation moved")

func (s *Annotations) editable(
	ctx context.Context,
	c AnnotationCaller,
	id uint64,
	matches func(cache.Annotation) bool,
) (cache.Annotation, error) {
	if !s.Enabled() {
		return cache.Annotation{}, cache.ErrAnnotationsOff
	}

	current, err := s.get(c, id)
	if err != nil {
		return cache.Annotation{}, err
	}

	if !s.MayEdit(ctx, c, current) {
		return cache.Annotation{}, ErrAnnotationDenied
	}

	// The caller's validator is the one a read answered, marks and all.
	if matches != nil && !matches(s.cache.WithRemovals(current)) {
		return cache.Annotation{}, ErrAnnotationPrecondition
	}

	return current, nil
}

func (s *Annotations) patched(
	c AnnotationCaller,
	current cache.Annotation,
	patch AnnotationPatch,
) (cache.Annotation, error) {
	next := current

	if patch.Timestamp != nil {
		next.Timestamp = *patch.Timestamp
	}
	if patch.Until != nil {
		next.Until = *patch.Until
	}
	if patch.Text != nil {
		next.Text = *patch.Text
	}
	if patch.Tags != nil {
		next.Tags = *patch.Tags
	}
	if patch.Resources != nil {
		resources, err := s.resolve(*patch.Resources, current.Resources, c.CanRead)
		if err != nil {
			return cache.Annotation{}, err
		}

		next.Resources = resources
	}

	return next, validateAnnotation(next)
}

// resolve looks each reference up, keeping the record of one already named by
// kept so an edit can leave a removed resource on the annotation. One canRead
// does not admit is not found, so the refusal does not confirm it exists.
func (s *Annotations) resolve(
	refs []AnnotationRef,
	kept []cache.AnnotationResource,
	canRead func(string) bool,
) ([]cache.AnnotationResource, error) {
	if len(refs) > MaxAnnotationResources {
		return nil, invalidAnnotation("resources", "at most %d", MaxAnnotationResources)
	}

	resources := make([]cache.AnnotationResource, 0, len(refs))

	for i, ref := range refs {
		field := fmt.Sprintf("resources[%d]", i)

		resource, err := s.resolveOne(ref, kept, canRead, field)
		if err != nil {
			return nil, err
		}

		if slices.ContainsFunc(resources, func(r cache.AnnotationResource) bool {
			return r.Type == resource.Type && r.ID == resource.ID
		}) {
			return nil, invalidAnnotation(field, "%s %q is named twice", ref.Type, ref.ID)
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

func (s *Annotations) resolveOne(
	ref AnnotationRef,
	kept []cache.AnnotationResource,
	canRead func(string) bool,
	field string,
) (cache.AnnotationResource, error) {
	switch cache.EventType(ref.Type) {
	case cache.EventService, cache.EventStack, cache.EventNode:
	default:
		return cache.AnnotationResource{}, invalidAnnotation(
			field+".type", "must be service, stack or node",
		)
	}

	if ref.ID == "" {
		return cache.AnnotationResource{}, invalidAnnotation(field+".id", "is required")
	}

	for _, r := range kept {
		if string(r.Type) == ref.Type && (r.ID == ref.ID || r.Name == ref.ID) {
			return r, nil
		}
	}

	record, found, err := LookupAs(s.cache, ref.Type, ref.ID, canRead)
	if err != nil || !found || !canRead(record.Resource()) {
		return cache.AnnotationResource{}, invalidAnnotation(
			field+".id", "%s %q not found", ref.Type, ref.ID,
		)
	}

	resource := cache.AnnotationResource{
		Type: cache.EventType(ref.Type),
		ID:   record.ID,
		Name: record.Name,
	}

	switch resource.Type {
	case cache.EventService:
		resource.Service = record.Name
		if svc, ok := s.cache.GetService(record.ID); ok {
			resource.Stack = svc.Spec.Labels[cache.StackLabel]
		}
	case cache.EventStack:
		resource.Stack = record.Name
	default:
	}

	return resource, nil
}

func validateAnnotation(a cache.Annotation) error {
	if strings.TrimSpace(a.Text) == "" {
		return invalidAnnotation("text", "is required")
	}
	if utf8.RuneCountInString(a.Text) > MaxAnnotationText {
		return invalidAnnotation("text", "at most %d characters", MaxAnnotationText)
	}

	if !a.Until.IsZero() && !a.Until.After(a.Timestamp) {
		return invalidAnnotation("until", "must be after the start")
	}

	if len(a.Tags) > MaxAnnotationTags {
		return invalidAnnotation("tags", "at most %d", MaxAnnotationTags)
	}
	for i, tag := range a.Tags {
		field := fmt.Sprintf("tags[%d]", i)
		if !annotationTag.MatchString(tag) {
			return invalidAnnotation(field, "must match %s", annotationTag)
		}
		if slices.Contains(a.Tags[:i], tag) {
			return invalidAnnotation(field, "%q is named twice", tag)
		}
	}

	return nil
}

// MayRead needs every named resource readable: with any, one readable name
// would reveal what was written about a resource the caller cannot read.
func (c AnnotationCaller) MayRead(a cache.Annotation) bool {
	if len(a.Resources) == 0 {
		return c.AnyGrant
	}

	return allAdmit(a.Resources, c.CanRead)
}

func (c AnnotationCaller) mayCreate(resources []cache.AnnotationResource) bool {
	if len(resources) == 0 {
		return c.WritesEverything
	}

	return allAdmit(resources, c.CanWrite)
}

// allAdmit checks each resource as CanReadEvent checks a removal: by the
// service and stack it was recorded under, should it be gone.
func allAdmit(resources []cache.AnnotationResource, can func(string) bool) bool {
	for _, r := range resources {
		if !can(string(r.Type)+":"+r.Name) &&
			(r.Service == "" || !can("service:"+r.Service)) &&
			(r.Stack == "" || !can("stack:"+r.Stack)) {
			return false
		}
	}

	return true
}

// authored compares subject and provider, but a deployment's OAuth server
// issues tokens only to whom its one upstream provider signed in, so a token's
// provider stands for that provider: one person is one author on both.
func authored(ctx context.Context, a cache.Annotation) bool {
	actor, ok := cache.ActorFromContext(ctx)

	return ok && a.Actor != nil && a.Actor.Subject == actor.Subject &&
		(a.Actor.Provider == actor.Provider ||
			a.Actor.Provider == auth.ProviderToken || actor.Provider == auth.ProviderToken)
}
