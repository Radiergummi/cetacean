package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/problem"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
	"github.com/radiergummi/cetacean/internal/config"
)

// AnnotationResponse is the extra payload for GET /annotations/{id}.
type AnnotationResponse struct {
	Annotation cache.Annotation `json:"annotation"`
}

type annotationRequest struct {
	Timestamp *time.Time              `json:"timestamp"`
	Until     *time.Time              `json:"until"`
	Text      string                  `json:"text"`
	Tags      []string                `json:"tags"`
	Resources []cluster.AnnotationRef `json:"resources"`
}

// annotationWrite gates an annotation write. It skips requireLevel: the tiers
// bound what Cetacean does to Docker, and an annotation does nothing to it.
func annotationWrite(permits func(*http.Request) bool) tieredChain {
	return tieredChain{
		gates:   NewChain(attributeWrite),
		level:   config.OpsReadOnly,
		permits: permits,
	}
}

func (h *Handlers) annotationCaller(r *http.Request) cluster.AnnotationCaller {
	id := auth.IdentityFromContext(r.Context())

	return cluster.AnnotationCaller{
		CanRead:          h.acl.Checker(id, "read"),
		CanWrite:         h.acl.Checker(id, "write"),
		AnyGrant:         h.acl.HasAnyGrant(id),
		WritesEverything: h.acl.CanWriteEverything(id),
	}
}

// mayAnnotate reports whether the caller may write some service, stack or node,
// the least a create needs; which resources it covers is the use case's to check.
func (h *Handlers) mayAnnotate(r *http.Request) bool {
	if !h.annotations.Enabled() {
		return false
	}

	id := auth.IdentityFromContext(r.Context())
	access := h.acl.TypeGrants(id)

	return access.Can("write", "service") ||
		access.Can("write", "stack") ||
		access.Can("write", "node")
}

// mayEditAnnotation is whether the caller may edit the annotation the path names.
func (h *Handlers) mayEditAnnotation(r *http.Request) bool {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil || !h.annotations.Enabled() {
		return false
	}

	caller := h.annotationCaller(r)

	a, err := h.annotations.Get(caller, id)

	return err == nil && h.annotations.MayEdit(attributedRead(r), caller, a)
}

// attributedRead is the request's context with its caller as the actor, which
// a read needs to tell the author from anyone else.
func attributedRead(r *http.Request) context.Context {
	return cluster.Attributed(r.Context(), writeTransport(r))
}

// linkAnnotations links /annotations as rel="edit" when the caller may annotate
// resource, so the dashboard offers it from the server's answer, not its own.
func (h *Handlers) linkAnnotations(w http.ResponseWriter, r *http.Request, resource string) {
	h.linkAnnotationsWhen(w, r, h.acl.Can(auth.IdentityFromContext(r.Context()), "write", resource))
}

// linkClusterAnnotations links /annotations as rel="edit" when the caller may
// annotate the whole cluster, which takes write on everything.
func (h *Handlers) linkClusterAnnotations(w http.ResponseWriter, r *http.Request) {
	h.linkAnnotationsWhen(w, r, h.acl.CanWriteEverything(auth.IdentityFromContext(r.Context())))
}

func (h *Handlers) linkAnnotationsWhen(w http.ResponseWriter, r *http.Request, allowed bool) {
	if allowed && h.annotations.Enabled() {
		w.Header().Add("Link", `<`+absPath(r.Context(), "/annotations")+`>; rel="edit"`)
	}
}

func (h *Handlers) HandleGetAnnotation(w http.ResponseWriter, r *http.Request) {
	a, ok := h.annotationOr404(w, r)
	if !ok {
		return
	}

	offer := readOnly
	if h.annotations.Enabled() &&
		h.annotations.MayEdit(attributedRead(r), h.annotationCaller(r), a) {
		offer = offeredWrites{
			methods:     []string{"GET", "HEAD", "PATCH", "DELETE"},
			acceptPatch: AcceptMergePatch,
		}
	}
	writeAllow(w, offer)

	body, err := json.Marshal(annotationBody(r, a))
	if err != nil {
		problem.Write(w, r, "API009", "failed to serialize response")
		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	writeRenderedJSON(w, r, http.StatusOK,
		renderedDoc{body: body, etag: annotationETag(a)}, annotationModified(a))
}

// HandleAnnotationTags lists the tags on the annotations the caller may read,
// with how many carry each, so a count never reveals a hidden note.
func (h *Handlers) HandleAnnotationTags(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}

	if !h.requireAnnotations(w, r) {
		return
	}

	p, err := parsePagination(r)
	if err != nil {
		problem.Write(w, r, "API016", err.Error())
		return
	}

	tags := h.cache.AnnotationTags(r.URL.Query().Get("q"), h.annotationCaller(r).MayRead)

	if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
		page := csvPage(tags, p)
		table := csvTable{header: []string{"tag", "count"}, records: make([][]string, len(page))}
		for i, t := range page {
			table.records[i] = []string{t.Tag, strconv.Itoa(t.Count)}
		}

		writeCSV(w, r, "annotation-tags", table)
		return
	}

	writeListPage(w, r, tags, p, "", "AnnotationTag",
		func(t cache.AnnotationTag) string { return "/annotations?tag=" + url.QueryEscape(t.Tag) },
		func(page []cache.AnnotationTag) []cache.AnnotationTag { return page },
	)
}

