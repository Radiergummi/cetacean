package api

import "net/http"

// The shared context maps none of RFC 9457's members: elsewhere `type`,
// `status` and `detail` are Cetacean's own, a resource kind among them.
// A problem document adds problemContextTerms after it instead.
const jsonLDContextDoc = `{
  "@context": {
    "@vocab": "urn:cetacean:",
    "items": {"@container": "@set"},
    "kind": "urn:cetacean:kind",
    "name": "urn:cetacean:name",
    "replicas": "urn:cetacean:replicas",
    "runningReplicas": "urn:cetacean:runningReplicas",
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
    "usedBy": "urn:cetacean:usedBy",
    "versionOf": "urn:cetacean:versionOf",
    "versionInferred": "urn:cetacean:versionInferred",
    "driver": "urn:cetacean:driver",
    "scope": "urn:cetacean:scope",
    "networks": {"@container": "@list"},
    "actor": "urn:cetacean:actor",
    "transport": "urn:cetacean:transport"
  }
}`

const problemContextTerms = `{` +
	`"type":{"@id":"urn:ietf:rfc:9457#type","@type":"@id"},` +
	`"title":"urn:ietf:rfc:9457#title",` +
	`"status":"urn:ietf:rfc:9457#status",` +
	`"detail":"urn:ietf:rfc:9457#detail",` +
	`"instance":{"@id":"urn:ietf:rfc:9457#instance","@type":"@id"}` +
	`}`

// contextBody is the JSON-LD context document, fixed for the life of the
// process like the two documents /api serves beside it.
var contextBody = newStaticBody([]byte(jsonLDContextDoc))

var contextSends = sending{ContentTypeJSON: mustMediaType("application/ld+json")}

// HandleContext serves the JSON-LD context document.
func HandleContext(w http.ResponseWriter, r *http.Request) {
	if !acceptableAs(r, contextSends, ContentTypeJSON) {
		notAcceptable(w, r, "application/ld+json")
		return
	}

	w.Header().Set("Content-Type", "application/ld+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	contextBody.serve(w, r)
}
