package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/radiergummi/cetacean/internal/auth"
)

// ContentType represents the resolved content type for a request.
type ContentType int

const (
	ContentTypeJSON ContentType = iota
	ContentTypeHTML
	ContentTypeSSE
	ContentTypeAtom
	ContentTypeJSONFeed
	ContentTypeJGF
	ContentTypeGraphML
	ContentTypeDOT
	ContentTypeCSV

	// ContentTypeYAML is served by the two specification documents and by the
	// compose export.
	ContentTypeYAML

	// ContentTypeUnsupported means no supported media type matched. What to
	// do about it is the endpoint's to decide.
	ContentTypeUnsupported ContentType = -1
)

func (ct ContentType) String() string {
	switch ct {
	case ContentTypeJSON:
		return "JSON"
	case ContentTypeHTML:
		return "HTML"
	case ContentTypeSSE:
		return "SSE"
	case ContentTypeAtom:
		return "Atom"
	case ContentTypeJSONFeed:
		return "JSONFeed"
	case ContentTypeJGF:
		return "JGF"
	case ContentTypeGraphML:
		return "GraphML"
	case ContentTypeDOT:
		return "DOT"
	case ContentTypeCSV:
		return "CSV"
	case ContentTypeYAML:
		return "YAML"
	case ContentTypeUnsupported:
		return "Unsupported"
	default:
		return "Unknown"
	}
}

type contentTypeKey struct{}

// htmlUnacceptableKey marks a request whose client would not take text/html.
// Recorded only when true, so the zero value is the permissive one.
type htmlUnacceptableKey struct{}

type extensionKey struct{}

// ContentTypeFromContext returns the negotiated content type, defaulting to JSON.
func ContentTypeFromContext(ctx context.Context) ContentType {
	if ct, ok := ctx.Value(contentTypeKey{}).(ContentType); ok {
		return ct
	}
	return ContentTypeJSON
}

// extensionFromContext returns the suffix negotiate stripped from the path, or
// "" when the type came from Accept instead. Anything rebuilding the URI puts
// it back: it is the only thing naming the representation, so a redirect that
// drops it is re-negotiated from an Accept that may disagree.
func extensionFromContext(ctx context.Context) string {
	ext, _ := ctx.Value(extensionKey{}).(string)

	return ext
}

// selfPath is the request's own path as a client would spell it again, its
// suffix included, for a link to another page of the same representation.
func selfPath(r *http.Request) string {
	return absPath(r.Context(), r.URL.EscapedPath()) + extensionFromContext(r.Context())
}

// htmlUnacceptable reports whether the client ruled out text/html — by naming
// an extension suffix that is not .html, or by an Accept header that does not
// admit it.
func htmlUnacceptable(ctx context.Context) bool {
	refused, _ := ctx.Value(htmlUnacceptableKey{}).(bool)

	return refused
}

// negotiate resolves the effective content type from an extension suffix or
// Accept header and stores it in the request context. It resolves and records;
// it does not refuse. 406 is a statement about one endpoint, and the route is
// not known here.
func negotiate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept")

		// On a write to a member, a suffix would only choose the response's
		// representation, and reading it off a name instead changes or removes
		// the wrong resource.
		ct, ext := ContentTypeUnsupported, ""
		if isSafeMethod(r.Method) || r.Method == http.MethodOptions ||
			!addressesMember(r.URL.Path) {
			ct, ext = resolveExtension(r)
		}

		next.ServeHTTP(w, r.WithContext(negotiated(r, ct, ext)))
	})
}

// negotiated records the type a suffix resolved to, or, when ct is
// ContentTypeUnsupported, the one the Accept header prefers.
func negotiated(r *http.Request, ct ContentType, ext string) context.Context {
	acceptsHTML := ct == ContentTypeHTML

	if ct == ContentTypeUnsupported {
		ranges := parseAcceptRanges(r.Header.Get("Accept"))
		ct = bestMatch(ranges)
		acceptsHTML = rangesAcceptHTML(ranges)
	}

	ctx := context.WithValue(r.Context(), contentTypeKey{}, ct)
	ctx = context.WithValue(ctx, extensionKey{}, ext)

	return context.WithValue(ctx, htmlUnacceptableKey{}, !acceptsHTML)
}

// suffixedNames puts back a suffix negotiate stripped when the whole segment
// names a member the caller may read: volume backup.json is that volume, not
// volume backup as JSON. After authentication, so how the path is answered
// reveals no name the caller cannot read.
func (h *Handlers) suffixedNames(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ext := extensionFromContext(r.Context())
		collection, identifier, rest := splitResourcePath(r.URL.Path)

		if ext == "" || identifier == "" || rest != "" ||
			!h.namesReadableMember(r, collection, identifier+ext) {
			next.ServeHTTP(w, r)

			return
		}

		r.URL.Path += ext
		if r.URL.RawPath != "" {
			r.URL.RawPath += ext
		}

		next.ServeHTTP(w, r.WithContext(negotiated(r, ContentTypeUnsupported, "")))
	})
}

