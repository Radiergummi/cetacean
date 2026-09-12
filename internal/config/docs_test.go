package config

// docs_test.go holds internal/config's CETACEAN_* environment-variable surface
// against what docs/configuration.mdx and CLAUDE.md promise: derive both sides
// from source, then assert both directions through the excuse map in
// internal/contract/excused.go.
//
// # Deriving what the product reads
//
// Every CETACEAN_* variable this package consults reaches it as a literal
// string argument to a `resolve*` helper, to os.Getenv, or via a local
// `envKey` constant. The scan is go/ast over whole string-literal nodes, not a
// regexp over source text: comments and flag descriptions here mention several
// of these names in prose, and only a whole-literal scan excludes them. Which
// settings resolveSecret also wires for a `_FILE` variant needs a second,
// call-site-aware pass.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var envVarPattern = regexp.MustCompile(`^CETACEAN_[A-Z0-9_]+$`)

// readEnvVars returns every base CETACEAN_* environment variable
// internal/config consults. It excludes the derived _FILE variant each
// resolveSecret call also wires: the docs promise that convention as a
// property of a setting, not as a separate named variable.
func readEnvVars(t *testing.T) map[string]bool {
	t.Helper()

	return literalEnvVars(t)
}

// literalEnvVars collects every whole string-literal node across the
// package's non-test files that matches the CETACEAN_ pattern. See the
// file-level comment for why a literal-node scan, rather than a text
// regexp, is the reliable mechanism here.
func literalEnvVars(t *testing.T) map[string]bool {
	t.Helper()

	vars := map[string]bool{}

	forEachSourceFile(t, func(file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			if value, ok := stringLiteral(n); ok && envVarPattern.MatchString(value) {
				vars[value] = true
			}

			return true
		})
	})

	return vars
}

// resolveSecretVars returns the envKey argument of every call to
// resolveSecret in the package's non-test files: the settings that also
// accept a _FILE-suffixed variant, per resolveSecret's own doc comment in
// resolve.go.
func resolveSecretVars(t *testing.T) map[string]bool {
	t.Helper()

	vars := map[string]bool{}

	forEachSourceFile(t, func(file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			ident, ok := call.Fun.(*ast.Ident)
			if !ok || ident.Name != "resolveSecret" || len(call.Args) < 2 {
				return true
			}

			if value, ok := stringLiteral(call.Args[1]); ok && envVarPattern.MatchString(value) {
				vars[value] = true
			}

			return true
		})
	})

	return vars
}

// stringLiteral reports the unquoted value of n when it is a string literal.
func stringLiteral(n ast.Node) (string, bool) {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}

	return value, true
}

// forEachSourceFile parses every non-test .go file directly in this package
// directory — it carries no build tags and no subpackages, so a plain glob
// is the whole surface — and calls fn with each parsed file.
func forEachSourceFile(t *testing.T, fn func(*ast.File)) {
	t.Helper()

	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob *.go: %v", err)
	}

	fset := token.NewFileSet()
	found := false

	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}

		found = true

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		fn(file)
	}

	if !found {
		t.Fatal("no non-test .go files found in internal/config — the glob pattern is broken")
	}
}

// # Deriving what the docs promise
//
// docs/configuration.mdx documents each setting as a `<ConfigParam>`
// component, read through its `env` attribute. CLAUDE.md's copy is a plain
// markdown table, read keyed by column so a value one column over cannot be
// mistaken for the one asked about.

const configDocsPath = "../../docs/configuration.mdx"

var (
	configParamPattern = regexp.MustCompile(`(?s)<ConfigParam\s+([^>]*)>(.*?)</ConfigParam>`)
	envAttrPattern     = regexp.MustCompile(`env="([A-Z0-9_]+)"`)
)

// configParamCard is one <ConfigParam> block: its attribute string and its
// body text, kept apart because the two rules below read them at different
// scopes — documentedEnvVars only needs the env attribute, while the _FILE
// convention check reads a sentence in the body.
type configParamCard struct {
	attrs string
	body  string
}

func configDoc(t *testing.T) string {
	t.Helper()

	content, err := os.ReadFile(configDocsPath)
	if err != nil {
		t.Fatalf("read %s: %v", configDocsPath, err)
	}

	return string(content)
}

