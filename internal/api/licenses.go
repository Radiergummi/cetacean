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

// HandleSBOM serves the raw embedded CycloneDX SBOM for supply-chain tooling.
// Public (registered under the auth-exempt /-/ prefix).
func HandleSBOM(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json; version=1.6")
	writeRawWithETag(w, r, sbom.Raw())
}
