package imagecheck

import "strings"

// challenge is one auth-scheme and its parameters from a WWW-Authenticate
// field. Parameter names are lower-cased: RFC 9110 matches them
// case-insensitively, as it does the scheme.
type challenge struct {
	scheme string
	params map[string]string
}

// parseChallenges reads every challenge out of the given field values. A
// field may hold several, separated by commas that also separate parameters,
// so a token is a new scheme exactly when no "=" follows it.
func parseChallenges(values []string) []challenge {
	var out []challenge

	for _, value := range values {
		p := parser{s: value}

		for {
			p.skip(" \t,")

			scheme := p.token()
			if scheme == "" {
				break
			}

			c := challenge{scheme: strings.ToLower(scheme), params: map[string]string{}}
			p.params(c.params)
			out = append(out, c)

			if p.pos == len(p.s) {
				break
			}
		}
	}

	return out
}

type parser struct {
	s   string
	pos int
}

func (p *parser) skip(set string) {
	for p.pos < len(p.s) && strings.IndexByte(set, p.s[p.pos]) >= 0 {
		p.pos++
	}
}

// params reads auth-params until the next scheme or the end, leaving the
// position on that scheme. A token68 credential is skipped whole.
func (p *parser) params(into map[string]string) {
	for {
		p.skip(" \t,")
		start := p.pos

		name := p.token()
		if name == "" {
			// Not a token at all: skip the junk up to the next comma.
			for p.pos < len(p.s) && p.s[p.pos] != ',' {
				p.pos++
			}

			if p.pos == len(p.s) {
				return
			}

			continue
		}

		p.skip(" \t")

		if p.pos >= len(p.s) || p.s[p.pos] != '=' {
			p.pos = start

			return
		}

		p.pos++

		// token68 ends in "=" padding, which reads here as "==" or "=,".
		if p.pos >= len(p.s) || p.s[p.pos] == '=' || p.s[p.pos] == ',' {
			for p.pos < len(p.s) && p.s[p.pos] != ',' {
				p.pos++
			}

			continue
		}

		p.skip(" \t")

		var value string
		if p.pos < len(p.s) && p.s[p.pos] == '"' {
			value = p.quoted()
		} else {
			value = p.token()
		}

		into[strings.ToLower(name)] = value
	}
}

func (p *parser) token() string {
	start := p.pos
	for p.pos < len(p.s) && isTokenChar(p.s[p.pos]) {
		p.pos++
	}

	return p.s[start:p.pos]
}

// quoted reads a quoted-string, undoing each quoted-pair.
func (p *parser) quoted() string {
	var b strings.Builder

	p.pos++ // the opening quote

	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++

		switch {
		case c == '"':
			return b.String()
		case c == '\\' && p.pos < len(p.s):
			b.WriteByte(p.s[p.pos])
			p.pos++
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}

func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	default:
		return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
	}
}