// requireAnnotations refuses a request with ANN003 when annotations are off.
func (h *Handlers) requireAnnotations(w http.ResponseWriter, r *http.Request) bool {
	if h.annotations.Enabled() {
		return true
	}

	writeAnnotationError(w, r, cache.ErrAnnotationsOff)

	return false
}

// refuseParams answers 400 for the first of names the request carries.
func refuseParams(w http.ResponseWriter, r *http.Request, names ...string) bool {
	for _, name := range names {
		if r.URL.Query().Has(name) {
			problem.Write(w, r, "API016", "annotations take no "+name+" parameter")
			return true
		}
	}

	return false
}

func (h *Handlers) HandleCreateAnnotation(w http.ResponseWriter, r *http.Request) {
	limitWriteBody(w, r)

	var body annotationRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		problem.Write(w, r, "ANN001", "invalid annotation: "+err.Error())
		return
	}

	in := cluster.AnnotationInput{Text: body.Text, Tags: body.Tags, Resources: body.Resources}
	if body.Timestamp != nil {
		in.Timestamp = *body.Timestamp
	}
	if body.Until != nil {
		in.Until = *body.Until
	}

	a, err := h.annotations.Create(r.Context(), h.annotationCaller(r), in)
	if err != nil {
		writeAnnotationError(w, r, err)
		return
	}

	w.Header().Set("Location", absPath(r.Context(), annotationPath(a.ID)))
	w.Header().Set("ETag", annotationETag(a))
	writeJSONStatus(w, http.StatusCreated, annotationBody(r, a))
}

func (h *Handlers) HandlePatchAnnotation(w http.ResponseWriter, r *http.Request) {
	id, ok := annotationID(w, r)
	if !ok {
		return
	}

	limitWriteBody(w, r)

	if !requireMergePatch(w, r) {
		return
	}

	patch, ok := decodeAnnotationPatch(w, r)
	if !ok {
		return
	}

	a, err := h.annotations.Update(
		r.Context(), h.annotationCaller(r), id, patch, annotationPreconditions(r),
	)
	if err != nil {
		writeAnnotationError(w, r, err)
		return
	}

	w.Header().Set("ETag", annotationETag(a))
	writeMutationResponse(w, r, annotationBody(r, a))
}

func (h *Handlers) HandleDeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	id, ok := annotationID(w, r)
	if !ok {
		return
	}

	err := h.annotations.Delete(
		r.Context(), h.annotationCaller(r), id, annotationPreconditions(r),
	)
	if err != nil {
		writeAnnotationError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) annotationOr404(
	w http.ResponseWriter,
	r *http.Request,
) (cache.Annotation, bool) {
	id, ok := annotationID(w, r)
	if !ok {
		return cache.Annotation{}, false
	}

	a, err := h.annotations.Get(h.annotationCaller(r), id)
	if err != nil {
		writeAnnotationError(w, r, err)
		return cache.Annotation{}, false
	}

	return a, true
}

func annotationID(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		problem.Write(w, r, "ANN004", "annotation "+strconv.Quote(r.PathValue("id"))+" not found")
		return 0, false
	}

	return id, true
}

// annotationPreconditions evaluates RFC 9110 §13.2.2 against the annotation
// the write finds, inside the use case, so no other write lands between the
// check and the commit. Nil when the request names none.
func annotationPreconditions(r *http.Request) func(cache.Annotation) bool {
	ifNoneMatch := r.Header.Get("If-None-Match")
	if r.Header.Get("If-Match") == "" && ifNoneMatch == "" &&
		r.Header.Get("If-Unmodified-Since") == "" {
		return nil
	}

	return func(a cache.Annotation) bool {
		etag := annotationETag(a)
		if preconditionFailed(r, etag, annotationModified(a)) != "" {
			return false
		}

		return ifNoneMatch == "" || !etagMatch(ifNoneMatch, etag)
	}
}

