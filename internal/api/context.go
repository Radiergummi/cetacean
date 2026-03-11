package api

import "net/http"

const jsonLDContextDoc = `{
  "@context": {
    "@vocab": "urn:cetacean:",
    "items": {"@container": "@set"},
    "type": "urn:ietf:rfc:9457#type",
    "title": "urn:ietf:rfc:9457#title",
    "status": "urn:ietf:rfc:9457#status",
    "detail": "urn:ietf:rfc:9457#detail",
    "instance": "urn:ietf:rfc:9457#instance"
  }
}`

// HandleContext serves the JSON-LD context document.
func HandleContext(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/ld+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(jsonLDContextDoc)) //nolint:errcheck
}
