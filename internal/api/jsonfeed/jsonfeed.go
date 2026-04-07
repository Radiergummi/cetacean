// Package jsonfeed implements JSON Feed 1.1 (https://www.jsonfeed.org/version/1.1/).
package jsonfeed

import "time"

const Version = "https://jsonfeed.org/version/1.1"

// Feed is a JSON Feed 1.1 document.
type Feed struct {
	Version     string   `json:"version"`
	Title       string   `json:"title"`
	HomePageURL string   `json:"home_page_url,omitempty"`
	FeedURL     string   `json:"feed_url,omitempty"`
	Description string   `json:"description,omitempty"`
	Authors     []Author `json:"authors,omitempty"`
	NextURL     string   `json:"next_url,omitempty"`
	Items       []Item   `json:"items"`
}

// Author is a JSON Feed author object.
type Author struct {
	Name string `json:"name"`
}

// Item is a single JSON Feed item.
type Item struct {
	ID           string    `json:"id"`
	URL          string    `json:"url,omitempty"`
	Title        string    `json:"title,omitempty"`
	ContentHTML  string    `json:"content_html,omitempty"`
	DateModified time.Time `json:"date_modified"`
	Tags         []string  `json:"tags,omitempty"`
}
