package api

import (
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/swarm"

	"github.com/radiergummi/cetacean/internal/attestations"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// A task never changes the image it runs, so a browser may keep its SBOM or
// Dockerfile view. A service's changes with every deploy, and its URLs are
// revalidated against the ETag instead, as provenance always is.
const attestationCacheControl = "private, max-age=3600"

// imageTarget is the attestation target of the resource a request addresses.
type imageTarget struct {
	path      string // for @id
	immutable bool   // a task's, whose image never changes
	cluster.AttestationTarget
}

// Provenance is a section of the detail page, not a page of its own, so it
// is served as JSON only.
func (h *Handlers) HandleServiceProvenance(w http.ResponseWriter, r *http.Request) {
	if !acceptable(r, ContentTypeJSON) {
		notAcceptable(w, r, servedMediaTypes[ContentTypeJSON])
		return
	}

	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	h.writeProvenance(w, r, "/services/"+svc.ID, cluster.ServiceProvenance(
		r.Context(), svc, h.cache.ListTasksByService(svc.ID), h.cache.GetNode, h.attestations,
	))
}

func (h *Handlers) HandleTaskProvenance(w http.ResponseWriter, r *http.Request) {
	if !acceptable(r, ContentTypeJSON) {
		notAcceptable(w, r, servedMediaTypes[ContentTypeJSON])
		return
	}

	task, ok := h.lookupTaskACL(w, r)
	if !ok {
		return
	}

	h.writeProvenance(w, r, "/tasks/"+task.ID,
		cluster.TaskProvenance(r.Context(), task, h.cache.GetNode, h.attestations))
}

func (h *Handlers) writeProvenance(
	w http.ResponseWriter,
	r *http.Request,
	resource string,
	body cluster.ImageProvenance,
) {
	writeAllow(w, readOnly)
	writeCachedJSON(
		w,
		r,
		NewDetailResponse(r.Context(), resource+"/provenance", "ImageProvenance", body),
	)
}

// HandleServiceSBOM and its siblings dispatch to the SPA for HTML, which loads
// the data itself; every other type is answered after the ACL check.
func (h *Handlers) HandleServiceSBOM(spa http.Handler) http.HandlerFunc {
	return h.attestationEndpoint(spa, h.serviceImage, h.writeSBOM,
		ContentTypeJSON, ContentTypeHTML, ContentTypeCSV, ContentTypeSPDX)
}

func (h *Handlers) HandleTaskSBOM(spa http.Handler) http.HandlerFunc {
	return h.attestationEndpoint(spa, h.taskImage, h.writeSBOM,
		ContentTypeJSON, ContentTypeHTML, ContentTypeCSV, ContentTypeSPDX)
}

func (h *Handlers) HandleServiceDockerfile(spa http.Handler) http.HandlerFunc {
	return h.attestationEndpoint(spa, h.serviceImage, h.writeDockerfile,
		ContentTypeJSON, ContentTypeHTML, ContentTypePlain)
}

func (h *Handlers) HandleTaskDockerfile(spa http.Handler) http.HandlerFunc {
	return h.attestationEndpoint(spa, h.taskImage, h.writeDockerfile,
		ContentTypeJSON, ContentTypeHTML, ContentTypePlain)
}

func (h *Handlers) attestationEndpoint(
	spa http.Handler,
	resolve func(http.ResponseWriter, *http.Request, string) (imageTarget, bool),
	write func(http.ResponseWriter, *http.Request, ContentType, imageTarget, attestations.Platform),
	serves ...ContentType,
) http.HandlerFunc {
	served := servedList(serves)

	return func(w http.ResponseWriter, r *http.Request) {
		r, ct := choose(r, nil, serves...)
		if ct == ContentTypeHTML {
			spa.ServeHTTP(w, r)
			return
		}

		if !slices.Contains(serves, ct) {
			notAcceptable(w, r, served)
			return
		}

		suffix := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/'):]

		target, ok := resolve(w, r, suffix)
		if !ok {
			return
		}

		writeAllow(w, readOnly)

		platform, err := target.Platform(r.URL.Query().Get("platform"))
		if err != nil {
			writeAttestationError(w, r, err)
			return
		}

		write(w, r, ct, target, platform)
	}
}

func (h *Handlers) serviceImage(
	w http.ResponseWriter,
	r *http.Request,
	suffix string,
) (imageTarget, bool) {
	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return imageTarget{}, false
	}

	return imageTarget{
		path:              "/services/" + svc.ID + suffix,
		AttestationTarget: cluster.ServiceAttestationTarget(r.Context(), svc, h.attestations),
	}, true
}

