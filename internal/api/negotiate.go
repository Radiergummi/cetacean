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
)

func (ct ContentType) String() string {
	switch ct {
	case ContentTypeJSON:
		return "JSON"
	case ContentTypeHTML:
		return "HTML"
	case ContentTypeSSE:
		return "SSE"
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

		ctx := context.WithValue(r.Context(), contentTypeKey, ct)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// parseAccept parses an Accept header value and returns the best matching ContentType.
func parseAccept(accept string) ContentType {
	accept = strings.TrimSpace(accept)
	if accept == "" {
		return ContentTypeJSON
	}

	type entry struct {
		ct ContentType
		q  float64
	}

	var best entry
	best.q = -1

	for _, part := range strings.Split(accept, ",") {
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

		var ct ContentType
		matched := true
		switch mediaType {
		case "application/json", "application/vnd.cetacean.v1+json":
			ct = ContentTypeJSON
		case "text/html", "application/xhtml+xml":
			ct = ContentTypeHTML
		case "text/event-stream":
			ct = ContentTypeSSE
		case "*/*":
			ct = ContentTypeJSON
		default:
			matched = false
		}

		if matched && q > best.q {
			best = entry{ct: ct, q: q}
		}
	}

	if best.q < 0 {
		return ContentTypeJSON
	}
	return best.ct
}
