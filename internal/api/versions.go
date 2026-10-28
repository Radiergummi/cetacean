package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/cluster"
)

// versionedSpec groups a config or secret list into versions: id names an
// item, and view groups the items one caller may read.
type versionedSpec[T any] struct {
	id   func(T) string
	view func(readable []T, canRead func(string) bool) cluster.VersionView
}

// versionSelection reads ?versions and ?versionsOf. A CSV export holds every
// version unless ?versions says otherwise.
func versionSelection(r *http.Request) (cluster.VersionSelection, error) {
	query := r.URL.Query()

	sel, err := cluster.ParseVersionSelection(
		query.Get("versions"),
		query.Get("versionsOf"),
		cluster.Narrowing{Search: query.Get("search"), Filter: query.Get("filter")},
	)
	if err == nil && ContentTypeFromContext(r.Context()) == ContentTypeCSV &&
		query.Get("versions") == "" {
		sel.All = true
	}

	return sel, err
}
