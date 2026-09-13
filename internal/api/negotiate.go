package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
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

// htmlUnacceptable reports whether the client ruled out text/html — by naming
// an extension suffix that is not .html, or by an Accept header that does not
// admit it.
func htmlUnacceptable(ctx context.Context) bool {
	refused, _ := ctx.Value(htmlUnacceptableKey{}).(bool)

	return refused
}

// negotiate resolves the effective content type from an extension suffix or
// Accept header and stores it in the request context for downstream handlers.
//
// It resolves and records; it does not refuse. 406 is a statement about one
// endpoint, and the route is not known here.
func negotiate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept")

		ct, ext := resolveExtension(r)
		acceptsHTML := ct == ContentTypeHTML

		if ct == ContentTypeUnsupported {
			ranges := parseAcceptRanges(r.Header.Get("Accept"))
			ct = bestMatch(ranges)
			acceptsHTML = rangesAcceptHTML(ranges)
		}

		ctx := context.WithValue(r.Context(), contentTypeKey{}, ct)
		if ext != "" {
			ctx = context.WithValue(ctx, extensionKey{}, ext)
		}

		if !acceptsHTML {
			ctx = context.WithValue(ctx, htmlUnacceptableKey{}, true)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
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

// extensionTypes maps URL extension suffixes to content types.
// Extension suffix takes priority over the Accept header.
var extensionTypes = []struct {
	ext string
	ct  ContentType
}{
	{".json", ContentTypeJSON},
	{".html", ContentTypeHTML},
	{".atom", ContentTypeAtom},
	{".feed", ContentTypeJSONFeed},
	{".jgf", ContentTypeJGF},
	{".graphml", ContentTypeGraphML},
	{".dot", ContentTypeDOT},
	{".csv", ContentTypeCSV},
	{".yaml", ContentTypeYAML},
	{".yml", ContentTypeYAML},
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
		if strings.Contains(path, ext.ext+"/") {
			return true
		}
	}

	return false
}

// resolveExtension checks for a known extension suffix on the request path.
// If found, it strips the suffix from r.URL.Path and returns the content type
// along with the suffix it removed. Returns ContentTypeUnsupported if no
// extension matches.
func resolveExtension(r *http.Request) (ContentType, string) {
	path := r.URL.Path
	if literalDocuments[path] {
		return ContentTypeUnsupported, ""
	}

	for _, ext := range extensionTypes {
		if trimmed, ok := strings.CutSuffix(path, ext.ext); ok {
			r.URL.Path = trimmed
			return ext.ct, ext.ext
		}
	}
	return ContentTypeUnsupported, ""
}

// rangesAcceptHTML reports whether the ranges admit text/html at a usable
// quality (RFC 9110 §12.5.1): the most specific matching range carries the
// weight, so "*/*, text/html;q=0" refuses it. The resolved ContentType cannot
// answer this — it reports JSON for a wildcard and an explicit type alike.
func rangesAcceptHTML(ranges []mediaRange) bool {
	if len(ranges) == 0 {
		return true
	}

	specificity, quality := -1, 0.0

	for _, mr := range ranges {
		if !mr.matches("text", "html") {
			continue
		}

		if s := mr.specificity(); s > specificity {
			specificity, quality = s, mr.q
		}
	}

	return quality > 0
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

	var ranges []mediaRange
	for i, part := range strings.Split(accept, ",") {
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
				if strings.HasPrefix(param, "q=") {
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
		typ := mediaType[:slash]
		subtype := mediaType[slash+1:]

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

	// For each supported type, find the best matching range.
	type candidate struct {
		ct          ContentType
		q           float64
		specificity int
		order       int // header position of the matching range
	}

	var best *candidate

	for _, sup := range supportedTypes {
		for _, mr := range ranges {
			// RFC 9110 §12.5.1: a weight of zero means not acceptable.
			if mr.q <= 0 || !mr.matches(sup.typ, sup.subtype) {
				continue
			}

			spec := mr.specificity()
			c := candidate{
				ct:          sup.ct,
				q:           mr.q,
				specificity: spec,
				order:       mr.order,
			}

			if best == nil {
				best = &c
				continue
			}

			// Higher quality wins.
			if c.q > best.q {
				best = &c
			} else if c.q == best.q {
				// Same quality: higher specificity wins.
				if c.specificity > best.specificity {
					best = &c
				} else if c.specificity == best.specificity {
					// Same specificity: earlier in header wins.
					if c.order < best.order {
						best = &c
					}
				}
			}
		}
	}

	if best == nil {
		return ContentTypeUnsupported
	}
	return best.ct
}
