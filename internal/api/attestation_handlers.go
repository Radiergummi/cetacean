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
	"github.com/radiergummi/cetacean/internal/imagecheck"
)

// Everything under a platform manifest digest is immutable, so a browser may
// keep an SBOM or a Dockerfile view; provenance carries task counts and is
// revalidated instead.
const attestationCacheControl = "private, max-age=3600"

// imageTarget is the image a service or task runs, with the platforms a
// request about it may name.
type imageTarget struct {
	path       string // the resource the request addresses, for @id
	image      string
	status     attestations.Image
	candidates []attestations.Platform
}

func (h *Handlers) HandleServiceProvenance(w http.ResponseWriter, r *http.Request) {
	r, ct := choose(r, nil, ContentTypeJSON)
	if ct != ContentTypeJSON {
		notAcceptable(w, r, servedMediaTypes[ContentTypeJSON])
		return
	}

	svc, ok := h.lookupServiceACL(w, r)
	if !ok {
		return
	}

	writeAllow(w, readOnly)

	body := cluster.ServiceProvenance(
		r.Context(), svc, h.cache.ListTasksByService(svc.ID), h.cache.GetNode, h.attestations,
	)
	writeCachedJSON(w, r, NewDetailResponse(
		r.Context(), "/services/"+svc.ID+"/provenance", "ImageProvenance", body,
	))
}

