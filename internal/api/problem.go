package api

import (
	"net/http"

	json "github.com/goccy/go-json"
)

// ProblemDetail is an RFC 9457 problem details object.
type ProblemDetail struct {
	Context   problemContext `json:"@context,omitempty"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Status    int            `json:"status"`
	Detail    string         `json:"detail,omitempty"`
	Instance  string         `json:"instance,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
}

// problemContext is the shared context's URL. It serialises as that URL
// followed by problemContextTerms, which only a problem document may carry.
type problemContext string

func (c problemContext) MarshalJSON() ([]byte, error) {
	url, err := json.Marshal(string(c))
	if err != nil {
		return nil, err
	}

	buf := append([]byte{'['}, url...)
	buf = append(buf, ',')
	buf = append(buf, problemContextTerms...)

	return append(buf, ']'), nil
}

func (c *problemContext) UnmarshalJSON(data []byte) error {
	var list []json.RawMessage
	if err := json.Unmarshal(data, &list); err != nil || len(list) == 0 {
		return json.Unmarshal(data, (*string)(c))
	}

	return json.Unmarshal(list[0], (*string)(c))
}

// writeProblem writes an RFC 9457 problem details response with about:blank type.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	ctx := r.Context()
	p := ProblemDetail{
		Context:   problemContext(absPath(ctx, jsonLDContext)),
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		Instance:  absPath(ctx, r.URL.Path),
		RequestID: RequestIDFrom(ctx),
	}
	writeProblemJSON(w, p)
}

// writeProblemTyped writes an RFC 9457 problem details response with a domain-specific type URI.
// It fills in defaults for context, instance, and requestId if not already set.
func writeProblemTyped(w http.ResponseWriter, r *http.Request, p ProblemDetail) {
	ctx := r.Context()
	if p.Context == "" {
		p.Context = problemContext(absPath(ctx, jsonLDContext))
	}
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	if p.Instance == "" {
		p.Instance = absPath(ctx, r.URL.Path)
	}
	if p.RequestID == "" {
		p.RequestID = RequestIDFrom(ctx)
	}
	writeProblemJSON(w, p)
}

func writeProblemJSON(w http.ResponseWriter, p ProblemDetail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p) // best-effort: status already sent
}
