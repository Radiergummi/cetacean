package api

import (
	"context"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cluster"
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
			!addressesMember(r.URL.EscapedPath()) {
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
		collection, identifier, rest := splitResourcePath(r.URL.EscapedPath())
		identifier, err := url.PathUnescape(identifier)

		if ext == "" || identifier == "" || rest != "" || err != nil ||
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

// addressesMember reports whether the escaped path ends in the identifier of a
// member of a collection that can be addressed by a name, where a dot is part
// of the name. A plugin's tag is one: foo:1.csv.
func addressesMember(escapedPath string) bool {
	collection, identifier, rest := splitResourcePath(escapedPath)
	if identifier == "" || rest != "" {
		return false
	}

	_, resolvable := canonicalKinds[collection]

	return resolvable || collection == "volumes" || collection == "stacks" ||
		collection == "plugins" && identifier != "privileges"
}

// namesReadableMember reports whether identifier addresses a member of the
// collection the caller may read. A name several members share counts: the
// ambiguity report answers it, not a suffix.
func (h *Handlers) namesReadableMember(r *http.Request, collection, identifier string) bool {
	var resource string

	switch kind, resolvable := canonicalKinds[collection]; {
	case resolvable:
		record, found, err := cluster.Lookup(h.cache, kind, identifier)
		named := record.Resource()
		if err != nil {
			named, found = kind+":"+identifier, true
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
	case collection == "plugins":
		return h.namesReadablePlugin(r, identifier)
	default:
		return false
	}

	return h.acl.Can(auth.IdentityFromContext(r.Context()), "read", resource)
}

// namesReadablePlugin is namesReadableMember for a plugin, which the cache does
// not hold: the daemon is asked, and only once the caller may read the name.
func (h *Handlers) namesReadablePlugin(r *http.Request, name string) bool {
	if h.pluginClient == nil ||
		!h.acl.Can(auth.IdentityFromContext(r.Context()), "read", "plugin:"+name) {
		return false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := h.pluginClient.PluginInspect(ctx, name)

	return err == nil
}

// notAcceptable refuses a type the endpoint does not serve, naming what it
// does. Every dispatcher that chooses among representations ends here.
func notAcceptable(w http.ResponseWriter, r *http.Request, serves string) {
	writeErrorCode(w, r, "API003", "this endpoint supports "+serves)
}

// supportedTypes lists the names a client may ask for each ContentType by.
// A type is weighed by the one it is sent as (see sentAs); the others are
// aliases, which count only where named. Among equal weights, the ContentType
// listed first wins, so a text/* wildcard resolves to HTML.
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
	{"text", "csv", ContentTypeCSV},
	// Both halves of each specification-document pair are named, so a tool
	// sending `…+json, …+yaml` is weighed on both rather than defaulting to
	// the half that matched.
	{"application", "vnd.oai.openapi+json", ContentTypeJSON},
	{"application", "openapi+json", ContentTypeJSON},
	{"application", "vnd.aai.asyncapi+json", ContentTypeJSON},
	{"application", "asyncapi+json", ContentTypeJSON},
	{"application", "yaml", ContentTypeYAML},
	{"application", "x-yaml", ContentTypeYAML},
	{"application", "vnd.oai.openapi", ContentTypeYAML},
	{"application", "openapi+yaml", ContentTypeYAML},
	{"application", "vnd.aai.asyncapi+yaml", ContentTypeYAML},
	{"application", "asyncapi+yaml", ContentTypeYAML},
	{"text", "yaml", ContentTypeYAML},
	{"text", "x-yaml", ContentTypeYAML},
}

// contentTypeOrder is each ContentType once, in supportedTypes order.
var contentTypeOrder = func() []ContentType {
	var order []ContentType
	for _, sup := range supportedTypes {
		if !slices.Contains(order, sup.ct) {
			order = append(order, sup.ct)
		}
	}

	return order
}()

// sentAs is the media type each ContentType goes out as, unless the endpoint
// says otherwise with a sending.
var sentAs = map[ContentType]mediaType{
	ContentTypeJSON:     mustMediaType("application/json"),
	ContentTypeHTML:     mustMediaType("text/html"),
	ContentTypeSSE:      mustMediaType("text/event-stream"),
	ContentTypeAtom:     mustMediaType("application/atom+xml"),
	ContentTypeJSONFeed: mustMediaType("application/feed+json"),
	ContentTypeJGF:      mustMediaType("application/vnd.jgf+json"),
	ContentTypeGraphML:  mustMediaType("application/graphml+xml"),
	ContentTypeDOT:      mustMediaType("text/vnd.graphviz"),
	ContentTypeCSV:      mustMediaType("text/csv;header=present"),
	ContentTypeYAML:     mustMediaType("application/yaml"),
}

// sending names what an endpoint sends for a type where that is not sentAs.
type sending map[ContentType]mediaType

func (s sending) as(ct ContentType) mediaType {
	if mt, ok := s[ct]; ok {
		return mt
	}

	return sentAs[ct]
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
			r.URL.RawPath = r.URL.RawPath[:rawOffset(r.URL.RawPath, len(trimmed))]

			return ext.ct, ext.ext
		}
	}
	return ContentTypeUnsupported, ""
}

// acceptable reports whether the request admits one of the types an endpoint
// serves.
func acceptable(r *http.Request, serves ...ContentType) bool {
	return acceptableAs(r, nil, serves...)
}

// acceptableAs is acceptable for an endpoint sending some type as sends says.
func acceptableAs(r *http.Request, sends sending, serves ...ContentType) bool {
	if slices.Contains(serves, negotiatedAmong(r, sends, serves...)) {
		return true
	}

	// No Accept at all is no preference, so any type will do.
	return extensionFromContext(r.Context()) == "" &&
		len(parseAcceptRanges(r.Header.Get("Accept"))) == 0
}

// negotiatedAmong is the best of the types an endpoint serves, weighed by what
// it sends for each. A suffix names one type, so it is not second-guessed;
// without an Accept the default stands, and with none served the client's
// favourite does, for the endpoint to refuse in terms of that type.
func negotiatedAmong(r *http.Request, sends sending, serves ...ContentType) ContentType {
	ct := ContentTypeFromContext(r.Context())
	if extensionFromContext(r.Context()) != "" {
		return ct
	}

	ranges := parseAcceptRanges(r.Header.Get("Accept"))
	if len(ranges) == 0 {
		return ct
	}

	if best := bestServed(ranges, serves, sends); best != ContentTypeUnsupported ||
		slices.Contains(serves, ct) {
		return best
	}

	return ct
}

// choose is negotiatedAmong, recorded on the request, for a dispatcher to hand
// the arm it picked: a handler choosing its own rendering off the context must
// read this choice, not the best of every endpoint's types.
func choose(
	r *http.Request,
	sends sending,
	serves ...ContentType,
) (*http.Request, ContentType) {
	ct := negotiatedAmong(r, sends, serves...)
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
	if len(ranges) == 0 {
		return true
	}

	weight, ok := weigh(ranges, ContentTypeHTML, sentAs[ContentTypeHTML])

	return ok && weight.q > 0
}

// governingRange returns the range that weighs a media type: the most
// specific one matching it, the first of equals (RFC 9110 §12.5.1).
func governingRange(ranges []mediaRange, mt mediaType) (mediaRange, bool) {
	var governing mediaRange
	found := false

	for _, mr := range ranges {
		if mr.matches(mt) && (!found || mr.specificity() > governing.specificity()) {
			governing, found = mr, true
		}
	}

	return governing, found
}

// weigh returns the range that weighs ct, sent as sent. Naming one of its
// aliases at a usable weight stands in for naming sent, unless sent is named
// itself: that says what the client thinks of what it would receive. A
// wildcard admits no stream: it asks for a document, not an endless connection.
func weigh(ranges []mediaRange, ct ContentType, sent mediaType) (mediaRange, bool) {
	governing, ok := governingRange(ranges, sent)
	if ok && governing.named() {
		return governing, true
	}

	var alias mediaRange
	aliased := false

	for _, sup := range supportedTypes {
		if sup.ct != ct || sup.typ == sent.typ && sup.subtype == sent.subtype {
			continue
		}

		named := mediaType{typ: sup.typ, subtype: sup.subtype, params: sent.params}
		if mr, found := governingRange(ranges, named); found && mr.named() && mr.q > 0 &&
			(!aliased || mr.outranks(alias)) {
			alias, aliased = mr, true
		}
	}

	switch {
	case aliased:
		return alias, true
	case ct == ContentTypeSSE:
		return mediaRange{}, false
	default:
		return governing, ok
	}
}

// mediaType is a type, subtype and parameters, names lower-cased.
type mediaType struct {
	typ     string
	subtype string
	params  map[string]string
}

// parseMediaType parses one media type or range with its parameters.
func parseMediaType(s string) (mediaType, bool) {
	full, params, err := mime.ParseMediaType(s)
	if err != nil {
		return mediaType{}, false
	}

	typ, subtype, ok := strings.Cut(full, "/")
	if !ok || typ == "" || subtype == "" {
		return mediaType{}, false
	}

	return mediaType{typ: typ, subtype: subtype, params: params}, true
}

func mustMediaType(s string) mediaType {
	mt, ok := parseMediaType(s)
	if !ok {
		panic("api: invalid media type " + s)
	}

	return mt
}

// carries reports whether the type has a parameter at this value. Every body
// this API sends is UTF-8, JSON by definition, so each carries that charset.
func (mt mediaType) carries(name, value string) bool {
	if name == "charset" {
		return strings.EqualFold(value, "utf-8")
	}

	got, ok := mt.params[name]

	return ok && got == value
}

// mediaRange is a parsed Accept header entry.
type mediaRange struct {
	mediaType

	q     float64 // quality value 0.0-1.0
	order int     // position in the Accept header (for tie-breaking)
}

// specificity ranks a range: a full wildcard below a partial one below a type,
// and each parameter above the same range without it (RFC 9110 §12.5.1).
func (mr mediaRange) specificity() int {
	level := 3
	switch {
	case mr.typ == "*":
		level = 1
	case mr.subtype == "*":
		level = 2
	}

	return level*100 + len(mr.params)
}

// named reports whether the range names a type rather than a wildcard.
func (mr mediaRange) named() bool {
	return mr.typ != "*" && mr.subtype != "*"
}

// outranks orders two ranges weighing different types: the heavier, then the
// more specific, then the one the client listed first.
func (mr mediaRange) outranks(other mediaRange) bool {
	if mr.q != other.q {
		return mr.q > other.q
	}

	if mr.specificity() != other.specificity() {
		return mr.specificity() > other.specificity()
	}

	return mr.order < other.order
}

// matches reports whether this range admits mt: its type and subtype, and
// every parameter it names, which mt must carry.
func (mr mediaRange) matches(mt mediaType) bool {
	switch {
	case mr.typ == "*" && mr.subtype == "*":
	case mr.typ == mt.typ && (mr.subtype == "*" || mr.subtype == mt.subtype):
	default:
		return false
	}

	for name, value := range mr.params {
		if !mt.carries(name, value) {
			return false
		}
	}

	return true
}

// parseAcceptRanges parses an Accept field value into its media ranges. An
// absent, empty or wholly unparsable header yields none, which every consumer
// reads as "no preference expressed". A malformed element is skipped.
func parseAcceptRanges(accept string) []mediaRange {
	elements := splitList(accept)
	ranges := make([]mediaRange, 0, len(elements))

	for i, element := range elements {
		mt, ok := parseMediaType(element)
		if !ok {
			continue
		}

		// Any parameter named q is the weight, wherever it sits.
		q := 1.0
		if v, ok := mt.params["q"]; ok {
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				q = parsed
			}
			delete(mt.params, "q")
		}

		ranges = append(ranges, mediaRange{mediaType: mt, q: q, order: i})
	}

	return ranges
}

