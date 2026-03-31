package api

import (
	"net/http"
	"time"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/api/sse"
	"github.com/radiergummi/cetacean/internal/auth"
	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/recommendations"
)

func (h *Handlers) HandleRecommendations(w http.ResponseWriter, r *http.Request) {
	if !h.requireAnyGrant(w, r) {
		return
	}
	results := h.recEngine.Results()
	summary := recommendations.ComputeSummary(results)
	writeJSONWithETag(
		w,
		r,
		NewDetailResponse(
			r.Context(),
			"/recommendations",
			"RecommendationCollection",
			RecommendationsResponse{
				Items:      results,
				Total:      len(results),
				Summary:    summary,
				ComputedAt: h.recEngine.LastTick(),
			},
		),
	)
}

func (h *Handlers) streamList(w http.ResponseWriter, r *http.Request, typ cache.EventType) {
	typMatch := sse.TypeMatcher(typ)
	h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, typMatch))
}

func (h *Handlers) streamResource(
	w http.ResponseWriter, r *http.Request, typ cache.EventType, id string,
) {
	resMatch := sse.ResourceMatcher(typ, id)
	h.broadcaster.ServeSSE(w, r, h.aclMatchWrap(r, resMatch))
}

// aclMatchWrap wraps an SSE match function with an ACL authorization check.
// Events that pass the type/resource matcher are further filtered by ACL.
// Sync events always pass through.
func (h *Handlers) aclMatchWrap(r *http.Request, inner func(cache.Event) bool) func(cache.Event) bool {
	id := auth.IdentityFromContext(r.Context())
	return func(ev cache.Event) bool {
		if inner != nil && !inner(ev) {
			return false
		}
		if ev.Type == cache.EventSync {
			return true
		}
		return h.acl.Can(id, "read", string(ev.Type)+":"+ev.Name)
	}
}

func (h *Handlers) isReady() bool {
	select {
	case <-h.ready:
		return true
	default:
		return false
	}
}

func (h *Handlers) HandleHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	if !h.isReady() {
		status = "error"
	}

	writeJSON(w, NewHealthResponse(status, h.operationsLevel))
}

func (h *Handlers) HandleReady(w http.ResponseWriter, r *http.Request) {
	if !h.isReady() {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // best-effort: status already sent
}

// HandleProfile returns the authenticated user's identity as JSON.
// Registered with content negotiation so /profile serves the SPA for
// browsers and JSON for API clients (/profile.json or Accept: application/json).
func (h *Handlers) HandleProfile(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFromContext(r.Context())
	if id == nil {
		writeErrorCode(w, r, "AUT001", "not authenticated")
		return
	}

	type profileResponse struct {
		*auth.Identity
		Permissions map[string][]string `json:"permissions,omitempty"`
	}
	resp := profileResponse{Identity: id}
	resp.Permissions = h.acl.PermissionsFor(id)

	w.Header().Set("Cache-Control", "no-store")
	writeJSONWithETag(w, r, resp)
}
