// Command spec-genmcp writes the MCP families of the requirement registry. The
// conformance suite's requirement files say which obligations exist; the
// published revision says what each one is, so every text is a quotation of it
// and every citation an anchor into it. Output follows scripts/dump-errors.
package main

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/radiergummi/cetacean/internal/spec"
)

// The pins. Bumping one is the only way the text here changes, which is what
// makes a drift a reviewable diff.
const (
	revision      = "2026-07-28"
	conformanceAt = "c37eec888e1c6ff140af79987a40008548b7cc5f"
	specAt        = "dae694f61a31f7a3ec2af3aff709e11f70f61e48"
	tasksAt       = "b3d28aceaa6d49f5f92258e3d06b61ad6f111e84"
)

const conformanceBase = "https://raw.githubusercontent.com/modelcontextprotocol/conformance/"

func specPage(key string) pageRef {
	return pageRef{
		key: key,
		raw: "https://raw.githubusercontent.com/modelcontextprotocol/modelcontextprotocol/" +
			specAt + "/docs/specification/" + revision + "/" + key + ".mdx",
		web:  "https://modelcontextprotocol.io/specification/" + revision + "/" + key,
		live: true,
	}
}

// The Tasks extension is specified outside the core repository, and its
// published revision has no page of its own on the site.
var tasksPage = pageRef{
	key: "tasks",
	raw: "https://raw.githubusercontent.com/modelcontextprotocol/ext-tasks/" +
		tasksAt + "/specification/" + revision + "/tasks.md",
	web: "https://github.com/modelcontextprotocol/ext-tasks/blob/" +
		tasksAt + "/specification/" + revision + "/tasks.md",
}

var corePages = []pageRef{
	specPage("basic/index"),
	specPage("basic/versioning"),
	specPage("basic/transports/streamable-http"),
	specPage("basic/patterns/subscriptions"),
	specPage("server/prompts"),
	specPage("server/tools"),
	specPage("server/resources"),
	specPage("server/utilities/caching"),
	specPage("server/utilities/logging"),
	specPage("client/roots"),
	specPage("client/sampling"),
	specPage("deprecated"),
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "spec-genmcp:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := filepath.Join("internal", "spec", "registry", "mcp")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	c := &corpus{pages: map[string]*page{}, ids: map[string]map[string]bool{}}
	if err := c.load(append(slices.Clone(corePages), tasksPage)); err != nil {
		return err
	}

	seen := map[string]bool{}

	var docs []*document

	for _, s := range suites {
		d, err := fromSuite(c, s, seen)
		if err != nil {
			return fmt.Errorf("sep-%d: %w", s.sep, err)
		}

		docs = append(docs, d)
	}

	if err := unconsumed(seen); err != nil {
		return err
	}

	for i := range quoted {
		d, err := fromQuotes(c, &quoted[i])
		if err != nil {
			return fmt.Errorf("%s: %w", quoted[i].name, err)
		}

		docs = append(docs, d)
	}

	written := map[string]bool{}

	for _, d := range docs {
		for _, r := range d.reqs {
			written[d.name+"/"+r.id] = true
		}
	}

	for key := range mutants {
		if !written[key] {
			return fmt.Errorf("mutants names %s, which no document registers", key)
		}
	}

	for _, d := range docs {
		path := filepath.Join(dir, d.name+".yaml")

		out, err := d.write(path)
		if err != nil {
			return fmt.Errorf("%s: %w", d.name, err)
		}

		fmt.Fprintf(os.Stderr, "spec-genmcp: wrote %s\n", out)
	}

	return nil
}

