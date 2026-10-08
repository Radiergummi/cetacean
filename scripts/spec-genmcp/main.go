// Command spec-genmcp writes the MCP families of the requirement registry from
// the conformance suite's own requirement files. The suite publishes them as
// YAML already, so transcribing them by hand is how the text stops being a
// quote. Output follows scripts/dump-errors: plain stderr, not slog.
package main

import (
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

// upstreamRev pins the commit the registry quotes. Bumping it is the only way
// the text here changes, which is what makes a drift a reviewable diff.
const upstreamRev = "7169291ec0b68eb370fddcd9947313ab0d5e4156"

const rawBase = "https://raw.githubusercontent.com/modelcontextprotocol/conformance/"

var seps = []int{2575, 2243, 2549, 2164}

// gaps are requirements of ours that no test reaches yet. They are registered
// rather than dismissed: the obligation is real, only the evidence is missing.
var gaps = map[string]string{
	"sep-2575-http-server-no-independent-requests-on-stream": "The stream lane reads " +
		"notifications and never asserts that nothing else arrives on the stream.",
}

// dismissed says why there is nothing of ours to test. A check absent from
// here becomes a registered requirement instead, which the static gate fails
// on until a test claims it — so an upstream addition is never silent.
var dismissed = map[string]string{
	"sep-2575-client-populates-meta":                  reasonClientSide,
	"sep-2575-client-retry-supported-version":         reasonClientSide,
	"sep-2575-http-client-sends-version-header":       reasonClientSide,
	"sep-2575-client-declares-elicitation-capability": reasonClientSide,
	"sep-2575-client-declares-roots-capability":       reasonClientSide,
	"sep-2575-client-declares-sampling-capability":    reasonClientSide,
	"sep-2243-client-includes-standard-headers":       reasonClientSide,
	"sep-2243-client-supports-custom-headers":         reasonClientSide,
	"sep-2243-client-mirrors-designated-params":       reasonClientSide,
	"sep-2243-client-reject-invalid-tool":             reasonClientSide,
	"sep-2243-client-encode-values":                   reasonClientSide,
	"sep-2243-client-base64-unsafe":                   reasonClientSide,
	"sep-2243-client-omit-null":                       reasonClientSide,
	"sep-2575-http-version-header-matches-meta": "Addressed to the client sending the " +
		"header; the obligation it puts on this server is http-server-header-mismatch-400.",
	"sep-2575-server-sends-prompts-list-changed-on-subscription": "The prompt catalog is " +
		"built once at startup, so the list this notification reports never changes.",
	"sep-2575-server-sends-tools-list-changed-on-subscription": "The tool catalog is built " +
		"once at startup and filtered per identity at list time; the list itself never changes.",
	"sep-2575-server-no-log-without-loglevel": "This server emits no notifications/message " +
		"at all, per-request log level or not.",
	"sep-2243-x-mcp-header-not-empty":            reasonNoDesignations,
	"sep-2243-x-mcp-header-charset":              reasonNoDesignations,
	"sep-2243-x-mcp-header-unique":               reasonNoDesignations,
	"sep-2243-x-mcp-header-primitive-only":       reasonNoDesignations,
	"sep-2243-server-decode-base64":              reasonNoDesignatedParams,
	"sep-2243-server-not-expect-null":            reasonNoDesignatedParams,
	"sep-2243-server-reject-missing-required":    reasonNoDesignatedParams,
	"sep-2243-server-reject-invalid-param-chars": reasonNoDesignatedParams,
	"sep-2243-server-validate-param-match":       reasonNoDesignatedParams,
	"sep-2243-server-reject-param-mismatch":      reasonNoDesignatedParams,
}

const reasonNoDesignations = "This server designates no x-mcp-header parameter, so no " +
	"tool definition of ours can carry a value this constrains."

const reasonNoDesignatedParams = "Mcp-Param-{Name} headers exist only for parameters a " +
	"tool designates with x-mcp-header; no tool of this server designates any."

const reasonClientSide = "Client-side requirement."

// levels carries the strength of a requirement whose text states a closed
// enumeration rather than an RFC 2119 keyword. The suite fails a server that
// violates it either way, so it has a strength even where the sentence does not.
var levels = map[string]string{
	"sep-2549-cache-scope-valid": "MUST",
}

type upstreamDoc struct {
	SEP          int           `yaml:"sep"`
	SpecURL      string        `yaml:"spec_url"`
	Requirements []upstreamReq `yaml:"requirements"`
}

type upstreamReq struct {
	Check string `yaml:"check"`
	Text  string `yaml:"text"`
	URL   string `yaml:"url"`
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

	seen := map[string]bool{}

	for _, sep := range seps {
		raw := fmt.Sprintf("%s%s/src/seps/sep-%d.yaml", rawBase, upstreamRev, sep)

		body, err := fetch(raw)
		if err != nil {
			return err
		}

		var doc upstreamDoc
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return fmt.Errorf("sep-%d: %w", sep, err)
		}

		path := filepath.Join(dir, fmt.Sprintf("sep-%d.yaml", sep))

		out, err := render(&doc, raw, reviewed(path), seen)
		if err != nil {
			return fmt.Errorf("sep-%d: %w", sep, err)
		}

		if err := os.WriteFile(path, out, 0o600); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "spec-genmcp: wrote %s\n", path)
	}

	return unconsumed(seen)
}

