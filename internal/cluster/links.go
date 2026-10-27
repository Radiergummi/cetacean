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
// duplicates of title and value merged.
func StackLinks(services []swarm.Service) []ServiceLink {
	type key struct{ title, href, text string }

	var links []ServiceLink

	seen := make(map[key]bool)

	for _, svc := range services {
		for _, link := range ServiceLinks(svc.Spec.Labels) {
			k := key{link.Title, link.Href, link.Text}
			if seen[k] {
				continue
			}

			seen[k] = true
			links = append(links, link)
		}
	}

	slices.SortFunc(links, func(a, b ServiceLink) int {
		return cmp.Or(
			strings.Compare(a.ID, b.ID),
			strings.Compare(a.Href+a.Text, b.Href+b.Text),
		)
	})

	return links
}

func isHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return false
	}

	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}
