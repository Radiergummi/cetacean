package cluster

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/docker/docker/api/types/swarm"
	"golang.org/x/net/idna"
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

		title := strings.TrimSpace(labels[key+linkTitleSuffix])
		if title == "" {
			title = id
		}

		link := ServiceLink{ID: id, Title: title}
		if href, ok := normaliseHTTPURL(value); ok {
			link.Href = href
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

// stripUserinfo drops the credentials a URL's authority may carry, which a git
// remote recorded from a local checkout sometimes does.
func stripUserinfo(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return raw
	}

	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}

	if at := strings.LastIndexByte(rest[:end], '@'); at >= 0 {
		rest = rest[at+1:]
	}

	return scheme + "://" + rest
}

// normaliseHTTPURL returns value as a valid RFC 3986 URI when it is
// structurally an absolute http(s) URL, percent-encoding whatever its path,
// query and fragment may not hold. Userinfo is refused: it can make a link
// read as one host while it opens another.
func normaliseHTTPURL(value string) (string, bool) {
	scheme, rest, ok := strings.Cut(value, "://")
	if !ok || !strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https") {
		return "", false
	}

	rest, fragment, hasFragment := strings.Cut(rest, "#")
	rest, query, hasQuery := strings.Cut(rest, "?")

	authority, path := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority, path = rest[:i], rest[i:]
	}

	authority, ok = normaliseAuthority(authority)
	if !ok {
		return "", false
	}

	var out strings.Builder

	out.WriteString(scheme + "://" + authority)
	percentEncode(&out, path, ":@/")

	if hasQuery {
		out.WriteByte('?')
		percentEncode(&out, query, ":@/?")
	}

	if hasFragment {
		out.WriteByte('#')
		percentEncode(&out, fragment, ":@/?")
	}

	return out.String(), true
}

// normaliseAuthority checks host [":" port], where host is a bracketed IPv6
// literal or a reg-name without escapes, which a browser would decode into
// another host, and port is empty or at most 65535. An IDN becomes punycode.
func normaliseAuthority(authority string) (string, bool) {
	if strings.Contains(authority, "@") {
		return "", false
	}

	var host, port string

	if literal, ok := strings.CutPrefix(authority, "["); ok {
		var after string

		host, after, ok = strings.Cut(literal, "]")
		if !ok {
			return "", false
		}

		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return "", false
		}

		if after != "" {
			if port, ok = strings.CutPrefix(after, ":"); !ok {
				return "", false
			}
		}
	} else {
		var hasPort bool

		host, port, hasPort = strings.Cut(authority, ":")

		if strings.ContainsFunc(host, func(r rune) bool { return r > unicode.MaxASCII }) {
			ascii, err := idna.Lookup.ToASCII(host)
			if err != nil {
				return "", false
			}

			host = ascii
			authority = host

			if hasPort {
				authority += ":" + port
			}
		}

		if host == "" || strings.ContainsFunc(host, func(r rune) bool {
			return r > unicode.MaxASCII || !isURIUnreservedOrSubDelim(byte(r))
		}) {
			return "", false
		}
	}

	if port == "" {
		return authority, true
	}

	if strings.ContainsFunc(port, func(r rune) bool { return r < '0' || r > '9' }) {
		return "", false
	}

	if n, err := strconv.Atoi(port); err != nil || n > 65535 {
		return "", false
	}

	return authority, true
}

// percentEncode writes s with every byte the component does not allow
// percent-encoded, leaving well-formed escapes as they are.
func percentEncode(out *strings.Builder, s, extra string) {
	const hex = "0123456789ABCDEF"

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch {
		case c == '%' && isEscape(s, i):
			out.WriteString(s[i : i+3])
			i += 2
		case c != '%' && (isURIUnreservedOrSubDelim(c) || strings.IndexByte(extra, c) >= 0):
			out.WriteByte(c)
		default:
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0x0f])
		}
	}
}

func isEscape(s string, i int) bool {
	return i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2])
}

func isURIUnreservedOrSubDelim(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		strings.IndexByte("-._~!$&'()*+,;=", c) >= 0
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