// unconsumed refuses a table entry for a check upstream no longer publishes.
// It is the sweep's stale-dismissal rule applied to this file's own tables: a
// reason for something that does not exist is worse than no reason.
func unconsumed(seen map[string]bool) error {
	var stale []string

	for _, check := range slices.Concat(
		slices.Collect(maps.Keys(dismissed)),
		slices.Collect(maps.Keys(gaps)),
		slices.Collect(maps.Keys(deferred)),
		slices.Collect(maps.Keys(levels)),
		slices.Collect(maps.Keys(located)),
	) {
		if !seen[check] {
			stale = append(stale, check)
		}
	}

	if len(stale) == 0 {
		return nil
	}

	return fmt.Errorf(
		"upstream no longer publishes %s; drop the entry from scripts/spec-genmcp",
		strings.Join(slices.Compact(slices.Sorted(slices.Values(stale))), ", "),
	)
}

func fetch(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil) //nolint:noctx // one-shot generator
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only fetch

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

var levelRE = regexp.MustCompile(`\b(MUST|SHOULD|MAY)\b`)

// level reads the requirement's strength off its own text. A statement with no
// RFC 2119 keyword has no strength to read, and is a fault in the source. A
// statement with several takes the strongest: a SHOULD beside a MUST does not
// make the MUST optional.
func level(text string) (string, error) {
	found := levelRE.FindAllString(text, -1)

	for _, l := range []string{"MUST", "SHOULD", "MAY"} {
		if slices.Contains(found, l) {
			return l, nil
		}
	}

	return "", fmt.Errorf("no RFC 2119 keyword in %q", text)
}

type suite struct {
	sep   int
	title string
	at    string
	pages []pageRef

	// lane is where the claimants of this suite's requirements live.
	lane spec.Lane
}

type upstreamDoc struct {
	SEP          int           `yaml:"sep"`
	Requirements []upstreamReq `yaml:"requirements"`
}

type upstreamReq struct {
	Check string `yaml:"check"`
	Text  string `yaml:"text"`
}

func fromSuite(c *corpus, s suite, seen map[string]bool) (*document, error) {
	raw := fmt.Sprintf("%s%s/src/seps/sep-%d.yaml", conformanceBase, conformanceAt, s.sep)

	body, err := fetch(raw)
	if err != nil {
		return nil, err
	}

	var up upstreamDoc
	if err := yaml.Unmarshal(body, &up); err != nil {
		return nil, err
	}

	if err := c.load(s.pages); err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(s.pages))
	for _, p := range s.pages {
		keys = append(keys, p.key)
	}

	_, home, err := c.cite(s.at)
	if err != nil {
		return nil, err
	}

	name := fmt.Sprintf("sep-%d", s.sep)
	prefix := name + "-"

	d := &document{
		name:      name,
		source:    fmt.Sprintf("SEP-%d", s.sep),
		title:     s.title,
		url:       []string{home},
		from:      raw,
		dismissed: map[string]string{},
	}

	var problems []string

	for _, r := range up.Requirements {
		if r.Check == "" {
			continue
		}

		d.count++
		seen[r.Check] = true
		id := strings.TrimPrefix(r.Check, prefix)

		if reason, ok := dismissed[r.Check]; ok {
			d.dismissed[id] = reason

			continue
		}

		quote, url, err := c.locateCheck(keys, r)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", r.Check, err))

			continue
		}

		q := requirement{
			id:       id,
			text:     quote,
			urls:     []string{url, raw},
			gap:      gaps[r.Check],
			deferred: deferred[r.Check],
			mutants:  mutants[name+"/"+id],
		}

		if q.gap == "" {
			q.lane = s.lane
		}

		if q.level = levels[r.Check]; q.level == "" {
			if q.level, err = level(quote); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v; add it to the levels "+
					"table in scripts/spec-genmcp", r.Check, err))

				continue
			}
		}

		d.reqs = append(d.reqs, q)
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("locate each in the located table, or dismiss it:\n  %s",
			strings.Join(problems, "\n  "))
	}

	return d, nil
}

// locateCheck quotes the published text a check stands for: found from the
// suite's own wording where that is a quotation, or from the located table
// where the suite paraphrases.
func (c *corpus) locateCheck(keys []string, r upstreamReq) (string, string, error) {
	loc, ok := located[r.Check]
	if !ok {
		return c.locate(keys, "", r.Text)
	}

	if loc.quote == "" {
		return c.locate(keys, loc.at, r.Text)
	}

	s, url, err := c.cite(loc.at)
	if err != nil {
		return "", "", err
	}

	if !s.holds(loc.quote) {
		return "", "", fmt.Errorf("%s does not hold %q", loc.at, loc.quote)
	}

	return loc.quote, url, nil
}

