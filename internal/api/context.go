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
    "instance": "urn:ietf:rfc:9457#instance",
    "kind": "urn:cetacean:kind",
    "name": "urn:cetacean:name",
    "replicas": "urn:cetacean:replicas",
    "mode": "urn:cetacean:mode",
    "role": "urn:cetacean:role",
    "state": "urn:cetacean:state",
    "availability": "urn:cetacean:availability",
    "ports": {"@container": "@list"},
    "aliases": "urn:cetacean:aliases",
    "tasks": {"@container": "@list"},
    "slot": "urn:cetacean:slot",
    "image": "urn:cetacean:image",
    "updateStatus": "urn:cetacean:updateStatus",
    "driver": "urn:cetacean:driver",
    "scope": "urn:cetacean:scope",
    "networks": {"@container": "@list"}
  }
}`

// HandleContext serves the JSON-LD context document.
func HandleContext(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/ld+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(jsonLDContextDoc)) //nolint:errcheck
}
