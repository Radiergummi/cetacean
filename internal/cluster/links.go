package cluster

import (
	"cmp"
	"net/url"
	"slices"
	"strings"

	"github.com/docker/docker/api/types/swarm"
)

const (
	linkLabelPrefix = "cetacean.link."
	linkTitleSuffix = ".title"
)

// ServiceLink is one custom link declared on a service's labels. Exactly one
// of Href and Text is set: Href only for an http(s) URL, so a consumer that
// renders Href as a link can never be handed another scheme.
type ServiceLink struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Href  string `json:"href,omitempty"`
	Text  string `json:"text,omitempty"`
}

// ServiceLinks reads the cetacean.link.<id>=<url> labels, with the optional
// cetacean.link.<id>.title, sorted by id. A title without a value is ignored.
func ServiceLinks(labels map[string]string) []ServiceLink {
	var links []ServiceLink

	for key, value := range labels {
		id, ok := strings.CutPrefix(key, linkLabelPrefix)
		if !ok || id == "" || value == "" || strings.HasSuffix(id, linkTitleSuffix) {
			continue
		}

		title := labels[key+linkTitleSuffix]
		if title == "" {
			title = id
		}

		link := ServiceLink{ID: id, Title: title}
		if isHTTPURL(value) {
			link.Href = value
		} else {
			link.Text = value
		}

		links = append(links, link)
	}

	slices.SortFunc(links, func(a, b ServiceLink) int { return strings.Compare(a.ID, b.ID) })

	return links
}

// StackLinks is the union of its member services' links, with exact
// duplicates merged. The order is total, so the response's ETag is stable.
func StackLinks(services []swarm.Service) []ServiceLink {
	var links []ServiceLink

	for _, svc := range services {
		links = append(links, ServiceLinks(svc.Spec.Labels)...)
	}

	slices.SortFunc(links, func(a, b ServiceLink) int {
		return cmp.Or(
			strings.Compare(a.ID, b.ID),
			strings.Compare(a.Href, b.Href),
			strings.Compare(a.Text, b.Text),
			strings.Compare(a.Title, b.Title),
		)
	})

	return slices.Compact(links)
}

// isHTTPURL accepts an absolute http(s) URL with a hostname, written only in
// the characters RFC 3986 allows, so an href is always a valid URI.
func isHTTPURL(value string) bool {
	if strings.ContainsFunc(value, func(r rune) bool { return !isURIChar(r) }) {
		return false
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return false
	}

	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}

func isURIChar(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' ||
		strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=%", r)
}
