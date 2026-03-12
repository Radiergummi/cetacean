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

	// ContentTypeUnsupported means the client explicitly asked for a type we
	// cannot provide. Dispatch helpers should return 406 Not Acceptable.
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
	case ContentTypeUnsupported:
		return "Unsupported"
	default:
		return "Unknown"
	}
}

const contentTypeKey ctxKey = 1

// ContentTypeFromContext returns the negotiated content type, defaulting to JSON.
func ContentTypeFromContext(ctx context.Context) ContentType {
	if ct, ok := ctx.Value(contentTypeKey).(ContentType); ok {
		return ct
	}
	return ContentTypeJSON
}

// negotiate resolves the effective content type from an extension suffix or
// Accept header and stores it in the request context for downstream handlers.
func negotiate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "Accept")

		ct := ContentTypeJSON
		path := r.URL.Path

		// Extension suffix takes priority over Accept header.
		if strings.HasSuffix(path, ".json") {
			ct = ContentTypeJSON
			path = strings.TrimSuffix(path, ".json")
			r.URL.Path = path
		} else if strings.HasSuffix(path, ".html") {
			ct = ContentTypeHTML
			path = strings.TrimSuffix(path, ".html")
			r.URL.Path = path
		} else {
			ct = parseAccept(r.Header.Get("Accept"))
		}

		if ct == ContentTypeUnsupported {
			writeProblem(w, r, http.StatusNotAcceptable, "no supported media type in Accept header")
			return
		}

		ctx := context.WithValue(r.Context(), contentTypeKey, ct)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
	{"text", "html", ContentTypeHTML},
	{"application", "xhtml+xml", ContentTypeHTML},
	{"text", "event-stream", ContentTypeSSE},
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

// parseAccept parses an Accept header value per RFC 7231 Section 5.3.2 and
// returns the best matching ContentType. Returns ContentTypeUnsupported when
// the header contains media types but none match our supported types.
func parseAccept(accept string) ContentType {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return ContentTypeJSON
	}

	// Parse all media ranges from the header.
	var ranges []mediaRange
	for i, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Split media type from parameters (;q=...)
		mediaType := part
		q := 1.0
		if idx := strings.Index(part, ";"); idx != -1 {
			mediaType = strings.TrimSpace(part[:idx])
			params := part[idx+1:]
			for _, param := range strings.Split(params, ";") {
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

	// No valid ranges parsed — treat as empty Accept (default JSON).
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
			if !mr.matches(sup.typ, sup.subtype) {
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
