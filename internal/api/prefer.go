package api

import (
	"net/http"
	"strings"
)

// preferMinimal returns true if the request carries a Prefer header
// containing the "return=minimal" preference token (RFC 7240 §4.2).
func preferMinimal(r *http.Request) bool {
	for _, v := range r.Header.Values("Prefer") {
		for token := range strings.SplitSeq(v, ",") {
			if strings.TrimSpace(token) == "return=minimal" {
				return true
			}
		}
	}
	return false
}

// writePreferMinimal sends a 204 No Content response with the
// Preference-Applied header confirming the server honored the
// return=minimal preference (RFC 7240 §3).
func writePreferMinimal(w http.ResponseWriter) {
	w.Header().Set("Preference-Applied", "return=minimal")
	w.WriteHeader(http.StatusNoContent)
}

// writePreferCreated sends a 201 Created response with the
// Preference-Applied header but no body (RFC 7240 §4.2).
// The caller should set the Location header before calling this.
func writePreferCreated(w http.ResponseWriter) {
	w.Header().Set("Preference-Applied", "return=minimal")
	w.WriteHeader(http.StatusCreated)
}

// writeMutationResponse checks for Prefer: return=minimal and either
// sends a 204 No Content or writes the full JSON response body.
func writeMutationResponse(w http.ResponseWriter, r *http.Request, v any) {
	if preferMinimal(r) {
		writePreferMinimal(w)
		return
	}
	writeJSON(w, v)
}
