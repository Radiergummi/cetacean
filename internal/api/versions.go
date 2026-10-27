package api

import (
	"net/http"

	"github.com/radiergummi/cetacean/internal/cache"
	"github.com/radiergummi/cetacean/internal/cluster"
)

// VersionFields is what a config or secret list item adds: its place among the
// versions the caller may read, and how many services it may read use it.
type VersionFields struct {
	VersionOf       string `json:"VersionOf,omitempty"`
	VersionInferred bool   `json:"VersionInferred,omitempty"`
	NewestVersion   bool   `json:"NewestVersion,omitempty"`
	OlderVersions   int    `json:"OlderVersions,omitempty"`
	UsedBy          int    `json:"UsedBy"`
}

// versionedList narrows spec's readable items by ?versions and ?versionsOf,
// and returns what each item of the page it renders gains.
func versionedList[T any](
	h *Handlers,
	r *http.Request,
	spec *listSpec[T],
	id func(T) string,
	group func(readable []T) cluster.VersionGroups,
	users func(id string) []cache.ServiceRef,
) func(id string) VersionFields {
	var groups cluster.VersionGroups

	spec.narrow = func(readable []T) ([]T, error) {
		query := r.URL.Query()

		sel, err := cluster.ParseVersionSelection(query.Get("versions"), query.Get("versionsOf"))
		if err != nil {
			return nil, err
		}

		groups = group(readable)

		return cluster.SelectVersions(readable, id, groups, sel), nil
	}

	canRead := h.readChecker(r)

	return func(itemID string) VersionFields {
		return versionFields(groups.Version(itemID), cluster.ReadableUsers(users(itemID), canRead))
	}
}

func versionFields(v cluster.Version, usedBy int) VersionFields {
	return VersionFields{
		VersionOf:       v.Of,
		VersionInferred: v.Inferred,
		NewestVersion:   v.Newest,
		OlderVersions:   v.OlderVersions,
		UsedBy:          usedBy,
	}
}