// configParamCards parses every <ConfigParam> card in docs/configuration.mdx,
// keyed by the CETACEAN_* env var its `env` attribute names. A card with no
// `env` attribute is skipped rather than indexed under "".
func configParamCards(t *testing.T) map[string]configParamCard {
	t.Helper()

	matches := configParamPattern.FindAllStringSubmatch(configDoc(t), -1)
	if len(matches) == 0 {
		t.Fatalf(
			"no <ConfigParam> cards found in %s — the page's markup has changed",
			configDocsPath,
		)
	}

	cards := map[string]configParamCard{}

	for _, match := range matches {
		attrs, body := match[1], match[2]

		env := envAttrPattern.FindStringSubmatch(attrs)
		if env == nil {
			continue
		}

		cards[env[1]] = configParamCard{attrs: attrs, body: body}
	}

	return cards
}

// documentedEnvVars returns the set of CETACEAN_* env vars
// docs/configuration.mdx promises.
func documentedEnvVars(t *testing.T) map[string]bool {
	t.Helper()

	vars := map[string]bool{}
	for name := range configParamCards(t) {
		vars[name] = true
	}

	return vars
}

// fileSuffixDocumentedVars returns the env vars whose card states plainly
// that they accept the _FILE suffix, matching the exact sentence
// docs/configuration.mdx uses for all five of them today.
func fileSuffixDocumentedVars(t *testing.T) map[string]bool {
	t.Helper()

	vars := map[string]bool{}

	for name, card := range configParamCards(t) {
		if strings.Contains(card.body, "Accepts the `_FILE` suffix") {
			vars[name] = true
		}
	}

	return vars
}

const claudeMDPath = "../../CLAUDE.md"

var backtickedEnvPattern = regexp.MustCompile("`(CETACEAN_[A-Z0-9_]+)`")

// claudeMDEnvVars returns the set of CETACEAN_* env vars listed in the
// Variable column of CLAUDE.md's "Environment variables" table. Reading only
// that column keeps a name mentioned in prose elsewhere in the row — the
// deprecated alias names its replacement — from counting as a row of its own.
func claudeMDEnvVars(t *testing.T) map[string]bool {
	t.Helper()

	content, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("read %s: %v", claudeMDPath, err)
	}

	vars := map[string]bool{}
	inTable := false

	for line := range strings.SplitSeq(string(content), "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "| Variable | Default | Required |") {
			inTable = true

			continue
		}

		if !inTable {
			continue
		}

		if !strings.HasPrefix(trimmed, "|") {
			inTable = false

			continue
		}

		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) == 0 {
			continue
		}

		if match := backtickedEnvPattern.FindStringSubmatch(
			strings.TrimSpace(cells[0]),
		); match != nil {
			vars[match[1]] = true
		}
	}

	if len(vars) == 0 {
		t.Fatalf(
			"no CETACEAN_* variables found in the environment variables table in %s — "+
				"the table's header text or format has changed",
			claudeMDPath,
		)
	}

	return vars
}

// claudeMDFileSuffixSentencePattern anchors on the one sentence in CLAUDE.md
// that spells the _FILE-suffixed names out literally. Scoping to that sentence
// rather than scanning the whole document matters because
// CETACEAN_ACL_POLICY_FILE is named with a _FILE suffix on its own merits and
// is not part of the secret-file convention. The pattern stops at the first
// closing parenthesis because the clause after it hand-wraps in the source.
var claudeMDFileSuffixSentencePattern = regexp.MustCompile(
	"(?s)Secret settings also accept a `_FILE` suffix on their env var \\((.*?)\\)",
)

// claudeMDFileSuffixVars returns the _FILE-suffixed names listed in that one
// sentence.
func claudeMDFileSuffixVars(t *testing.T) map[string]bool {
	t.Helper()

	content, err := os.ReadFile(claudeMDPath)
	if err != nil {
		t.Fatalf("read %s: %v", claudeMDPath, err)
	}

	match := claudeMDFileSuffixSentencePattern.FindStringSubmatch(string(content))
	if match == nil {
		t.Fatalf(
			"no \"Secret settings also accept a `_FILE` suffix\" sentence found in %s — "+
				"its wording has changed",
			claudeMDPath,
		)
	}

	vars := map[string]bool{}
	for _, m := range backtickedEnvPattern.FindAllStringSubmatch(match[1], -1) {
		vars[m[1]] = true
	}

	return vars
}

