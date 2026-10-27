package cluster

import (
	"cmp"
	"net/netip"
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

	// Services names the member services declaring the link. Stack links only.
	Services []string `json:"services,omitempty"`
}

// ServiceLinks reads the cetacean.link.<id>=<url> labels, with the optional
// cetacean.link.<id>.title, sorted by id. A title without a value is ignored.
func ServiceLinks(labels map[string]string) []ServiceLink {
	var links []ServiceLink

	for key, value := range labels {
		id, ok := strings.CutPrefix(key, linkLabelPrefix)
		value = strings.TrimSpace(value)

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

// StackLinks is the union of its member services' links. Exact duplicates
// merge into one link naming every service that declares it. The order is
// total, so the response's ETag is stable.
func StackLinks(services []swarm.Service) []ServiceLink {
	type key struct{ id, title, href, text string }

	declaredBy := make(map[key][]string)

	for _, svc := range services {
		for _, link := range ServiceLinks(svc.Spec.Labels) {
			k := key{link.ID, link.Title, link.Href, link.Text}
			declaredBy[k] = append(declaredBy[k], svc.Spec.Name)
		}
	}

	links := make([]ServiceLink, 0, len(declaredBy))

	for k, names := range declaredBy {
		slices.Sort(names)
		links = append(links, ServiceLink{
			ID: k.id, Title: k.title, Href: k.href, Text: k.text, Services: names,
		})
	}

	slices.SortFunc(links, func(a, b ServiceLink) int {
		return cmp.Or(
			strings.Compare(a.ID, b.ID),
			strings.Compare(a.Href, b.Href),
			strings.Compare(a.Text, b.Text),
			strings.Compare(a.Title, b.Title),
		)
	})

	if len(links) == 0 {
		return nil
	}

	return links
}

// isHTTPURL accepts only an absolute http(s) URL that is a valid URI by the
// RFC 3986 grammar, with a non-empty host, so an href is always well-formed.
func isHTTPURL(value string) bool {
	scheme, rest, ok := strings.Cut(value, "://")
	if !ok || !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") {
		return false
	}

	rest, fragment, hasFragment := strings.Cut(rest, "#")
	if hasFragment && !validURIPart(fragment, ":@/?") {
		return false
	}

	rest, query, hasQuery := strings.Cut(rest, "?")
	if hasQuery && !validURIPart(query, ":@/?") {
		return false
	}

	authority, path := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority, path = rest[:i], rest[i:]
	}

	return validURIPart(path, ":@/") && validAuthority(authority)
}

// validAuthority checks [userinfo "@"] host [":" port], where host is a
// bracketed IPv6 literal or a non-empty reg-name.
func validAuthority(authority string) bool {
	if i := strings.LastIndexByte(authority, '@'); i >= 0 {
		if !validURIPart(authority[:i], ":") {
			return false
		}

		authority = authority[i+1:]
	}

	var host, port string

	if literal, ok := strings.CutPrefix(authority, "["); ok {
		var after string

		host, after, ok = strings.Cut(literal, "]")
		if !ok {
			return false
		}

		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return false
		}

		if after != "" {
			if port, ok = strings.CutPrefix(after, ":"); !ok {
				return false
			}
		}
	} else {
		host, port, _ = strings.Cut(authority, ":")
		if !validURIPart(host, "") {
			return false
		}
	}

	return host != "" &&
		!strings.ContainsFunc(port, func(r rune) bool { return r < '0' || r > '9' })
}

// validURIPart reports whether s holds only unreserved characters, sub-delims,
// well-formed percent-escapes, and the extra characters its component allows.
func validURIPart(s, extra string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case c == '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return false
			}

			i += 2
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		case strings.IndexByte("-._~!$&'()*+,;="+extra, c) >= 0:
		default:
			return false
		}
	}

	return true
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