// addressesMember reports whether path ends in the identifier of a member of a
// collection that can be addressed by a name, where a dot is part of the name.
func addressesMember(path string) bool {
	collection, identifier, rest := splitResourcePath(path)
	if identifier == "" || rest != "" {
		return false
	}

	_, resolvable := canonicalResolvers[collection]

	return resolvable || collection == "volumes" || collection == "stacks"
}

// namesReadableMember reports whether identifier addresses a member of the
// collection the caller may read. A name several members share counts: the
// ambiguity report answers it, not a suffix.
func (h *Handlers) namesReadableMember(r *http.Request, collection, identifier string) bool {
	var resource string

	switch resolve, resolvable := canonicalResolvers[collection]; {
	case resolvable:
		_, named, found, err := resolve(h.cache, identifier)
		if err != nil {
			named, found = singularType[collection]+":"+identifier, true
		}
		if !found {
			return false
		}
		resource = named
	case collection == "volumes":
		if _, ok := h.cache.GetVolume(identifier); !ok {
			return false
		}
		resource = "volume:" + identifier
	case collection == "stacks":
		if _, ok := h.cache.GetStack(identifier); !ok {
			return false
		}
		resource = "stack:" + identifier
	default:
		return false
	}

	return h.acl.Can(auth.IdentityFromContext(r.Context()), "read", resource)
}

// notAcceptable refuses a type the endpoint does not serve, naming what it
// does. Every dispatcher that chooses among representations ends here.
func notAcceptable(w http.ResponseWriter, r *http.Request, serves string) {
	writeErrorCode(w, r, "API003", "this endpoint supports "+serves)
}

// supportedTypes lists the media types we support, mapped to ContentType.
// When a wildcard matches multiple supported types, we prefer earlier entries.
var supportedTypes = []struct {
	typ     string // e.g. "application"
	subtype string // e.g. "json"
	ct      ContentType
}{
	{"application", "json", ContentTypeJSON},
	{"application", "vnd.cetacean.v1+json", ContentTypeJSON},
	// Every JSON response here is a JSON-LD document, so this is an alias for
	// the JSON branch rather than a separate representation.
	{"application", "ld+json", ContentTypeJSON},
	{"text", "html", ContentTypeHTML},
	{"application", "xhtml+xml", ContentTypeHTML},
	{"text", "event-stream", ContentTypeSSE},
	{"application", "atom+xml", ContentTypeAtom},
	{"application", "feed+json", ContentTypeJSONFeed},
	{"application", "vnd.jgf+json", ContentTypeJGF},
	{"application", "graphml+xml", ContentTypeGraphML},
	{"text", "vnd.graphviz", ContentTypeDOT},
	// After text/html, so a text/* wildcard still resolves to HTML.
	{"text", "csv", ContentTypeCSV},
	// The specification documents, served by /api and /api/asyncapi only.
	// After application/json, so an application/* wildcard still resolves to
	// JSON.
	//
	// Both halves of each pair are named. Registering only the +yaml spelling
	// would resolve `Accept: …+json, …+yaml` — the pair an AsyncAPI tool sends
	// — to YAML, since the JSON half would match nothing and the YAML half
	// would win by default; and it would leave /api answering 406 to the
	// registered JSON type for an OpenAPI document.
	{"application", "vnd.oai.openapi+json", ContentTypeJSON},
	{"application", "openapi+json", ContentTypeJSON},
	{"application", "vnd.aai.asyncapi+json", ContentTypeJSON},
	{"application", "asyncapi+json", ContentTypeJSON},
	{"application", "vnd.oai.openapi", ContentTypeYAML},
	{"application", "openapi+yaml", ContentTypeYAML},
	{"application", "vnd.aai.asyncapi+yaml", ContentTypeYAML},
	{"application", "asyncapi+yaml", ContentTypeYAML},
	{"application", "yaml", ContentTypeYAML},
	{"application", "x-yaml", ContentTypeYAML},
	// After text/html and text/csv, for the same wildcard reason.
	{"text", "yaml", ContentTypeYAML},
	{"text", "x-yaml", ContentTypeYAML},
}

// yamlPaths are the routes that have a YAML representation: the specification
// documents, and a compose export of a stack or a service. Elsewhere the
// suffix is part of the identifier — a volume is addressed by its name, and
// prometheus.yml is a name rather than a request for YAML.
var yamlPaths = []string{"/api", "/services", "/stacks"}

