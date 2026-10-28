package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/radiergummi/cetacean/internal/api/problem"
)

// HandleRevokeSubject withdraws every client the subject authorized. It is
// gated as the join tokens are: these are credentials into the cluster too.
func HandleRevokeSubject(revoker TokenRevoker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if revoker == nil {
			problem.WriteStatus(
				w,
				r,
				http.StatusNotFound,
				"no authorization server issues tokens here",
			)
			return
		}

		subject := r.PathValue("subject")
		revoked := revoker.RevokeSubject(subject)

		slog.InfoContext(
			r.Context(),
			"revoked a subject's token grants",
			"subject",
			subject,
			"grants",
			revoked,
		)

		w.Header().Set("Revoked-Grants", strconv.Itoa(revoked))
		w.WriteHeader(http.StatusNoContent)
	}
}