func fromQuotes(c *corpus, q *quotedDoc) (*document, error) {
	d := &document{
		name:      q.name,
		source:    q.source,
		title:     q.title,
		from:      q.from,
		dismissed: map[string]string{},
	}

	for _, at := range q.at {
		_, url, err := c.cite(at)
		if err != nil {
			return nil, err
		}

		d.url = append(d.url, url)
	}

	for _, r := range q.reqs {
		s, url, err := c.cite(r.at)
		if err != nil {
			return nil, err
		}

		if !s.holds(r.quote) {
			return nil, fmt.Errorf("%s: %s does not hold %q", r.id, r.at, r.quote)
		}

		lvl := r.level
		if lvl == "" {
			if lvl, err = level(r.quote); err != nil {
				return nil, fmt.Errorf("%s: %w", r.id, err)
			}
		}

		d.reqs = append(d.reqs, requirement{
			id:       r.id,
			level:    lvl,
			text:     r.quote,
			urls:     []string{url},
			deferred: r.deferred,
			mutants:  mutants[q.name+"/"+r.id],
		})
	}

	// A dismissal quotes its statement too, though only the reason is written:
	// it is what holds the inventory to statements the revision makes.
	for _, r := range q.dismissed {
		s, _, err := c.cite(r.at)
		if err != nil {
			return nil, err
		}

		if !s.holds(r.quote) {
			return nil, fmt.Errorf("%s: %s does not hold %q", r.id, r.at, r.quote)
		}

		d.dismissed[r.id] = r.reason
	}

	d.count = len(d.reqs) + len(d.dismissed)

	return d, nil
}

type document struct {
	name, source, title string
	url                 []string
	from                string
	count               int
	reqs                []requirement
	dismissed           map[string]string
}

type requirement struct {
	id, level, text string
	lane            spec.Lane
	urls            []string
	gap, deferred   string
	mutants         []spec.Mutant
}

// write keeps the date the existing file carries when nothing else in it
// changed. Stamping today unconditionally would make every regeneration a
// diff, and the date would stop meaning anything.
func (d *document) write(path string) (string, error) {
	prev, err := os.ReadFile(path) // #nosec G304 -- a path this generator built
	if err == nil {
		var old spec.Document
		if yaml.Unmarshal(prev, &old) == nil && old.Reviewed != "" {
			out, err := d.render(old.Reviewed)
			if err != nil {
				return "", err
			}

			if bytes.Equal(out, prev) {
				return path, nil
			}
		}
	}

	out, err := d.render(time.Now().Format(time.DateOnly))
	if err != nil {
		return "", err
	}

	return path, os.WriteFile(path, out, 0o600)
}

