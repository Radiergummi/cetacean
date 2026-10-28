package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

// HandleErrorIndex serves the list of all well-known error codes.
func HandleErrorIndex(w http.ResponseWriter, r *http.Request) {
	defs := problem.Defs()

	writeCachedJSON(w, r, NewCollectionResponse(r.Context(), defs, len(defs), len(defs), 0))
}

// HandleErrorDetail serves documentation for a single error code.
func HandleErrorDetail(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	def, ok := problem.Lookup(code)
	if !ok {
		problem.WriteStatus(w, r, http.StatusNotFound, "no error code "+code)
		return
	}

	writeCachedJSON(w, r, NewDetailResponse(r.Context(), "/api/errors/"+code, "ErrorDef", def))
}
