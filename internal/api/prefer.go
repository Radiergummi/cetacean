package api

import (
	"net/http"
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

// preference splits one Prefer token into its name and its value. Names are
// case-insensitive (RFC 7240 §2) and come back lowercased; the
// preference-parameters after the first ";" are dropped, since this server
// understands none of them, and a quoted-string value loses its quotes.
func preference(token string) (name, value string) {
	token, _, _ = strings.Cut(token, ";")

	name, value, _ = strings.Cut(token, "=")
	name = strings.ToLower(strings.TrimSpace(name))
	value = strings.TrimSpace(value)

	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		value = value[1 : len(value)-1]
	}

	return name, value
}

// preferMinimal returns true if the request carries a Prefer header
// containing the "return=minimal" preference token (RFC 7240 §4.2).
func preferMinimal(r *http.Request) bool {
	for _, token := range preferTokens(r) {
		// RFC 7240 §2: names compare case-insensitively, values case-sensitively.
		// So "Return=minimal" is this preference and "return=MINIMAL" is not.
		if name, value := preference(token); name == "return" && value == "minimal" {
			return true
		}
	}

	return false
}

// preferWait returns the RFC 7240 §4.3 wait preference in seconds, clamped to
// cluster.ConvergenceTimeout. A malformed or negative value is ignored rather
// than rejected: §2 says an unparseable preference is simply not applied.
func preferWait(r *http.Request) (time.Duration, bool) {
	for _, token := range preferTokens(r) {
		name, value := preference(token)
		if name != "wait" {
			continue
		}

		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 0 {
			return 0, false
		}

		// Clamp before multiplying: a large enough second count overflows
		// int64 and comes back negative, which min would then keep.
		capped := min(seconds, int(cluster.ConvergenceTimeout/time.Second))

		return time.Duration(capped) * time.Second, true
	}

	return 0, false
}

// preferRespondAsync reports the RFC 7240 §4.1 respond-async preference.
func preferRespondAsync(r *http.Request) bool {
	for _, token := range preferTokens(r) {
		if name, _ := preference(token); name == "respond-async" {
			return true
		}
	}

	return false
}

// applyPreference records one preference the server honoured (RFC 7240 §3).
// It adds rather than sets, because one response may honour several: a mutation
// carrying "return=minimal, wait=30" waits and then answers 204. No caller may
// set Preference-Applied directly, or the result would depend on ordering.
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
