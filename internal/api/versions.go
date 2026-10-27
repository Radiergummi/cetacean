package api

import (
	"net/http"
	"sync"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// versionedList narrows spec's readable items by ?versions and ?versionsOf,
// gives its CSV rows their version columns, and returns what each item of the
// JSON page gains. A CSV export holds every version.
func versionedList[T any](
	h *Handlers,
	r *http.Request,
	spec *listSpec[T],
	id func(T) string,
	group func(readable []T) cluster.VersionGroups,
	users func(id string) []cache.ServiceRef,
) func(id string) cluster.VersionFields {
	var groups cluster.VersionGroups

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

		groups = group(readable)

		return cluster.SelectVersions(readable, id, groups, sel), nil
	}

	// Built on first use: a 304 answers before anything is rendered.
	canRead := sync.OnceValue(func() func(string) bool { return h.readChecker(r) })

	rows := spec.rows
	spec.rows = func(page []T) []cluster.Row {
		out := rows(page)
		cluster.AnnotateVersionRows(out, groups, users, canRead())

		return out
	}

	return func(itemID string) cluster.VersionFields {
		return groups.Version(itemID).Fields(cluster.ReadableUsers(users(itemID), canRead()))
	}
}
