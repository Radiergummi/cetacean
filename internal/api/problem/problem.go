package problem

import (
	"errors"
	"net/http"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/reqctx"
)

// ContextPath is the JSON-LD context every response, a problem included, names.
const ContextPath = "/api/context.jsonld"

// contextTerms map a problem's members to RFC 9457, which only a problem
// document may carry.
const contextTerms = `{` +
	`"type":{"@id":"urn:ietf:rfc:9457#type","@type":"@id"},` +
	`"title":"urn:ietf:rfc:9457#title",` +
	`"status":"urn:ietf:rfc:9457#status",` +
	`"detail":"urn:ietf:rfc:9457#detail",` +
	`"instance":{"@id":"urn:ietf:rfc:9457#instance","@type":"@id"}` +
	`}`

// Detail is an RFC 9457 problem details object.
type Detail struct {
	Context   contextRef `json:"@context,omitempty"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Status    int        `json:"status"`
	Detail    string     `json:"detail,omitempty"`
	Instance  string     `json:"instance,omitempty"`
	RequestID string     `json:"requestId,omitempty"`
}

// contextRef is the shared context's URL. It serialises as that URL
// followed by contextTerms.
type contextRef string

func (c contextRef) MarshalJSON() ([]byte, error) {
	url, err := json.Marshal(string(c))
	if err != nil {
		return nil, err
	}

	buf := append([]byte{'['}, url...)
	buf = append(buf, ',')
	buf = append(buf, contextTerms...)

	return append(buf, ']'), nil
}

// UnmarshalJSON accepts only the array form, so a decoder in a test fails if
// the wire form ever regresses to a bare string.
func (c *contextRef) UnmarshalJSON(data []byte) error {
	var list []json.RawMessage
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	if len(list) == 0 {
		return errors.New("problem @context: empty array")
	}

	return json.Unmarshal(list[0], (*string)(c))
}

// WriteStatus writes an RFC 9457 problem details response with about:blank type.
func WriteStatus(w http.ResponseWriter, r *http.Request, status int, detail string) {
	ctx := r.Context()
	p := Detail{
		Context:   contextRef(reqctx.AbsPath(ctx, ContextPath)),
		Type:      "about:blank",
		Title:     statusPhrase(status),
		Status:    status,
		Detail:    detail,
		Instance:  reqctx.AbsPath(ctx, r.URL.Path),
		RequestID: reqctx.RequestID(ctx),
	}
	writeJSON(w, p)
}

// WriteTyped writes an RFC 9457 problem details response with a domain-specific type URI.
// It fills in defaults for context, instance, and requestId if not already set.
func WriteTyped(w http.ResponseWriter, r *http.Request, p Detail) {
	ctx := r.Context()
	if p.Context == "" {
		p.Context = contextRef(reqctx.AbsPath(ctx, ContextPath))
	}
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Title == "" {
		p.Title = statusPhrase(p.Status)
	}
	if p.Instance == "" {
		p.Instance = reqctx.AbsPath(ctx, r.URL.Path)
	}
	if p.RequestID == "" {
		p.RequestID = reqctx.RequestID(ctx)
	}
	writeJSON(w, p)
}

// statusPhrase is the reason phrase RFC 9110 §15 recommends, where net/http
// still carries the one an obsoleted RFC gave.
func statusPhrase(status int) string {
	switch status {
	case http.StatusRequestEntityTooLarge:
		return "Content Too Large"
	case http.StatusRequestedRangeNotSatisfiable:
		return "Range Not Satisfiable"
	case http.StatusUnprocessableEntity:
		return "Unprocessable Content"
	default:
		return http.StatusText(status)
	}
}

func writeJSON(w http.ResponseWriter, p Detail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p) // best-effort: status already sent
}
