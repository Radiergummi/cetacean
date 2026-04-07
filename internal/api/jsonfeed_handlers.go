package api

import (
	"fmt"
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/jsonfeed"
)

// renderJSONFeed converts format-agnostic feedData to a JSON Feed and writes it.
func renderJSONFeed(w http.ResponseWriter, r *http.Request, data feedData) {
	items := make([]jsonfeed.Item, 0, len(data.Entries))
	for _, e := range data.Entries {
		items = append(items, jsonfeed.Item{
			ID:           e.ID,
			Title:        e.Title,
			ContentHTML:  e.ContentHTML,
			URL:          e.URL,
			DateModified: e.Updated,
			Tags:         e.Tags,
		})
	}

	feed := jsonfeed.Feed{
		Version:     jsonfeed.Version,
		Title:       data.Title,
		FeedURL:     absURL(r, r.URL.Path+".feed"),
		HomePageURL: absURL(r, r.URL.Path),
		Authors:     []jsonfeed.Author{{Name: "Cetacean"}},
		Items:       items,
	}

	if data.LastItemID > 0 && len(data.Entries) == data.Limit {
		q := r.URL.Query()
		q.Set("before", fmt.Sprintf("%d", data.LastItemID))
		q.Set("limit", fmt.Sprintf("%d", data.Limit))
		feed.NextURL = absURL(r, r.URL.Path+".feed") + "?" + q.Encode()
	}

	w.Header().Set("Content-Type", "application/feed+json;charset=utf-8")
	writeCachedJSON(w, r, feed)
}
