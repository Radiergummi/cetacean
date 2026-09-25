package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/radiergummi/cetacean/internal/spec"
)

// sourceURL resolves the plain-text form of a document. RFCs are immutable and
// addressed by number; an Internet-Draft is addressed by the revision the
// registry pins, so both are a function of what the document already declares.
func sourceURL(d *spec.Document) (string, error) {
	if m := rfcNumber.FindStringSubmatch(d.Source); m != nil {
		return "https://www.rfc-editor.org/rfc/rfc" + m[1] + ".txt", nil
	}

	if strings.HasPrefix(d.Revision, "draft-") {
		return "https://www.ietf.org/archive/id/" + d.Revision + ".txt", nil
	}

	return "", fmt.Errorf("no plain-text source: not an RFC, and revision %q is not a draft",
		d.Revision)
}

var rfcNumber = regexp.MustCompile(`^RFC\s*(\d+)$`)

// fetch returns the document text, caching it under the user cache directory.
// Every source this reaches is immutable — an RFC by number, a draft by
// revision — so a cached copy cannot go stale without the registry changing
// first.
func fetch(url string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	dir = filepath.Join(dir, "cetacean-spec")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}

	cached := filepath.Join(dir, filepath.Base(url))
	if body, err := os.ReadFile(cached); err == nil { // #nosec G304 -- own cache directory
		return string(body), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck // response body

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", url, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}

	return string(body), os.WriteFile(cached, body, 0o600)
}

// pageFooter matches the line a paginated RFC ends every page with. The line
// after the form feed that follows is the running header, and neither is part
// of the text.
var pageFooter = regexp.MustCompile(`\[Page \d+\]\s*$`)

// depaginate drops the running headers and footers of a paginated RFC. An
// unpaginated one carries no form feeds and comes back unchanged.
func depaginate(body string) []string {
	var (
		out        []string
		afterBreak bool
	)

	for line := range strings.Lines(body) {
		line = strings.TrimRight(line, "\r\n")

		if strings.Contains(line, "\f") {
			afterBreak = true

			continue
		}

		if pageFooter.MatchString(line) {
			continue
		}

		// The running header is the first thing printed on the new page.
		if afterBreak {
			if strings.TrimSpace(line) == "" {
				continue
			}

			afterBreak = false

			continue
		}

		out = append(out, line)
	}

	return out
}

// heading matches a section heading. A table-of-contents entry for the same
// section is indented and carries dot leaders, so requiring column zero and
// refusing leaders is what tells the two apart.
var heading = regexp.MustCompile(`^(\d+(?:\.\d+)*)\.?\s+\S`)

// sectionsOf returns the lines belonging to the declared sections, subsections
// included. A section is declared by number: "4" takes 4, 4.4 and 4.4.1 with it.
func sectionsOf(lines []string, declared []string) []string {
	var (
		out    []string
		wanted bool
	)

	// wanted is false until a declared heading sets it, so the title block and
	// the table of contents ahead of the first one are skipped by the same
	// test that skips an undeclared section.
	for _, line := range lines {
		m := heading.FindStringSubmatch(line)
		if m != nil && !strings.Contains(line, " . ") {
			wanted = covers(declared, m[1])

			continue
		}

		if wanted {
			out = append(out, line)
		}
	}

	return out
}

// covers reports whether a declared section number takes this one with it.
func covers(declared []string, number string) bool {
	for _, want := range declared {
		if number == want || strings.HasPrefix(number, want+".") {
			return true
		}
	}

	return false
}

// paragraphs joins the wrapped lines of each blank-line-separated block into
// one string. Indented blocks are kept: a parameter declaration is indented
// and is exactly the kind of statement this is looking for.
func paragraphs(lines []string) []string {
	var (
		out     []string
		current []string
	)

	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(strings.Fields(strings.Join(current, " ")), " "))
			current = nil
		}
	}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()

			continue
		}

		current = append(current, line)
	}

	flush()

	return out
}

// abbreviation lists the places a period does not end a sentence. Splitting on
// every ". " cuts "e.g." and "Section 4.1." in half, and a fragment matches
// nothing on the other side.
var abbreviation = []string{
	"e.g.", "i.e.", "cf.", "etc.", "vs.", "resp.", "Sec.", "Fig.", "No.", "Ed.",
}

// sentences splits a paragraph. The rule is a terminator, then space, then
// something that can open a sentence; it is deliberately simple, and where it
// is wrong the fragment shows up unmatched rather than silently dropped.
func sentences(paragraph string) []string {
	var (
		out   []string
		start int
	)

	for i := range len(paragraph) - 1 {
		if paragraph[i] != '.' && paragraph[i] != '?' && paragraph[i] != '!' {
			continue
		}

		if paragraph[i+1] != ' ' || i+2 >= len(paragraph) {
			continue
		}

		if !opensASentence(paragraph[i+2]) {
			continue
		}

		if endsWithAbbreviation(paragraph[start : i+1]) {
			continue
		}

		out = append(out, strings.TrimSpace(paragraph[start:i+1]))
		start = i + 2
	}

	if rest := strings.TrimSpace(paragraph[start:]); rest != "" {
		out = append(out, rest)
	}

	return out
}

func opensASentence(c byte) bool {
	return c >= 'A' && c <= 'Z' || c == '"' || c == '(' || c == '['
}

func endsWithAbbreviation(s string) bool {
	for _, a := range abbreviation {
		if strings.HasSuffix(s, a) {
			return true
		}
	}

	return false
}

// keyword matches an RFC 2119 term. Upper case only: "may" in ordinary prose
// carries no obligation, and RFC 8174 makes that the rule rather than a
// convention.
var keyword = regexp.MustCompile(
	`\b(MUST|SHALL|SHOULD|RECOMMENDED|REQUIRED|OPTIONAL|MAY)\b`,
)

// Statements returns every normative sentence in the declared sections of the
// document text.
func Statements(body string, declared []string) []string {
	var out []string

	for _, p := range paragraphs(sectionsOf(depaginate(body), declared)) {
		for _, s := range sentences(p) {
			if keyword.MatchString(s) {
				out = append(out, s)
			}
		}
	}

	return out
}