// extensionTypes maps URL extension suffixes to content types.
// Extension suffix takes priority over the Accept header.
var extensionTypes = []struct {
	ext string
	ct  ContentType
	// under, when set, limits the suffix to these routes and what lies
	// below them. Elsewhere it is part of the identifier.
	under []string
}{
	{ext: ".json", ct: ContentTypeJSON},
	{ext: ".html", ct: ContentTypeHTML},
	{ext: ".atom", ct: ContentTypeAtom},
	{ext: ".feed", ct: ContentTypeJSONFeed},
	{ext: ".jgf", ct: ContentTypeJGF},
	{ext: ".graphml", ct: ContentTypeGraphML},
	{ext: ".dot", ct: ContentTypeDOT},
	{ext: ".csv", ct: ContentTypeCSV},
	{ext: ".yaml", ct: ContentTypeYAML, under: yamlPaths},
	{ext: ".yml", ct: ContentTypeYAML, under: yamlPaths},
}

// scopedTo reports whether path is one of the routes a suffix is limited to,
// or below it. An unscoped suffix reads everywhere.
func scopedTo(under []string, path string) bool {
	if under == nil {
		return true
	}

	for _, route := range under {
		if path == route || strings.HasPrefix(path, route+"/") {
			return true
		}
	}

	return false
}

// literalDocuments are served under a filename rather than as a representation
// of a resource: /api/openapi.yaml names the document, and stripping .yaml off
// it would route to an /api/openapi that does not exist.
var literalDocuments = map[string]bool{
	openAPIYAMLPath:  true,
	asyncAPIYAMLPath: true,
}

// hasMidPathExtension reports whether a known extension suffix appears in a
// non-terminal segment; resolveExtension answers for the terminal one. Matching
// the table rather than any dot is what keeps a Docker name like web.json from
// reading as a representation.
func hasMidPathExtension(path string) bool {
	for _, ext := range extensionTypes {
		if strings.Contains(path, ext.ext+"/") && scopedTo(ext.under, path) {
			return true
		}
	}

	return false
}

// resolveExtension checks for a known extension suffix on the request path.
// If found, it strips the suffix from the path, raw and decoded, and returns
// the content type along with the suffix it removed. Returns
// ContentTypeUnsupported if no extension matches.
func resolveExtension(r *http.Request) (ContentType, string) {
	path := r.URL.Path
	if literalDocuments[path] {
		return ContentTypeUnsupported, ""
	}

	for _, ext := range extensionTypes {
		if trimmed, ok := strings.CutSuffix(path, ext.ext); ok {
			if !scopedTo(ext.under, trimmed) {
				continue
			}
			r.URL.Path = trimmed
			r.URL.RawPath = strings.TrimSuffix(r.URL.RawPath, ext.ext)

			return ext.ct, ext.ext
		}
	}
	return ContentTypeUnsupported, ""
}

// acceptable reports whether the request admits one of the types an endpoint
// serves.
func acceptable(r *http.Request, serves ...ContentType) bool {
	if slices.Contains(serves, negotiatedAmong(r, serves...)) {
		return true
	}

	// No Accept at all is no preference, so any type will do.
	return extensionFromContext(r.Context()) == "" &&
		len(parseAcceptRanges(r.Header.Get("Accept"))) == 0
}

// negotiatedAmong is the negotiated type, or, when the endpoint does not serve
// it, the best of those it does: the negotiated type is the best of every
// endpoint's. A suffix names one type, so it is not second-guessed.
func negotiatedAmong(r *http.Request, serves ...ContentType) ContentType {
	ct := ContentTypeFromContext(r.Context())
	if slices.Contains(serves, ct) || extensionFromContext(r.Context()) != "" {
		return ct
	}

	if best := bestServed(
		parseAcceptRanges(r.Header.Get("Accept")),
		serves,
	); best != ContentTypeUnsupported {
		return best
	}

	return ct
}

// choose is negotiatedAmong, recorded on the request, for a dispatcher to hand
// the arm it picked: a handler choosing its own rendering off the context must
// read this choice, not the best of every endpoint's types.
func choose(r *http.Request, serves ...ContentType) (*http.Request, ContentType) {
	ct := negotiatedAmong(r, serves...)
	if ct == ContentTypeFromContext(r.Context()) {
		return r, ct
	}

	return r.WithContext(context.WithValue(r.Context(), contentTypeKey{}, ct)), ct
}

// rangesAcceptHTML reports whether the ranges admit text/html at a usable
// quality (RFC 9110 §12.5.1): the most specific matching range carries the
// weight, so "*/*, text/html;q=0" refuses it. The resolved ContentType cannot
// answer this — it reports JSON for a wildcard and an explicit type alike.
func rangesAcceptHTML(ranges []mediaRange) bool {
	return rangesAccept(ranges, "text", "html")
}

// rangesAccept is rangesAcceptHTML for any one media type.
func rangesAccept(ranges []mediaRange, typ, subtype string) bool {
	if len(ranges) == 0 {
		return true
	}

	governing, ok := governingRange(ranges, typ, subtype)

	return ok && governing.q > 0
}

