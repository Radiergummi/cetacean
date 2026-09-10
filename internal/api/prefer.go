package api

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// preferTokens yields each comma-separated preference token across every
// Prefer field, since RFC 7240 allows both forms.
func preferTokens(r *http.Request) []string {
	var tokens []string
	for _, v := range r.Header.Values("Prefer") {
		for token := range strings.SplitSeq(v, ",") {
			tokens = append(tokens, strings.TrimSpace(token))
		}
	}

	return tokens
}

// preferMinimal returns true if the request carries a Prefer header
// containing the "return=minimal" preference token (RFC 7240 §4.2).
func preferMinimal(r *http.Request) bool {
	return slices.Contains(preferTokens(r), "return=minimal")
}

// preferWait returns the RFC 7240 §4.3 wait preference in seconds, clamped to
// cluster.ConvergenceTimeout. A malformed or negative value is ignored rather
// than rejected: §2 says an unparseable preference is simply not applied. The
// value may be a bare token or a quoted-string (RFC 7240 §2), so a single
// pair of surrounding double quotes is stripped before parsing.
func preferWait(r *http.Request) (time.Duration, bool) {
	for _, token := range preferTokens(r) {
		name, value, found := strings.Cut(token, "=")
		if !found || strings.TrimSpace(name) != "wait" {
			continue
		}

		value = strings.TrimSpace(value)
		if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}

		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 0 {
			return 0, false
		}

		wait := min(time.Duration(seconds)*time.Second, cluster.ConvergenceTimeout)

		return wait, true
	}

	return 0, false
}

// preferRespondAsync reports the RFC 7240 §4.1 respond-async preference.
func preferRespondAsync(r *http.Request) bool {
	return slices.Contains(preferTokens(r), "respond-async")
}

// applyPreference records one preference the server honoured (RFC 7240 §3).
//
// Every site that reports a preference goes through here, and it always adds
// rather than sets. Preference-Applied is a list-valued field, so repeated
// field lines are equivalent to one comma-joined value, and a single response
// may honour more than one preference: a service mutation carrying
// "return=minimal, wait=30" waits, then answers 204, and both are true of it.
// Setting would make the result depend on which site ran last — an ordering
// invariant no type enforces — so no caller may set this header directly.
func applyPreference(w http.ResponseWriter, token string) {
	w.Header().Add("Preference-Applied", token)
}

// writePreferMinimal sends a 204 No Content response with the
// Preference-Applied header confirming the server honored the
// return=minimal preference (RFC 7240 §3).
func writePreferMinimal(w http.ResponseWriter) {
	applyPreference(w, "return=minimal")
	w.WriteHeader(http.StatusNoContent)
}

// writePreferCreated sends a 201 Created response with the
// Preference-Applied header but no body (RFC 7240 §4.2).
// The caller should set the Location header before calling this.
func writePreferCreated(w http.ResponseWriter) {
	applyPreference(w, "return=minimal")
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
