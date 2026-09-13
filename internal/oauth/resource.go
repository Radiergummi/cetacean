package oauth

import "strings"

// wellKnownPRM is the RFC 9728 well-known suffix. The document for a resource
// with a path is served beneath it, so one location describes one resource.
const wellKnownPRM = "/.well-known/oauth-protected-resource"

// Resource is one protected resource this server issues tokens for.
type Resource struct {
	// Path locates the resource under the deployment root: "" is the root
	// itself, "/sub" something mounted beneath it. It fixes both the resource
	// identifier and the metadata URL, so the two cannot drift apart.
	Path string

	// Realm is the WWW-Authenticate realm a resource server offers for it.
	Realm string
}

// identifier is the resource's RFC 8707 identifier and the aud claim of every
// token bound to it.
//
// A path under another is still a separate audience. Identifiers are compared
// for exact equality and nothing here treats one as containing another: a token
// for the deployment root does not reach a resource mounted beneath it. The
// opposite reading is the audience-confusion attack resource indicators exist to
// prevent.
func (r Resource) identifier(issuerID string) string {
	return issuerID + r.Path
}

// metadataPath is where this resource's RFC 9728 document is served.
//
// RFC 9728 §3.1 inserts the well-known segment after the authority, which for a
// base-path deployment would place the document outside the prefix Cetacean is
// mounted under — and often outside what the operator controls at all. The base
// path therefore precedes the well-known segment here, matching where the
// authorization server metadata has always been served.
func (r Resource) metadataPath(basePath string) string {
	return basePath + wellKnownPRM + r.Path
}

// resources returns the configured set, or a single root resource when none was
// configured — which is what a server with one unnamed protected resource is.
func (c ServerConfig) resources() []Resource {
	if len(c.Resources) == 0 {
		return []Resource{{Path: "", Realm: "cetacean"}}
	}

	return c.Resources
}

// defaultResource is what a token request carrying no RFC 8707 indicator
// resolves to: the first configured resource.
func (c ServerConfig) defaultResource() Resource {
	return c.resources()[0]
}

// defaultIdentifier is the identifier of the default resource: the audience a
// grant with no RFC 8707 indicator is bound to.
func (c ServerConfig) defaultIdentifier() string {
	return c.defaultResource().identifier(c.issuerID())
}

// knownIdentifiers lists every resource identifier this server will bind a
// token to, for validating an indicator against the set.
func (c ServerConfig) knownIdentifiers() []string {
	issuerID := c.issuerID()
	out := make([]string, 0, len(c.resources()))
	for _, r := range c.resources() {
		out = append(out, r.identifier(issuerID))
	}

	return out
}

// resourceFor returns the resource an identifier names, and whether it is one
// this server serves.
func (c ServerConfig) resourceFor(identifier string) (Resource, bool) {
	issuerID := c.issuerID()
	for _, r := range c.resources() {
		if r.identifier(issuerID) == identifier {
			return r, true
		}
	}

	return Resource{}, false
}

// metadataURL is the absolute URL of a resource's RFC 9728 document, for the
// resource_metadata parameter of a WWW-Authenticate challenge.
func (c ServerConfig) metadataURL(r Resource) string {
	// The base path is already part of issuerID, so it must not be added twice.
	return c.Issuer + r.metadataPath(c.BasePath)
}

// trimmedPath normalizes a configured resource path to the leading-slash form
// the identifier and metadata URL are built from. A bare "/" is the root.
func trimmedPath(path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}

	return trimmed
}
