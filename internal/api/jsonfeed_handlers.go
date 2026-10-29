package api

import (
	"fmt"
	"net/http"

	"github.com/radiergummi/cetacean/internal/api/jsonfeed"
)

// renderJSONFeed converts format-agnostic feedData to a JSON Feed and writes it.
func renderJSONFeed(w http.ResponseWriter, r *http.Request, data feedData) {
	r = echoingFeedRequest(r, data)

	items := make([]jsonfeed.Item, 0, len(data.Entries))
	for _, e := range data.Entries {
		var authors []jsonfeed.Author
		if e.Author != "" {
			authors = []jsonfeed.Author{{Name: e.Author}}
		}

		items = append(items, jsonfeed.Item{
			ID:           e.ID,
			Title:        e.Title,
			ContentHTML:  e.ContentHTML,
			URL:          e.URL,
			DateModified: e.Updated,
			Authors:      authors,
			Tags:         e.Tags,
		})
	}

	// The subscription and the home page are the feed without a cursor on it.
	baseQuery := feedQuery(r, data.QueryParams)
	baseQuery.Del("before")
	baseQuery.Del("limit")

	feed := jsonfeed.Feed{
		Version:     jsonfeed.Version,
		Title:       data.Title,
		FeedURL:     feedHref(absURL(r, r.URL.Path+".feed"), baseQuery),
		HomePageURL: feedHref(absURL(r, r.URL.Path), baseQuery),
		Authors:     []jsonfeed.Author{{Name: "Cetacean"}},
		Items:       items,
	}

	if data.LastItemID > 0 && len(data.Entries) == data.Limit {
		nextQuery := feedQuery(r, data.QueryParams)
		nextQuery.Set("before", fmt.Sprintf("%d", data.LastItemID))
		nextQuery.Set("limit", fmt.Sprintf("%d", data.Limit))

		feed.NextURL = feedHref(absURL(r, r.URL.Path+".feed"), nextQuery)
	}

	w.Header().Set("Content-Type", "application/feed+json;charset=utf-8")
	varyByIdentity(w)
	writeCachedJSON(w, r, feed)
}