// # The comparison itself
//
// docDrift and diffDocs hold the rule from internal/contract/excused.go: an
// excuse must carry a reason, and a stale excuse fails like a missing one.

// docDrift reports every unexcused gap between what is read and what is
// documented, in both directions, plus every excuse that no longer names a
// real gap.
type docDrift struct {
	unexcusedUndocumented []string // read, not documented, no excuse
	unexcusedUnread       []string // documented, not read, no excuse
	staleExcuses          []string // excuse no longer describes a real gap
}

func (d docDrift) clean() bool {
	return len(d.unexcusedUndocumented) == 0 &&
		len(d.unexcusedUnread) == 0 &&
		len(d.staleExcuses) == 0
}

// diffDocs is a pure function over its four inputs — no file I/O, no
// *testing.T — so the synthetic-input tests below can prove it fails in each
// of the three ways it exists to catch without touching a real product or doc
// file.
func diffDocs(
	read, documented map[string]bool,
	excusedUndocumented, excusedUnread map[string]string,
) docDrift {
	var d docDrift

	for name := range read {
		if documented[name] {
			continue
		}

		if _, ok := excusedUndocumented[name]; !ok {
			d.unexcusedUndocumented = append(d.unexcusedUndocumented, name)
		}
	}

	for name := range documented {
		if read[name] {
			continue
		}

		if _, ok := excusedUnread[name]; !ok {
			d.unexcusedUnread = append(d.unexcusedUnread, name)
		}
	}

	for name := range excusedUndocumented {
		if read[name] && !documented[name] {
			continue // still a real gap: the excuse still earns its place
		}

		d.staleExcuses = append(d.staleExcuses, "excusedUndocumented: "+name)
	}

	for name := range excusedUnread {
		if documented[name] && !read[name] {
			continue
		}

		d.staleExcuses = append(d.staleExcuses, "excusedUnread: "+name)
	}

	sort.Strings(d.unexcusedUndocumented)
	sort.Strings(d.unexcusedUnread)
	sort.Strings(d.staleExcuses)

	return d
}

// excusedUndocumented and excusedUnread carry drift between internal/config
// and docs/configuration.mdx. Both are empty. A future entry must begin
// "DEFECT: " if it excuses a real gap rather than a false positive in the
// derivation itself.
var (
	excusedUndocumented = map[string]string{}
	excusedUnread       = map[string]string{}
)

// excusedUndocumentedInCLAUDEMD and excusedUnreadInCLAUDEMD are the same
// pair for CLAUDE.md's table. Also empty: CLAUDE.md's table lists the same
// 66 variables as docs/configuration.mdx.
var (
	excusedUndocumentedInCLAUDEMD = map[string]string{}
	excusedUnreadInCLAUDEMD       = map[string]string{}
)

// excusedFileSuffixUndocumented and excusedFileSuffixUnwired are the pair
// for the narrower _FILE-convention check. Also empty: the five settings
// wired through resolveSecret are exactly the five docs/configuration.mdx
// says accept the suffix.
var (
	excusedFileSuffixUndocumented = map[string]string{}
	excusedFileSuffixUnwired      = map[string]string{}
)

// TestConfigEnvVarsMatchDocumentation holds internal/config's actual
// CETACEAN_* surface against what docs/configuration.mdx promises, in both
// directions.
func TestConfigEnvVarsMatchDocumentation(t *testing.T) {
	drift := diffDocs(readEnvVars(t), documentedEnvVars(t), excusedUndocumented, excusedUnread)

	for _, name := range drift.unexcusedUndocumented {
		t.Errorf("%s is read by internal/config but not documented in %s", name, configDocsPath)
	}

	for _, name := range drift.unexcusedUnread {
		t.Errorf("%s is documented in %s but internal/config never reads it", name, configDocsPath)
	}

	for _, entry := range drift.staleExcuses {
		t.Errorf("stale excuse in docs_test.go (no longer needed): %s", entry)
	}
}

