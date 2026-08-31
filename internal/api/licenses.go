package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

// The three attribution documents are embedded at build time and never change,
// so their ETags are hashed once here rather than on every request.
var (
	licensesETag = computeETag(sbom.ProjectedJSON())
	noticesETag  = computeETag(sbom.Notices())
	sbomETag     = computeETag(sbom.Raw())
)

// HandleLicenses serves the projected open-source licenses document consumed by
// the licenses page. Public (registered under the auth-exempt /-/ prefix).
func HandleLicenses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	writeRawWithPrecomputedETag(w, r, sbom.ProjectedJSON(), licensesETag)
}

// HandleLicenseText serves one pooled license or notice text by its
// content-addressed id. Public (registered under the auth-exempt /-/ prefix).
func HandleLicenseText(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	text, ok := sbom.Text(id)
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "no license text with that identifier")

		return
	}

	// The id is a hash of the bytes, so a given URL's content can never
	// change. Nothing to revalidate — and nothing to hash, either: the id was
	// interned exactly as computeETag would derive it.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writeRawWithPrecomputedETag(w, r, []byte(text), `"`+id+`"`)
}

// HandleNotices serves the full third-party attribution document. Public
// (registered under the auth-exempt /-/ prefix).
func HandleNotices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writeRawWithPrecomputedETag(w, r, sbom.Notices(), noticesETag)
}

// HandleSBOM serves the raw embedded CycloneDX SBOM for supply-chain tooling.
// Public (registered under the auth-exempt /-/ prefix).
func HandleSBOM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json; version=1.6")
	writeRawWithPrecomputedETag(w, r, sbom.Raw(), sbomETag)
}