// splitList splits a field value on the commas outside a quoted-string,
// dropping empty elements (RFC 9110 §5.6.1).
func splitList(value string) []string {
	var elements []string

	quoted, escaped, start := false, false, 0
	for i := 0; i <= len(value); i++ {
		if i < len(value) {
			c := value[i]
			switch {
			case escaped:
				escaped = false
				continue
			case quoted && c == '\\':
				escaped = true
				continue
			case c == '"':
				quoted = !quoted
				continue
			case c != ',' || quoted:
				continue
			}
		}

		if element := strings.TrimSpace(value[start:i]); element != "" {
			elements = append(elements, element)
		}
		start = i + 1
	}

	return elements
}

// bestMatch picks the supported type the given ranges prefer, defaulting to
// JSON when no preference was expressed and returning ContentTypeUnsupported
// when a preference was expressed that none of our types satisfies.
func bestMatch(ranges []mediaRange) ContentType {
	if len(ranges) == 0 {
		return ContentTypeJSON
	}

	return bestServed(ranges, nil, nil)
}

// bestServed is bestMatch among the types an endpoint serves, nil being all,
// each weighed by what sends says the endpoint sends for it.
func bestServed(ranges []mediaRange, serves []ContentType, sends sending) ContentType {
	var best mediaRange
	bestCT := ContentTypeUnsupported

	for _, ct := range contentTypeOrder {
		if serves != nil && !slices.Contains(serves, ct) {
			continue
		}

		mr, ok := weigh(ranges, ct, sends.as(ct))
		// A weight of zero means not acceptable.
		if !ok || mr.q <= 0 {
			continue
		}

		if bestCT == ContentTypeUnsupported || mr.outranks(best) {
			best, bestCT = mr, ct
		}
	}

	return bestCT
}