// TestConfigEnvVarsMatchCLAUDEMD holds the same surface against CLAUDE.md's
// own copy of it, which the top-level CLAUDE.md documents as the canonical
// list for anyone (human or agent) working in this repository.
func TestConfigEnvVarsMatchCLAUDEMD(t *testing.T) {
	drift := diffDocs(
		readEnvVars(t),
		claudeMDEnvVars(t),
		excusedUndocumentedInCLAUDEMD,
		excusedUnreadInCLAUDEMD,
	)

	for _, name := range drift.unexcusedUndocumented {
		t.Errorf("%s is read by internal/config but not listed in %s", name, claudeMDPath)
	}

	for _, name := range drift.unexcusedUnread {
		t.Errorf("%s is listed in %s but internal/config never reads it", name, claudeMDPath)
	}

	for _, entry := range drift.staleExcuses {
		t.Errorf("stale excuse in docs_test.go (no longer needed): %s", entry)
	}
}

// TestFileSuffixConventionMatches checks the _FILE secret-file convention on
// its own: every setting resolveSecret wires for a _FILE variant should be
// documented as accepting one, and every setting documented as accepting one
// should be wired through resolveSecret.
func TestFileSuffixConventionMatches(t *testing.T) {
	drift := diffDocs(
		resolveSecretVars(t),
		fileSuffixDocumentedVars(t),
		excusedFileSuffixUndocumented,
		excusedFileSuffixUnwired,
	)

	for _, name := range drift.unexcusedUndocumented {
		t.Errorf(
			"%s is wired through resolveSecret (accepts a _FILE variant) but its card in "+
				"%s never says so",
			name, configDocsPath,
		)
	}

	for _, name := range drift.unexcusedUnread {
		t.Errorf(
			"%s's card in %s says it accepts the _FILE suffix, but it is not wired "+
				"through resolveSecret",
			name, configDocsPath,
		)
	}

	for _, entry := range drift.staleExcuses {
		t.Errorf("stale excuse in docs_test.go (no longer needed): %s", entry)
	}
}

// excusedFileSuffixUndocumentedInCLAUDEMD and
// excusedFileSuffixUnwiredInCLAUDEMD are the same pair for CLAUDE.md's
// spelled-out _FILE names. Also empty: the five names in its sentence are
// exactly resolveSecretVars() with "_FILE" appended.
var (
	excusedFileSuffixUndocumentedInCLAUDEMD = map[string]string{}
	excusedFileSuffixUnwiredInCLAUDEMD      = map[string]string{}
)

// TestFileSuffixConventionMatchesCLAUDEMD is the same check for CLAUDE.md,
// which spells the _FILE-suffixed names out literally, so it compares at the
// literal-name level rather than the base-setting level.
func TestFileSuffixConventionMatchesCLAUDEMD(t *testing.T) {
	wired := map[string]bool{}
	for name := range resolveSecretVars(t) {
		wired[name+"_FILE"] = true
	}

	drift := diffDocs(
		wired,
		claudeMDFileSuffixVars(t),
		excusedFileSuffixUndocumentedInCLAUDEMD,
		excusedFileSuffixUnwiredInCLAUDEMD,
	)

	for _, name := range drift.unexcusedUndocumented {
		t.Errorf(
			"%s is wired through resolveSecret but not named in %s's _FILE-suffix sentence",
			name, claudeMDPath,
		)
	}

	for _, name := range drift.unexcusedUnread {
		t.Errorf(
			"%s is named in %s's _FILE-suffix sentence but not wired through resolveSecret",
			name, claudeMDPath,
		)
	}

	for _, entry := range drift.staleExcuses {
		t.Errorf("stale excuse in docs_test.go (no longer needed): %s", entry)
	}
}