// decodeAnnotationPatch reads an RFC 7396 merge patch. A member that is
// absent is no change; null clears tags, resources and until, and is refused
// for the two an annotation cannot be without.
func decodeAnnotationPatch(
	w http.ResponseWriter,
	r *http.Request,
) (cluster.AnnotationPatch, bool) {
	var patch cluster.AnnotationPatch

	members, ok := decodeMergePatch[map[string]json.RawMessage](w, r)
	if !ok {
		return patch, false
	}
	if members == nil {
		problem.Write(w, r, "API008", "the merge patch must be a JSON object")
		return patch, false
	}

	for name, raw := range members {
		null := bytes.Equal(bytes.TrimSpace(raw), []byte("null"))

		var err error

		switch name {
		case "timestamp", "text":
			if null {
				problem.Write(w, r, "ANN001", name+": cannot be removed")
				return patch, false
			}

			if name == "timestamp" {
				patch.Timestamp = new(time.Time)
				err = json.Unmarshal(raw, patch.Timestamp)
			} else {
				patch.Text = new(string)
				err = json.Unmarshal(raw, patch.Text)
			}
		case "until":
			patch.Until = new(time.Time)
			if !null {
				err = json.Unmarshal(raw, patch.Until)
			}
		case "tags":
			patch.Tags = new([]string)
			err = json.Unmarshal(raw, patch.Tags)
		case "resources":
			patch.Resources = new([]cluster.AnnotationRef)
			err = json.Unmarshal(raw, patch.Resources)
		default:
			problem.Write(w, r, "ANN001", strconv.Quote(name)+": not a member of an annotation")
			return patch, false
		}

		if err != nil {
			problem.Write(w, r, "ANN001", name+": "+err.Error())
			return patch, false
		}
	}

	return patch, true
}

func writeAnnotationError(w http.ResponseWriter, r *http.Request, err error) {
	if invalid, ok := errors.AsType[*cluster.AnnotationInvalidError](err); ok {
		problem.Write(w, r, "ANN001", invalid.Error())
		return
	}

	switch {
	case errors.Is(err, cache.ErrAnnotationNotFound):
		problem.Write(w, r, "ANN004", "annotation "+strconv.Quote(r.PathValue("id"))+" not found")
	case errors.Is(err, cache.ErrAnnotationLimit):
		problem.Write(w, r, "ANN002", err.Error())
	case errors.Is(err, cache.ErrAnnotationsOff):
		problem.Write(w, r, "ANN003", err.Error())
	case errors.Is(err, cluster.ErrAnnotationDenied):
		problem.Write(w, r, "ACL002", err.Error())
	case errors.Is(err, cluster.ErrAnnotationPrecondition):
		problem.Write(w, r, "API013", "the precondition did not match the current annotation")
	default:
		slog.ErrorContext(r.Context(), "failed to store an annotation", "error", err)
		problem.Write(w, r, "ANN005", "the annotation could not be stored")
	}
}

func annotationBody(r *http.Request, a cache.Annotation) DetailResponse {
	return NewDetailResponse(
		r.Context(), annotationPath(a.ID), "Annotation", AnnotationResponse{listedAnnotation(a)},
	)
}

// listedAnnotation is a as every response shows it: tags and resources are
// always arrays.
func listedAnnotation(a cache.Annotation) cache.Annotation {
	if a.Tags == nil {
		a.Tags = []string{}
	}
	if a.Resources == nil {
		a.Resources = []cache.AnnotationResource{}
	}

	return a
}

func annotationPath(id uint64) string { return "/annotations/" + strconv.FormatUint(id, 10) }

// annotationETag is the annotation's version, which every write advances, and
// which of its resources are removed, which changes it without a write.
func annotationETag(a cache.Annotation) string {
	var removed uint32
	for i, r := range a.Resources {
		if r.Removed {
			removed |= 1 << i
		}
	}

	version := strconv.FormatUint(a.Version, 10)
	if removed != 0 {
		version += "-" + strconv.FormatUint(uint64(removed), 16)
	}

	return `"` + version + `"`
}

func annotationModified(a cache.Annotation) time.Time {
	if !a.Updated.IsZero() {
		return a.Updated
	}

	return a.Created
}