func (h *Handlers) taskImage(
	w http.ResponseWriter,
	r *http.Request,
	suffix string,
) (imageTarget, bool) {
	task, ok := h.lookupTaskACL(w, r)
	if !ok {
		return imageTarget{}, false
	}

	return imageTarget{
		path:      "/tasks/" + task.ID + suffix,
		immutable: true,
		AttestationTarget: cluster.TaskAttestationTarget(
			r.Context(), task, h.cache.GetNode, h.attestations,
		),
	}, true
}

func (h *Handlers) lookupTaskACL(w http.ResponseWriter, r *http.Request) (swarm.Task, bool) {
	return lookupACL(h, w, r, "task", r.PathValue("id"), h.cache.GetTask,
		func(t swarm.Task) string { return "task:" + t.ID })
}

func writeUnavailable(w http.ResponseWriter, r *http.Request, retryAfter time.Time) {
	seconds := max(int(math.Ceil(time.Until(retryAfter).Seconds())), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeErrorCode(w, r, "ATT001", "the image's registry could not be read")
}

// writeAttestationError answers for a request whose platform or whose SBOM or
// Dockerfile could not be read.
func writeAttestationError(w http.ResponseWriter, r *http.Request, err error) {
	var unavailable *attestations.UnavailableError

	var status *attestations.ImageStatusError

	var tooLarge *attestations.TooLargeError

	switch {
	case errors.As(err, &unavailable):
		writeUnavailable(w, r, unavailable.RetryAfter)
	case errors.As(err, &status):
		writeErrorCode(w, r, "ATT007", "the image's attestations are "+string(status.Status))
	case errors.Is(err, attestations.ErrNoSBOM):
		writeErrorCode(w, r, "ATT002", "no SBOM for this platform")
	case errors.As(err, &tooLarge):
		writeErrorCode(w, r, "ATT005", tooLarge.Error())
	case errors.Is(err, attestations.ErrUnreadable):
		writeErrorCode(w, r, "ATT008", err.Error())
	case errors.Is(err, attestations.ErrNoSource):
		writeErrorCode(w, r, "ATT006", "the provenance embeds no Dockerfile")
	case errors.Is(err, cluster.ErrPlatformRequired):
		writeErrorCode(w, r, "ATT003", err.Error())
	case errors.Is(err, attestations.ErrUnknownPlatform):
		writeErrorCode(w, r, "ATT004", err.Error())
	default:
		writeErrorCode(w, r, "ATT001", err.Error())
	}
}

func (h *Handlers) writeSBOM(
	w http.ResponseWriter,
	r *http.Request,
	ct ContentType,
	target imageTarget,
	platform attestations.Platform,
) {
	sbom, raw, err := h.attestations.SBOM(r.Context(), target.Image, platform)
	if err != nil {
		writeAttestationError(w, r, err)
		return
	}

	if target.immutable {
		w.Header().Set("Cache-Control", attestationCacheControl)
	}

	switch ct {
	case ContentTypeSPDX:
		w.Header().Set("Content-Type", "application/spdx+json")
		writeRawWithETag(w, r, raw)
	case ContentTypeCSV:
		table := csvTable{
			header: []string{"name", "version", "ecosystem", "license", "purl", "found_in"},
		}
		for _, p := range sbom.Packages {
			table.records = append(table.records,
				[]string{p.Name, p.Version, p.Ecosystem, p.License, p.PURL, p.FoundIn})
		}

		writeCSV(w, r, "sbom", table)
	default:
		writeCachedJSON(w, r, NewDetailResponse(
			r.Context(), target.path, "ImageSBOM", cluster.NewSBOMView(platform, sbom),
		))
	}
}

func (h *Handlers) writeDockerfile(
	w http.ResponseWriter,
	r *http.Request,
	ct ContentType,
	target imageTarget,
	platform attestations.Platform,
) {
	d, err := h.attestations.Dockerfile(r.Context(), target.Image, platform)
	if err != nil {
		writeAttestationError(w, r, err)
		return
	}

	if target.immutable {
		w.Header().Set("Cache-Control", attestationCacheControl)
	}

	if ct == ContentTypePlain {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeRawWithETag(w, r, []byte(strings.Join(d.Lines, "\n")+"\n"))

		return
	}

	writeCachedJSON(w, r, NewDetailResponse(r.Context(), target.path, "ImageDockerfile",
		cluster.DockerfileView{Platform: platform.String(), Dockerfile: d}))
}