// unconsumed refuses a table entry for a check upstream no longer publishes.
// It is the sweep's stale-dismissal rule applied to this file's own tables: a
// reason for something that does not exist is worse than no reason.
func unconsumed(seen map[string]bool) error {
	var stale []string

	for _, table := range []map[string]string{dismissed, gaps, levels} {
		for check := range table {
			if !seen[check] {
				stale = append(stale, check)
			}
		}
	}

	if len(stale) == 0 {
		return nil
	}

	return fmt.Errorf(
		"upstream no longer publishes %s; drop the entry from scripts/spec-genmcp",
		strings.Join(slices.Sorted(slices.Values(stale)), ", "),
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
// RFC 2119 keyword has no strength to read, and is a fault in the source.
func level(text string) (string, error) {
	m := levelRE.FindString(text)
	if m == "" {
		return "", fmt.Errorf("no RFC 2119 keyword in %q", text)
	}

	return m, nil
}

// reviewed is the date the existing file carries, as long as it was written
// against this same upstream revision. Stamping today unconditionally would
// make every regeneration a diff, and the date would stop meaning anything.
func reviewed(path string) string {
	today := time.Now().Format(time.DateOnly)

	body, err := os.ReadFile(path) // #nosec G304 -- a path this generator built
	if err != nil {
		return today
	}

	var prev spec.Document

	if err := yaml.Unmarshal(body, &prev); err != nil || prev.Revision != upstreamRev {
		return today
	}

	return prev.Reviewed
}

func render(doc *upstreamDoc, raw, reviewed string, seen map[string]bool) ([]byte, error) {
	prefix := fmt.Sprintf("sep-%d-", doc.SEP)

	var (
		reqs    []upstreamReq
		dismiss = map[string]string{}
		checks  int
	)

	for _, r := range doc.Requirements {
		if r.Check == "" {
			continue
		}

		checks++
		seen[r.Check] = true

		reason, skip := dismissed[r.Check]
		if !skip {
			reqs = append(reqs, r)

			continue
		}

		dismiss[strings.TrimPrefix(r.Check, prefix)] = reason
	}

	var b strings.Builder

	fmt.Fprintf(&b, "# Generated by scripts/spec-genmcp. Do not edit: run `make spec-genmcp`.\n\n")
	fmt.Fprintf(&b, "source: SEP-%d\n", doc.SEP)
	fmt.Fprintf(&b, "revision: %s\n", upstreamRev)
	fmt.Fprintf(&b, "reviewed: %s\n", reviewed)
	fmt.Fprintf(&b, "url: %s\n\n", doc.SpecURL)

	fmt.Fprintf(&b, "inventory:\n")
	fmt.Fprintf(&b, "  from: %s\n", raw)
	fmt.Fprintf(&b, "  count: %d\n\n", checks)

	fmt.Fprintf(&b, "requirements:\n")

	for _, r := range reqs {
		lvl, ok := levels[r.Check]
		if !ok {
			var err error
			if lvl, err = level(r.Text); err != nil {
				return nil, fmt.Errorf("%s: %w; add it to the levels table in "+
					"scripts/spec-genmcp", r.Check, err)
			}
		}

		fmt.Fprintf(&b, "  - id: %s\n", strings.TrimPrefix(r.Check, prefix))
		fmt.Fprintf(&b, "    level: %s\n", lvl)

		// Every requirement this suite declares is driven from test/e2e, which
		// needs the conformance harness. A gap is driven by nothing yet, so it
		// gains the declaration when it leaves the table above.
		if gaps[r.Check] == "" {
			fmt.Fprintf(&b, "    lane: e2e\n")
		}

		b.WriteString(folded("    text", r.Text))

		url := r.URL
		if url == "" {
			url = doc.SpecURL
		}

		fmt.Fprintf(&b, "    url:\n      - %s\n      - %s\n", url, raw)

		if reason := gaps[r.Check]; reason != "" {
			b.WriteString(folded("    gap", reason))
		}

		b.WriteString("\n")
	}

	if len(dismiss) > 0 {
		fmt.Fprintf(&b, "dismissed:\n")

		for _, id := range slices.Sorted(maps.Keys(dismiss)) {
			b.WriteString(folded("  "+id, dismiss[id]))
		}
	}

	out := []byte(b.String())

	return out, verify(out, prefix, reqs, dismiss)
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
func verify(out []byte, prefix string, reqs []upstreamReq, dismiss map[string]string) error {
	var back spec.Document

	if err := yaml.Unmarshal(out, &back); err != nil {
		return fmt.Errorf("rendered file does not parse: %w", err)
	}

	if len(back.Requirements) != len(reqs) {
		return fmt.Errorf(
			"rendered %d requirements, read back %d",
			len(reqs),
			len(back.Requirements),
		)
	}

	for i, got := range back.Requirements {
		want := strings.Join(strings.Fields(reqs[i].Text), " ")
		if got.Text != want {
			return fmt.Errorf("%s: text did not survive the round trip:\n  got  %q\n  want %q",
				got.ID, got.Text, want)
		}

		if got.ID != strings.TrimPrefix(reqs[i].Check, prefix) {
			return fmt.Errorf("id %q, want %q", got.ID, reqs[i].Check)
		}
	}

	if len(back.Dismissed) != len(dismiss) {
		return fmt.Errorf("rendered %d dismissals, read back %d", len(dismiss), len(back.Dismissed))
	}

	return nil
}
