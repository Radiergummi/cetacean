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

	// Name is the human-readable name RFC 9728 §2 recommends, shown to the end
	// user by a client in place of the identifier.
	Name string
}

// metadataPath is the RFC 9728 §3.1 location of this resource's document: the
// well-known segment sits between the authority and the resource's whole path,
// base path included. This is the URL a conformant client derives from the
// resource identifier, so it is the one that has to answer.
func (r Resource) metadataPath(basePath string) string {
	return wellKnownPRM + basePath + r.Path
}

// mountedMetadataPath is the same document beneath the deployment's own prefix,
// served as well as the conformant location because a reverse proxy forwarding
// only {base_path}/* never delivers a request to the authority root.
func (r Resource) mountedMetadataPath(basePath string) string {
	return basePath + wellKnownPRM + r.Path
}

// identifierOf is a resource's RFC 8707 identifier and the aud claim of every
// token bound to it. Identifiers compare exactly, never by prefix: a token for
// the deployment root must not reach a resource mounted beneath it.
func (c ServerConfig) identifierOf(r Resource) string {
	return c.issuerID() + r.Path
}

// metadataURL is the absolute URL a WWW-Authenticate challenge points at: the
// mounted location, because it is followed rather than derived and must resolve
// through whatever proxy forwards this deployment.
func (c ServerConfig) metadataURL(r Resource) string {
	return c.Issuer + r.mountedMetadataPath(c.BasePath)
}

// resourceSet is everything about the configured resources settled in NewServer:
// which identifiers a token may be audienced for, which one an unindicated
// request resolves to, and the resource behind each.
type resourceSet struct {
	identifiers []string
	byID        map[string]Resource

	// Every accepted spelling of an identifier mapped onto the one a grant binds
	// to. A token's aud is compared byte-exact against the resource server's own
	// identifier, so an equivalent spelling must be resolved before it is stored.
	canonical map[string]string

	def      Resource
	fallback string
}

// newResourceSet indexes cfg's resources. The first is the default an
// unindicated token request resolves to, which is what lets a deployment
// offering one resource keep the behaviour it had before there were two.
func newResourceSet(cfg ServerConfig) resourceSet {
	set := resourceSet{
		identifiers: make([]string, 0, len(cfg.Resources)),
		byID:        make(map[string]Resource, len(cfg.Resources)),
		canonical:   make(map[string]string, len(cfg.Resources)),
		def:         cfg.Resources[0],
	}

	for _, r := range cfg.Resources {
		id := cfg.identifierOf(r)
		set.identifiers = append(set.identifiers, id)
		set.byID[id] = r
		set.canonical[id] = id

		// RFC 3986 §6.2.3 makes an empty path equivalent to "/" for http and https,
		// so a client that normalizes the identifier — or reads it off the API
		// catalog, which anchors at "/" — sends the slashed form. Indexed too, so
		// the same resource is not refused for being spelled canonically.
		if r.Path == "" {
			set.byID[id+"/"] = r
			set.canonical[id+"/"] = id
		}
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

// effectiveResource resolves a token request's RFC 8707 resource indicators to
// the one identifier the grant is bound to: the default if absent and not
// required, else an exact match. A repeat (RFC 8707 §2) is refused, not trimmed
// to one. POST callers pass body values only (RFC 6749 §3.2), not r.Form.
func (s resourceSet) effectiveResource(raw []string, required bool) (string, error) {
	if len(raw) > 1 {
		return "", errors.New(
			"only one resource parameter is supported: a token is bound to one resource",
		)
	}

	if len(raw) == 0 || raw[0] == "" {
		if required {
			return "", errors.New("resource parameter is required")
		}

		return s.fallback, nil
	}

	id, ok := s.canonical[raw[0]]
	if !ok {
		return "", errors.New("resource is not one this server issues tokens for")
	}

	return id, nil
}

// canonicalSpelling is identifier as a grant stores it, so a comparison against
// a bound resource survives an equivalent spelling on either side. One this
// server does not serve is returned unchanged, and still compares equal only to
// itself.
func (s resourceSet) canonicalSpelling(identifier string) string {
	if id, ok := s.canonical[identifier]; ok {
		return id
	}

	return identifier
}