func (d *document) render(reviewed string) ([]byte, error) {
	var b strings.Builder

	fmt.Fprintf(&b, "# Generated by scripts/spec-genmcp. Do not edit: run `make spec-genmcp`.\n\n")
	fmt.Fprintf(&b, "source: %s\n", d.source)
	fmt.Fprintf(&b, "title: %s\n", d.title)
	fmt.Fprintf(&b, "revision: %q\n", revision)
	fmt.Fprintf(&b, "reviewed: %s\n", reviewed)
	writeURLs(&b, "", d.url)
	b.WriteString("\n")

	fmt.Fprintf(&b, "inventory:\n")
	b.WriteString(folded("  from", d.from))
	fmt.Fprintf(&b, "  count: %d\n\n", d.count)

	if len(d.reqs) == 0 {
		fmt.Fprintf(&b, "requirements: []\n\n")
	} else {
		fmt.Fprintf(&b, "requirements:\n")
	}

	for _, r := range d.reqs {
		fmt.Fprintf(&b, "  - id: %s\n", r.id)
		fmt.Fprintf(&b, "    level: %s\n", r.level)

		if r.lane != "" {
			fmt.Fprintf(&b, "    lane: %s\n", r.lane)
		}

		b.WriteString(folded("    text", r.text))
		writeURLs(&b, "    ", r.urls)

		if r.gap != "" {
			b.WriteString(folded("    gap", r.gap))
		}

		if r.deferred != "" {
			b.WriteString(folded("    deferred", r.deferred))
		}

		if len(r.mutants) > 0 {
			b.WriteString("    mutants:\n")

			for _, m := range r.mutants {
				fmt.Fprintf(&b, "      - file: %s\n", m.File)
				fmt.Fprintf(&b, "        replace: %s\n", quoteYAML(m.Replace))
				fmt.Fprintf(&b, "        with: %s\n", quoteYAML(m.With))
			}
		}

		b.WriteString("\n")
	}

	if len(d.dismissed) > 0 {
		fmt.Fprintf(&b, "dismissed:\n")

		for _, id := range slices.Sorted(maps.Keys(d.dismissed)) {
			b.WriteString(folded("  "+id, d.dismissed[id]))
		}
	}

	out := []byte(strings.TrimRight(b.String(), "\n") + "\n")

	return out, d.verify(out)
}

func writeURLs(b *strings.Builder, indent string, urls []string) {
	if len(urls) == 1 {
		fmt.Fprintf(b, "%surl: %s\n", indent, urls[0])

		return
	}

	fmt.Fprintf(b, "%surl:\n", indent)

	for _, u := range urls {
		fmt.Fprintf(b, "%s  - %s\n", indent, u)
	}
}

// quoteYAML writes a mutant pattern as a double-quoted scalar, the one form
// that keeps its tabs and leading whitespace exactly.
func quoteYAML(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil || !strings.HasPrefix(string(out), `"`) {
		return fmt.Sprintf("%q", s)
	}

	return strings.TrimSpace(string(out))
}

// folded emits a >- block scalar, which quotes nothing and escapes nothing.
// Folding rejoins the wrapped lines with single spaces, so the value survives
// only if the text has none of its own; verify holds that.
func folded(key, text string) string {
	var (
		b    strings.Builder
		line string
	)

	fmt.Fprintf(&b, "%s: >-\n", key)

	indent := strings.Repeat(" ", len(key)-len(strings.TrimLeft(key, " "))+2)

	flush := func() {
		if line != "" {
			fmt.Fprintf(&b, "%s%s\n", indent, line)
			line = ""
		}
	}

	for word := range strings.FieldsSeq(text) {
		if line != "" && len(indent)+len(line)+1+len(word) > 88 {
			flush()
		}

		if line == "" {
			line = word
		} else {
			line += " " + word
		}
	}

	flush()

	return b.String()
}

// verify reads the rendered file back and fails unless every text survived the
// round trip. The registry's whole claim is that these are quotations.
func (d *document) verify(out []byte) error {
	var back spec.Document

	if err := yaml.Unmarshal(out, &back); err != nil {
		return fmt.Errorf("rendered file does not parse: %w", err)
	}

	if len(back.Requirements) != len(d.reqs) {
		return fmt.Errorf("rendered %d requirements, read back %d",
			len(d.reqs), len(back.Requirements))
	}

	for i, got := range back.Requirements {
		want := d.reqs[i]

		if got.ID != want.id || got.Text != strings.Join(strings.Fields(want.text), " ") {
			return fmt.Errorf("%s did not survive the round trip:\n  got  %q\n  want %q",
				want.id, got.Text, want.text)
		}

		if len(got.Mutants) != len(want.mutants) ||
			!slices.Equal(got.Mutants, want.mutants) {
			return fmt.Errorf("%s: mutants did not survive the round trip", want.id)
		}
	}

	if len(back.Dismissed) != len(d.dismissed) {
		return fmt.Errorf("rendered %d dismissals, read back %d",
			len(d.dismissed), len(back.Dismissed))
	}

	return nil
}
