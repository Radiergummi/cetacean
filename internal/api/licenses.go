package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/sbom"
)

// HandleLicenses serves the projected open-source licenses document consumed by
// the licenses page. Public (registered under the auth-exempt /-/ prefix).
func HandleLicenses(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	writeRawWithETag(w, r, sbom.ProjectedJSON())
}

// HandleLicenseText serves one pooled license or notice text by its
// content-addressed id. Public (registered under the auth-exempt /-/ prefix).
func HandleLicenseText(w http.ResponseWriter, r *http.Request) {
	text, ok := sbom.Text(r.PathValue("id"))
	if !ok {
		writeProblem(w, r, http.StatusNotFound, "no license text with that identifier")

		return
	}

	// The id is a hash of the bytes, so a given URL's content can never
	// change. Nothing to revalidate.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writeRawWithETag(w, r, []byte(text))
}

// HandleSBOM serves the raw embedded CycloneDX SBOM for supply-chain tooling.
// Public (registered under the auth-exempt /-/ prefix).
func HandleSBOM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json; version=1.6")
	writeRawWithETag(w, r, sbom.Raw())
}