// TestDeprecatedHeadersTrustedProxiesStillReads drives LoadAuth directly to
// prove CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES still parses into
// AuthConfig.Headers.TrustedProxies. The surface check above only proves the
// name still appears on both sides, and main.go's fallback depends on the value.
func TestDeprecatedHeadersTrustedProxiesStillReads(t *testing.T) {
	t.Setenv("CETACEAN_AUTH_MODE", "headers")
	t.Setenv("CETACEAN_AUTH_HEADERS_SUBJECT", "X-Remote-User")
	t.Setenv("CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES", "10.0.0.0/8")

	cfg, err := LoadAuth(nil, nil, "", "")
	if err != nil {
		t.Fatalf("LoadAuth: %v", err)
	}

	if len(cfg.Headers.TrustedProxies) != 1 {
		t.Fatalf(
			"CETACEAN_AUTH_HEADERS_TRUSTED_PROXIES no longer resolves into "+
				"AuthConfig.Headers.TrustedProxies (got %v) — the deprecated alias has "+
				"silently stopped working",
			cfg.Headers.TrustedProxies,
		)
	}
}

// # Proving the check can fail
//
// The three tests below drive diffDocs on made-up data: a read-but-undocumented
// variable, a documented-but-unread one, and a stale excuse of each kind.

func TestDiffDocsCatchesReadButUndocumented(t *testing.T) {
	drift := diffDocs(
		map[string]bool{"CETACEAN_FOO": true},
		map[string]bool{},
		map[string]string{},
		map[string]string{},
	)

	if len(drift.unexcusedUndocumented) != 1 || drift.unexcusedUndocumented[0] != "CETACEAN_FOO" {
		t.Fatalf(
			"expected [CETACEAN_FOO] in unexcusedUndocumented, got %v",
			drift.unexcusedUndocumented,
		)
	}

	if drift.clean() {
		t.Fatal("expected a dirty result")
	}

	// An excuse silences it.
	excused := diffDocs(
		map[string]bool{"CETACEAN_FOO": true},
		map[string]bool{},
		map[string]string{"CETACEAN_FOO": "test fixture, not a real gap"},
		map[string]string{},
	)

	if !excused.clean() {
		t.Fatalf("expected the excuse to silence CETACEAN_FOO, got %+v", excused)
	}
}

func TestDiffDocsCatchesDocumentedButUnread(t *testing.T) {
	drift := diffDocs(
		map[string]bool{},
		map[string]bool{"CETACEAN_BAR": true},
		map[string]string{},
		map[string]string{},
	)

	if len(drift.unexcusedUnread) != 1 || drift.unexcusedUnread[0] != "CETACEAN_BAR" {
		t.Fatalf("expected [CETACEAN_BAR] in unexcusedUnread, got %v", drift.unexcusedUnread)
	}

	excused := diffDocs(
		map[string]bool{},
		map[string]bool{"CETACEAN_BAR": true},
		map[string]string{},
		map[string]string{"CETACEAN_BAR": "test fixture, not a real gap"},
	)

	if !excused.clean() {
		t.Fatalf("expected the excuse to silence CETACEAN_BAR, got %+v", excused)
	}
}

func TestDiffDocsCatchesStaleExcuse(t *testing.T) {
	// excusedUndocumented claims CETACEAN_BAZ is read-but-undocumented, but
	// it is documented too — the excuse no longer names a real gap.
	staleUndocumented := diffDocs(
		map[string]bool{"CETACEAN_BAZ": true},
		map[string]bool{"CETACEAN_BAZ": true},
		map[string]string{"CETACEAN_BAZ": "no longer true"},
		map[string]string{},
	)

	if len(staleUndocumented.staleExcuses) != 1 {
		t.Fatalf(
			"expected one stale excuse for CETACEAN_BAZ, got %v",
			staleUndocumented.staleExcuses,
		)
	}

	// excusedUnread claims CETACEAN_QUX is documented-but-unread, but the
	// product reads it too — same failure, other direction.
	staleUnread := diffDocs(
		map[string]bool{"CETACEAN_QUX": true},
		map[string]bool{"CETACEAN_QUX": true},
		map[string]string{},
		map[string]string{"CETACEAN_QUX": "no longer true"},
	)

	if len(staleUnread.staleExcuses) != 1 {
		t.Fatalf("expected one stale excuse for CETACEAN_QUX, got %v", staleUnread.staleExcuses)
	}
}
