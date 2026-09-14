package api

import (
	"log/slog"
	"net/http"
	"time"

	json "github.com/goccy/go-json"

	"github.com/radiergummi/cetacean/internal/auth"
)

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

	body := NewHealthResponse(status, h.operationsLevel)

	if h.liveness != nil {
		body = body.withWatcher(h.liveness.Liveness())
	}

	writeJSON(w, body)
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

// HandleResync triggers a manual full re-fetch of cluster state, behind a shim
// so this package does not import docker. Authenticated and behind a grant
// despite its /-/ prefix: each call sweeps the whole Docker API, unthrottled.
// Not gated on the operations level, though — a resync only re-reads.
func HandleResync(r Resyncer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		if err := r.Resync(req.Context()); err != nil {
			slog.Warn("manual resync failed", "error", err)
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}
		writeJSONStatus(w, http.StatusOK, map[string]any{
			"status":     "ok",
			"durationMs": time.Since(start).Milliseconds(),
		})
	}
}

// profilePath answers from the identity on the request, never from the cache.
const profilePath = "/profile"

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
	writeCachedJSON(w, r, NewDetailResponse(r.Context(), "/profile", "Profile", resp))
}
