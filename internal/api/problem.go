package api

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// ProblemDetail is an RFC 9457 problem details object.
type ProblemDetail struct {
	Context   string `json:"@context,omitempty"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

const problemJSONLDContext = "/api/context.jsonld"

// writeProblem writes an RFC 9457 problem details response with about:blank type.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	p := ProblemDetail{
		Context:   problemJSONLDContext,
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Instance:  r.URL.Path,
		RequestID: RequestIDFrom(r.Context()),
	}
	writeProblemJSON(w, p)
}

// writeProblemTyped writes an RFC 9457 problem details response with a domain-specific type URI.
// It fills in defaults for context, instance, and requestId if not already set.
func writeProblemTyped(w http.ResponseWriter, r *http.Request, p ProblemDetail) {
	if p.Context == "" {
		p.Context = problemJSONLDContext
	}
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	if p.Instance == "" {
		p.Instance = r.URL.Path
	}
	if p.RequestID == "" {
		p.RequestID = RequestIDFrom(r.Context())
	}
	writeProblemJSON(w, p)
}

func writeProblemJSON(w http.ResponseWriter, p ProblemDetail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
