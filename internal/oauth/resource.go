package oauth

import "errors"

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

// identifierOf is a resource's RFC 8707 identifier and the aud claim of every
// token bound to it.
//
// A path under another is still a separate audience. Identifiers are compared
// for exact equality and nothing here treats one as containing another: a token
// for the deployment root does not reach a resource mounted beneath it. The
// opposite reading is the audience-confusion attack resource indicators exist to
// prevent.
func (c ServerConfig) identifierOf(r Resource) string {
	return c.issuerID() + r.Path
}

// metadataURL is the absolute URL of a resource's RFC 9728 document, for the
// resource_metadata parameter of a WWW-Authenticate challenge.
func (c ServerConfig) metadataURL(r Resource) string {
	// The base path is already part of issuerID, so it must not be added twice.
	return c.Issuer + r.metadataPath(c.BasePath)
}

// resourceSet is everything about the configured resources that is settled once
// the server is built: which identifiers a token may be audienced for, which one
// an unindicated request resolves to, and the resource behind each. Resolved in
// NewServer rather than per request, because which audiences a deployment
// accepts is a property of the deployment and not of a request.
type resourceSet struct {
	identifiers []string
	byID        map[string]Resource
	def         Resource
	fallback    string
}

// newResourceSet indexes cfg's resources. The first is the default an
// unindicated token request resolves to, which is what lets a deployment
// offering one resource keep the behaviour it had before there were two.
func newResourceSet(cfg ServerConfig) resourceSet {
	set := resourceSet{
		identifiers: make([]string, 0, len(cfg.Resources)),
		byID:        make(map[string]Resource, len(cfg.Resources)),
		def:         cfg.Resources[0],
	}

	for _, r := range cfg.Resources {
		id := cfg.identifierOf(r)
		set.identifiers = append(set.identifiers, id)
		set.byID[id] = r
	}

	set.fallback = set.identifiers[0]

	return set
}

// resourceFor returns the resource an identifier names, falling back to the
// default for one this server does not serve.
func (s resourceSet) resourceFor(identifier string) Resource {
	if r, ok := s.byID[identifier]; ok {
		return r
	}

	return s.def
}

// effectiveResource resolves the RFC 8707 resource indicator from a token
// request to the identifier the grant will be bound to.
//
// An absent indicator resolves to the default resource unless the deployment
// requires one. A present indicator must equal a configured identifier exactly:
// a prefix match would let a token for one resource reach another mounted
// beneath it.
func (s resourceSet) effectiveResource(raw string, required bool) (string, error) {
	if raw == "" {
		if required {
			return "", errors.New("resource parameter is required")
		}

		return s.fallback, nil
	}

	if _, ok := s.byID[raw]; !ok {
		return "", errors.New("resource is not one this server issues tokens for")
	}

	return raw, nil
}
