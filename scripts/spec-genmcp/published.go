package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// page is one published document, read into the sections its headings open.
// The text before the first heading is the section with no anchor.
type page struct {
	ref      pageRef
	sections []section
}

type section struct {
	anchor    string
	text      string
	sentences []string
}

var (
	headingRE  = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	listItemRE = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+`)
	refDefRE   = regexp.MustCompile(`^\s*\[[^\]]+\]:\s`)
)

// parsePage reads Markdown or MDX. Code blocks and tables are skipped: neither
// is where the published text states an obligation in a sentence.
func parsePage(ref pageRef, body string) *page {
	p := &page{ref: ref}
	slugs := map[string]int{}

	if rest, ok := strings.CutPrefix(body, "---\n"); ok {
		if _, after, found := strings.Cut(rest, "\n---\n"); found {
			body = after
		}
	}

	current := section{}

	var (
		block []string
		fence bool
	)

	flushBlock := func() {
		if text := normalize(strings.Join(block, " "), false); text != "" {
			current.sentences = append(current.sentences, sentences(text)...)
			current.text = strings.TrimSpace(current.text + " " + text)
		}

		block = nil
	}

	flushSection := func() {
		flushBlock()
		p.sections = append(p.sections, current)
	}

	for line := range strings.Lines(body) {
		line = strings.TrimRight(line, "\r\n")
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			fence = !fence

			continue
		}

		switch {
		case fence, strings.HasPrefix(trimmed, "|"), refDefRE.MatchString(line):
			continue
		case trimmed == "":
			flushBlock()
		case headingRE.MatchString(line):
			flushSection()

			heading := headingRE.FindStringSubmatch(line)[2]
			current = section{anchor: uniqueSlug(slugs, normalize(heading, false))}
		case listItemRE.MatchString(line):
			flushBlock()

			block = append(block, listItemRE.ReplaceAllString(line, ""))
		default:
			block = append(block, line)
		}
	}

	flushSection()

	return p
}

func (p *page) section(anchor string) (*section, bool) {
	for i := range p.sections {
		if p.sections[i].anchor == anchor {
			return &p.sections[i], true
		}
	}

	return nil, false
}

var (
	codeSpanRE  = regexp.MustCompile("`+([^`]*)`+")
	inlineRE    = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	refLinkRE   = regexp.MustCompile(`\[([^\]]*)\]\[[^\]]*\]`)
	insertionRE = regexp.MustCompile(`\s*\[[^\]]*\]`)
	tagRE       = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	strongRE    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	emRE        = regexp.MustCompile(`\*([^*\s][^*]*)\*`)
	underRE     = regexp.MustCompile(`(^|[^\w])_([^_\s][^_]*?)_($|[^\w])`)
	escapeRE    = regexp.MustCompile(`\\([<>*_\[\]()#])`)
	placeRE     = regexp.MustCompile("\x00([0-9]+)\x00")
)

// normalize reduces inline Markdown to the text a reader sees. Code spans are
// set aside first, so an identifier like _meta is not read as emphasis. With
// insertions, a [bracketed] editorial insertion outside code is dropped.
func normalize(s string, insertions bool) string {
	var code []string

	s = codeSpanRE.ReplaceAllStringFunc(s, func(m string) string {
		code = append(code, codeSpanRE.FindStringSubmatch(m)[1])

		return fmt.Sprintf("\x00%d\x00", len(code)-1)
	})

	s = inlineRE.ReplaceAllString(s, "$1")
	s = refLinkRE.ReplaceAllString(s, "$1")

	if insertions {
		s = insertionRE.ReplaceAllString(s, "")
	}

	s = tagRE.ReplaceAllString(s, "")
	s = strongRE.ReplaceAllString(s, "$1")
	s = emRE.ReplaceAllString(s, "$1")
	s = underRE.ReplaceAllString(s, "$1$2$3")
	s = escapeRE.ReplaceAllString(s, "$1")

	s = placeRE.ReplaceAllStringFunc(s, func(m string) string {
		var i int

		_, _ = fmt.Sscanf(placeRE.FindStringSubmatch(m)[1], "%d", &i)

		return code[i]
	})

	return strings.Join(strings.Fields(s), " ")
}

var abbreviations = []string{"e.g.", "i.e.", "etc.", "vs.", "cf."}

// sentences splits on a terminator followed by a space and something that can
// open a sentence. Where it is wrong, a requirement fails to locate and says
// so; it is never silently mismatched.
func sentences(text string) []string {
	var (
		out   []string
		start int
	)

	for i := 0; i < len(text)-2; i++ {
		if !strings.ContainsRune(".?!", rune(text[i])) || text[i+1] != ' ' {
			continue
		}

		if c := rune(text[i+2]); !unicode.IsUpper(c) && !strings.ContainsRune(`"([`, c) {
			continue
		}

		if slices.ContainsFunc(abbreviations, func(a string) bool {
			return strings.HasSuffix(text[start:i+1], a)
		}) {
			continue
		}

		out = append(out, strings.TrimSpace(text[start:i+1]))
		start = i + 2
	}

	if rest := strings.TrimSpace(text[start:]); rest != "" {
		out = append(out, rest)
	}

	return out
}

// slug is GitHub's heading anchor, which modelcontextprotocol.io also follows
// for every heading this generator cites; the live check holds it to that.
func slug(heading string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}

	return b.String()
}

func uniqueSlug(seen map[string]int, heading string) string {
	s := slug(heading)

	n := seen[s]
	seen[s]++

	if n == 0 {
		return s
	}

	return fmt.Sprintf("%s-%d", s, n)
}

// fragments splits a quote at its [bracketed] insertions; each piece must
// occur in the section in order, which is what makes the rest a quotation.
func fragments(quote string) []string {
	var out []string

	for _, f := range regexp.MustCompile(`\[[^\]]*\]`).Split(quote, -1) {
		if f = strings.Trim(strings.TrimSpace(f), ".,;:"); f != "" {
			out = append(out, f)
		}
	}

	return out
}

func (s *section) holds(quote string) bool {
	text := s.text

	for _, f := range fragments(quote) {
		i := strings.Index(text, f)
		if i < 0 {
			return false
		}

		text = text[i+len(f):]
	}

	return true
}
