package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// versionedList narrows spec's readable items by ?versions and ?versionsOf,
// gives its CSV rows their version columns, and returns what each item of the
// JSON page gains once rendered. A CSV export holds every version.
func versionedList[T any](
	h *Handlers,
	r *http.Request,
	spec *listSpec[T],
	id func(T) string,
	view func(readable []T, canRead func(string) bool) cluster.VersionView,
) *cluster.VersionView {
	versions := new(cluster.VersionView)

	// Runs only once a page is rendered, after a 304 would have answered.
	spec.narrow = func(readable []T) ([]T, error) {
		query := r.URL.Query()
		searching := query.Get("search") != "" || query.Get("filter") != ""

		sel, err := cluster.ParseVersionSelection(
			query.Get("versions"), query.Get("versionsOf"), searching,
		)
		if err != nil {
			return nil, err
		}

		if ContentTypeFromContext(r.Context()) == ContentTypeCSV {
			sel.All = true
		}

		*versions = view(readable, h.readChecker(r))

		return cluster.SelectVersions(readable, id, versions.Groups, sel), nil
	}

	rows := spec.rows
	spec.rows = func(page []T) []cluster.Row {
		out := rows(page)
		cluster.AnnotateVersionRows(out, *versions)

		return out
	}

	return versions
}