// governingRange returns the range that weighs a media type: the most
// specific one matching it, the first of equals (RFC 9110 §12.5.1).
func governingRange(ranges []mediaRange, typ, subtype string) (mediaRange, bool) {
	var governing mediaRange
	found := false

	for _, mr := range ranges {
		if mr.matches(typ, subtype) && (!found || mr.specificity() > governing.specificity()) {
			governing, found = mr, true
		}
	}

	return governing, found
}

// mediaRange is a parsed Accept header entry.
type mediaRange struct {
	typ     string  // e.g. "text", "*"
	subtype string  // e.g. "html", "*"
	q       float64 // quality value 0.0-1.0
	order   int     // position in the Accept header (for tie-breaking)
}

// specificity returns the specificity level of a media range:
//   - 3 for exact match (e.g. text/html)
//   - 2 for partial wildcard (e.g. text/*)
//   - 1 for full wildcard (*​/*)
func (mr mediaRange) specificity() int {
	if mr.typ == "*" {
		return 1
	}
	if mr.subtype == "*" {
		return 2
	}
	return 3
}

// matches reports whether this media range matches the given type/subtype.
func (mr mediaRange) matches(typ, subtype string) bool {
	if mr.typ == "*" && mr.subtype == "*" {
		return true
	}
	if mr.typ == typ && mr.subtype == "*" {
		return true
	}
	return mr.typ == typ && mr.subtype == subtype
}

// parseAcceptRanges parses an Accept field value into its media ranges. An
// absent, empty or wholly unparsable header yields none, which every consumer
// reads as "no preference expressed".
func parseAcceptRanges(accept string) []mediaRange {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return nil
	}

	ranges := make([]mediaRange, 0, strings.Count(accept, ",")+1)
	i := -1
	for part := range strings.SplitSeq(accept, ",") {
		i++
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split media type from parameters (;q=...)
		mediaType := part
		q := 1.0
		if before, after, ok := strings.Cut(part, ";"); ok {
			mediaType = strings.TrimSpace(before)
			params := after
			for param := range strings.SplitSeq(params, ";") {
				param = strings.TrimSpace(param)
				if len(param) > 2 && strings.EqualFold(param[:2], "q=") {
					if v, err := strconv.ParseFloat(param[2:], 64); err == nil {
						q = v
					}
				}
			}
		}

		// Split into type/subtype; skip malformed entries.
		slash := strings.IndexByte(mediaType, '/')
		if slash < 1 || slash >= len(mediaType)-1 {
			continue
		}
		// Type and subtype are case-insensitive (RFC 9110 §8.3.1).
		typ := strings.ToLower(mediaType[:slash])
		subtype := strings.ToLower(mediaType[slash+1:])

		ranges = append(ranges, mediaRange{
			typ:     typ,
			subtype: subtype,
			q:       q,
			order:   i,
		})
	}

	return ranges
}

// bestMatch picks the supported type the given ranges prefer, defaulting to
// JSON when no preference was expressed and returning ContentTypeUnsupported
// when a preference was expressed that none of our types satisfies.
func bestMatch(ranges []mediaRange) ContentType {
	if len(ranges) == 0 {
		return ContentTypeJSON
	}

	return bestServed(ranges, nil)
}

// bestServed is bestMatch among the types an endpoint serves, nil being all.
// Equal weights go to the more specific range, then the earlier one, then the
// earlier supported type.
func bestServed(ranges []mediaRange, serves []ContentType) ContentType {
	var best mediaRange
	bestCT := ContentTypeUnsupported

	for i, sup := range supportedTypes {
		if serves != nil && !slices.Contains(serves, sup.ct) {
			continue
		}

		mr, ok := governingRange(ranges, sup.typ, sup.subtype)
		// A weight of zero means not acceptable.
		if !ok || mr.q <= 0 || namedOnly[i] && mr.specificity() < 3 {
			continue
		}

		if bestCT == ContentTypeUnsupported || mr.q > best.q ||
			mr.q == best.q && (mr.specificity() > best.specificity() ||
				mr.specificity() == best.specificity() && mr.order < best.order) {
			best, bestCT = mr, sup.ct
		}
	}

	return bestCT
}

// namedOnly marks the supportedTypes entries no wildcard admits. An alias is
// one: the response carries the first entry of its ContentType, and a wildcard
// is judged against what is sent. A stream is the other: a wildcard asks for a
// document, not a connection that never ends.
var namedOnly = func() []bool {
	named := make([]bool, len(supportedTypes))
	seen := map[ContentType]bool{}

	for i, sup := range supportedTypes {
		named[i] = seen[sup.ct] || sup.ct == ContentTypeSSE
		seen[sup.ct] = true
	}

	return named
}()