func (h *Handlers) HandleTaskProvenance(w http.ResponseWriter, r *http.Request) {
	r, ct := choose(r, nil, ContentTypeJSON)
	if ct != ContentTypeJSON {
		notAcceptable(w, r, servedMediaTypes[ContentTypeJSON])
		return
	}

	task, ok := h.lookupTaskACL(w, r)
	if !ok {
		return
	}

	writeAllow(w, readOnly)

	body := cluster.TaskProvenance(r.Context(), task, h.taskNode(task), h.attestations)
	writeCachedJSON(w, r, NewDetailResponse(
		r.Context(), "/tasks/"+task.ID+"/provenance", "ImageProvenance", body,
	))
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
	names := make([]string, len(serves))
	for i, ct := range serves {
		names[i] = servedMediaTypes[ct]
	}

	served := strings.Join(names, ", ")

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

		if !h.imageAvailable(w, r, target.status) {
			return
		}

		platform, err := cluster.SelectPlatform(target.candidates, r.URL.Query().Get("platform"))
		switch {
		case errors.Is(err, cluster.ErrPlatformRequired):
			writeErrorCode(w, r, "ATT003", "this image has attestations for "+
				platformList(target.candidates)+"; name one in the platform query parameter")
			return
		case err != nil:
			writeErrorCode(w, r, "ATT004", "name one of "+platformList(target.candidates))
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

	image := ""
	if spec := svc.Spec.TaskTemplate.ContainerSpec; spec != nil {
		image = spec.Image
	}

	img := h.attestationImage(r, image)

	var candidates []attestations.Platform
	for _, p := range img.Platforms {
		candidates = append(candidates, p.Platform)
	}

	return imageTarget{
		path: "/services/" + svc.ID + suffix, image: image, status: img, candidates: candidates,
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

	image := ""
	if spec := task.Spec.ContainerSpec; spec != nil {
		image = spec.Image
	}

	img := h.attestationImage(r, image)

	return imageTarget{
		path:       "/tasks/" + task.ID + suffix,
		image:      image,
		status:     img,
		candidates: cluster.TaskPlatforms(task, h.taskNode(task), img),
	}, true
}

func (h *Handlers) attestationImage(r *http.Request, image string) attestations.Image {
	if h.attestations == nil {
		return attestations.Image{Status: attestations.StatusDisabled}
	}

	return h.attestations.Image(r.Context(), image)
}

func (h *Handlers) lookupTaskACL(w http.ResponseWriter, r *http.Request) (swarm.Task, bool) {
	return lookupACL(h, w, r, "task", r.PathValue("id"), h.cache.GetTask,
		func(t swarm.Task) string { return "task:" + t.ID })
}

func (h *Handlers) taskNode(task swarm.Task) *swarm.Node {
	if node, ok := h.cache.GetNode(task.NodeID); ok {
		return &node
	}

	return nil
}

// imageAvailable answers for an image whose attestations cannot be read at
// all, and reports whether the request may go on.
func (h *Handlers) imageAvailable(
	w http.ResponseWriter,
	r *http.Request,
	img attestations.Image,
) bool {
	switch img.Status {
	case attestations.StatusAvailable:
		return true
	case attestations.StatusUnavailable:
		writeUnavailable(w, r, img.RetryAfter)
	default:
		writeErrorCode(w, r, "ATT007", "the image's attestations are "+string(img.Status))
	}

	return false
}

func writeUnavailable(w http.ResponseWriter, r *http.Request, retryAfter time.Time) {
	seconds := max(int(math.Ceil(time.Until(retryAfter).Seconds())), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeErrorCode(w, r, "ATT001", "the image's registry could not be read")
}

// writeAttestationError answers for a failure reading one platform's SBOM or
// Dockerfile.
func writeAttestationError(w http.ResponseWriter, r *http.Request, err error) {
	var unavailable *attestations.UnavailableError

	var status *attestations.ImageStatusError

	switch {
	case errors.As(err, &unavailable):
		writeUnavailable(w, r, unavailable.RetryAfter)
	case errors.As(err, &status):
		writeErrorCode(w, r, "ATT007", "the image's attestations are "+string(status.Status))
	case errors.Is(err, attestations.ErrNoSBOM):
		writeErrorCode(w, r, "ATT002", "no SBOM for this platform")
	case errors.Is(err, imagecheck.ErrTooLarge):
		writeErrorCode(w, r, "ATT005", "the SBOM is larger than 32 MiB")
	case errors.Is(err, attestations.ErrNoSource):
		writeErrorCode(w, r, "ATT006", "the provenance embeds no Dockerfile")
	case errors.Is(err, attestations.ErrUnknownPlatform):
		writeErrorCode(w, r, "ATT004", "the image has no such platform")
	default:
		writeErrorCode(w, r, "ATT001", err.Error())
	}
}

type imageSBOMBody struct {
	Platform   string                 `json:"platform"`
	Generators []string               `json:"generators"`
	Created    *time.Time             `json:"created,omitempty"`
	Summary    imageSBOMSummary       `json:"summary"`
	Packages   []attestations.Package `json:"packages"`
}

type imageSBOMSummary struct {
	Total      int            `json:"total"`
	Licensed   int            `json:"licensed"`
	Ecosystems map[string]int `json:"ecosystems"`
}

func (h *Handlers) writeSBOM(
	w http.ResponseWriter,
	r *http.Request,
	ct ContentType,
	target imageTarget,
	platform attestations.Platform,
) {
	sbom, raw, err := h.attestations.SBOM(r.Context(), target.image, platform)
	if err != nil {
		writeAttestationError(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", attestationCacheControl)

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
		packages := sbom.Packages
		if packages == nil {
			packages = []attestations.Package{}
		}

		writeCachedJSON(
			w,
			r,
			NewDetailResponse(r.Context(), target.path, "ImageSBOM", imageSBOMBody{
				Platform:   platform.String(),
				Generators: sbom.Generators,
				Created:    sbom.Created,
				Summary: imageSBOMSummary{
					Total: len(sbom.Packages), Licensed: sbom.Licensed, Ecosystems: sbom.Ecosystems,
				},
				Packages: packages,
			}),
		)
	}
}

type imageDockerfileBody struct {
	Platform string `json:"platform"`
	*attestations.Dockerfile
}

func (h *Handlers) writeDockerfile(
	w http.ResponseWriter,
	r *http.Request,
	ct ContentType,
	target imageTarget,
	platform attestations.Platform,
) {
	d, err := h.attestations.Dockerfile(r.Context(), target.image, platform)
	if err != nil {
		writeAttestationError(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", attestationCacheControl)

	if ct == ContentTypePlain {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writeRawWithETag(w, r, []byte(strings.Join(d.Lines, "\n")+"\n"))

		return
	}

	writeCachedJSON(w, r, NewDetailResponse(r.Context(), target.path, "ImageDockerfile",
		imageDockerfileBody{Platform: platform.String(), Dockerfile: d}))
}

func platformList(platforms []attestations.Platform) string {
	names := make([]string, len(platforms))
	for i, p := range platforms {
		names[i] = p.String()
	}

	if len(names) == 0 {
		return "no platform"
	}

	return strings.Join(names, ", ")
}
